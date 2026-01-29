// Package mailbox
// @Title  统一Worker实现
// @Description  统一的消息处理Worker，支持双队列和多优先级队列两种模式
// @Author  yr  2025/11/27
// @Update  yr  2025/11/27
package mailbox

import (
	"fmt"
	"reflect"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/profiler"
	"github.com/njtc406/emberengine/engine/pkg/utils/idle"
)

const (
	QueueModeDual     = "dual"
	QueueModePriority = "priority"
)

// Worker 统一的消息处理 Worker，实现 IMailboxWorker。
//
// 职责：
//   - 接收 SubmitEvent 调用，将事件提交至内部队列管理器（IQueueManager）；
//   - 在独立 goroutine 中循环从队列获取事件并执行；
//   - 使用 idle.AdaptiveController 在队列为空时进行条件等待或退避，避免空转占用 CPU；
//   - 在 Stop 时，通过 queueManager.DrainAll 将队列中剩余事件处理完毕，保证关闭过程无消息丢失。
type Worker struct {
	workerId     int
	closed       atomic.Bool
	closing      atomic.Bool
	submitters   atomic.Int64
	pool         *WorkerPool
	wg           sync.WaitGroup
	queueManager IQueueManager            // 队列管理器（可以是双队列或多优先级队列）
	idler        *idle.AdaptiveController // 自适应空闲控制器
	count        atomic.Int64
	drainPolicy  DrainPolicy
}

// newWorker 创建统一Worker
func newWorker(workerId int, conf *config.MailboxConf, pool *WorkerPool) inf.IMailboxWorker {
	w := &Worker{
		workerId: workerId,
		pool:     pool,
		drainPolicy: func() DrainPolicy {
			if pool != nil {
				return pool.drainPolicy
			}
			return DrainExecute
		}(),
	}

	// 根据配置创建队列管理器
	w.queueManager = createQueueManager(conf)

	// 创建空闲控制器
	idlerConf := conf.SchedulePolicy.IdlerConf
	if idlerConf == nil {
		idlerConf = &config.WorkerIdlerConf{
			EnableCond: true,
		}
	}
	w.idler = idle.NewAdaptiveController(
		idlerConf.EnableCond,
		idlerConf.BackoffBaseDelay,
		idlerConf.BackoffMaxDelay,
		idlerConf.MaxIdleBeforeBackoff,
		idlerConf.BackoffMaxRetries,
	)

	return w
}

// createQueueManager 根据配置创建队列管理器
func createQueueManager(conf *config.MailboxConf) IQueueManager {
	// 确定队列模式
	queueMode := conf.QueueMode
	if queueMode == "" {
		queueMode = QueueModeDual // 默认双队列模式
	}

	switch queueMode {
	case QueueModeDual:
		// 双队列模式（系统队列 + 用户队列）
		return NewDualQueueManager()

	case QueueModePriority:
		// 多优先级队列模式
		return NewPriorityQueueManager(conf.SchedulePolicy.MultiLevelQueueConf)

	default:
		return NewDualQueueManager()
	}
}

// GetWorkerId 获取Worker的ID
func (w *Worker) GetWorkerId() int {
	return w.workerId
}

// SubmitEvent 提交事件到队列
func (w *Worker) SubmitJob(job inf.IMailboxJob) error {
	// Lock-free stop gate: prevent "submit after drain" without introducing mutex on hot path.
	if w.closing.Load() || w.closed.Load() {
		return def.ErrMailboxWorkerClosed
	}
	w.submitters.Add(1)
	// If Stop flipped closing concurrently, back out and refuse.
	if w.closing.Load() || w.closed.Load() {
		w.submitters.Add(-1)
		return def.ErrMailboxWorkerClosed
	}
	defer w.submitters.Add(-1)

	if w.queueManager == nil {
		return def.ErrMailboxWorkerChannelNotInit
	}

	// 提交到队列管理器
	err := w.queueManager.Submit(job)
	if err != nil {
		return err
	}
	// 增加事件计数
	w.count.Add(1) // TODO 后续替换为一个计数器,设置一个开关,打开时才计数,release环境可以关闭

	// 唤醒Worker
	if w.idler != nil {
		w.idler.Wake()
	}

	return nil
}

// Start 启动 Worker，在独立 goroutine 中运行 run 主循环。
func (w *Worker) Start() {
	w.wg.Add(1)
	go w.run()
}

// run 是 Worker 的主循环。
//
// 循环逻辑：
//  1. 尝试从 queueManager.NextJob() 获取下一个事件；
//  2. 若获取成功，则调用 safeExec 执行并继续下一轮；
//  3. 若当前没有事件，则调用 idler.Idle() 进行条件等待或退避；
//  4. 当 closed 标记为 true 时，循环退出，并在 defer 中通过 DrainAll 处理所有残留事件。
func (w *Worker) run() {
	defer w.wg.Done()

	// 退出时处理所有剩余消息
	defer func() {
		if w.queueManager == nil {
			return
		}
		switch w.drainPolicy {
		case DrainDiscard:
			w.queueManager.DrainAll(func(e inf.IMailboxJob) {
				w.discardExec(e)
			})
		default:
			w.queueManager.DrainAll(func(e inf.IMailboxJob) {
				w.safeExec(e)
			})
		}
	}()

	// 主处理循环
	for !w.closed.Load() {
		// 尝试获取下一个事件
		if e, ok := w.queueManager.NextJob(); ok {
			w.safeExec(e)
			continue
		}

		// 队列为空，使用空闲控制器等待
		w.idler.Idle()
	}
}

// BeginStop 发起停止（非阻塞）。
func (w *Worker) BeginStop() {
	// First, stop accepting new submissions.
	if !w.closing.CompareAndSwap(false, true) {
		return // already stopping/stopped
	}

	// Wait for in-flight SubmitJob calls to finish.
	for w.submitters.Load() != 0 {
		runtime.Gosched()
	}

	// Now stop the run loop.
	if !w.closed.CompareAndSwap(false, true) {
		return
	}

	// 唤醒可能在等待的Worker
	if w.idler != nil {
		w.idler.Wake()
	}

	// NOTE: 不在这里 Wait，避免在 worker 自身 goroutine 内调用导致自等死锁。
}

// Wait 等待 worker 完全退出。
func (w *Worker) Wait() {
	w.wg.Wait()
	if w.pool != nil && w.pool.logger != nil {
		w.pool.logger.Infof("Worker %d processed %d events", w.workerId, w.count.Load())
	}
}

// Stop 兼容接口：BeginStop + Wait。
func (w *Worker) Stop() {
	w.BeginStop()
	w.Wait()
}

// discardExec 在 DrainDiscard 策略下处理残留消息：不执行业务，仅触发 OnComplete 并回收引用。
func (w *Worker) discardExec(job inf.IMailboxJob) {
	ctx := job.GetContext()
	mctx := job.GetMiddlewareContext()
	defer func() {
		// 调用中间件链的 OnComplete（逆序执行）
		if mctx != nil {
			w.pool.middlewareChain.ExecuteOnComplete(mctx, def.ErrMailboxNotRunning, nil)
		}
		// 不执行业务，直接释放内部 event（原本由 InvokeJob 负责 Release）
		if job != nil {
			job.Release()
		}
		// 释放 CtxEvent 包装器
		job.Release()
	}()

	// 记录日志
	w.pool.logger.WithContext(ctx).Errorf("Worker %d discard job %v", w.workerId, job)
}

// safeExec 在执行事件处理逻辑时提供 panic 保护和可选的性能分析：
//   - 捕获业务处理中的 panic，调用 invoker.EscalateFailure 上报错误；
//   - 可选地通过 profiler.Analyzer 记录每类事件的处理耗时；
//   - 在业务处理完成后，调用中间件链的 OnComplete 回调。
func (w *Worker) safeExec(job inf.IMailboxJob) {
	ctx := job.GetContext()
	mctx := job.GetMiddlewareContext()
	var execErr error
	var panicVal interface{}

	defer func() {
		if r := recover(); r != nil {
			panicVal = r
			w.pool.logger.WithContext(ctx).Errorf("exec error: %v\ntrace:%s", r, debug.Stack())

			// 双重保护：EscalateFailure 可能也会 panic
			func() {
				defer func() {
					if r2 := recover(); r2 != nil {
						w.pool.logger.WithContext(ctx).Errorf("EscalateFailure also panicked: %v\ntrace:%s", r2, debug.Stack())
					}
				}()
				w.pool.invoker.EscalateFailure(ctx, r, job)
			}()
		}

		// 调用中间件链的 OnComplete（逆序执行）
		if mctx != nil {
			w.pool.middlewareChain.ExecuteOnComplete(mctx, execErr, panicVal)
		}

		// job执行后需要释放
		if job != nil {
			job.Release()
		}
	}()

	var analyzer *profiler.Analyzer
	if w.pool.profiler != nil {
		analyzer = w.pool.profiler.Push(fmt.Sprintf("[ STATE ]%s", reflect.TypeOf(job).String()))
	}

	// 调用消息处理器
	if err := w.pool.invoker.InvokeJob(ctx, job); err != nil {
		execErr = err
	}

	if analyzer != nil {
		analyzer.Pop()
		analyzer = nil
	}
}

// GetJobLen 获取队列中的任务总数
func (w *Worker) GetJobLen() int {
	if w.queueManager == nil {
		return 0
	}
	return w.queueManager.GetJobLen()
}
