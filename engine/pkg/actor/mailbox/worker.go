// Package mailbox
// @Title  统一Worker实现
// @Description  统一的消息处理Worker，支持双队列和多优先级队列两种模式
// @Author  yr  2025/11/27
// @Update  yr  2025/11/27
package mailbox

import (
	"fmt"
	"reflect"
	"runtime/debug"
	"sync"
	"sync/atomic"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
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
	pool         *WorkerPool
	wg           sync.WaitGroup
	queueManager IQueueManager            // 队列管理器（可以是双队列或多优先级队列）
	idler        *idle.AdaptiveController // 自适应空闲控制器
	count        atomic.Int64
}

// NewWorker 创建统一Worker
func NewWorker(workerId int, conf *config.MailboxConf, pool *WorkerPool) inf.IMailboxWorker {
	w := &Worker{
		workerId: workerId,
		pool:     pool,
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
		var queueConf *config.MultiLevelQueueConf

		// 优先使用新配置
		if conf.SchedulePolicy != nil && conf.SchedulePolicy.MultiLevelQueueConf != nil {
			queueConf = conf.SchedulePolicy.MultiLevelQueueConf
		} else if conf.SchedulePolicy != nil && conf.SchedulePolicy.MultiLevelConf != nil {
			// 兼容旧配置
			old := conf.SchedulePolicy.MultiLevelConf
			queueConf = &config.MultiLevelQueueConf{
				Strategy:        old.Strategy,
				TotalBatchLimit: old.TotalBatchLimit,
				PriorityBatches: old.PriorityBatches,
			}
		} else {
			// 使用默认配置
			queueConf = DefaultMultiLevelQueueConf()
		}

		return NewPriorityQueueManager(queueConf)

	default:
		log.SysLogger.Warnf("Unknown queue mode: %s, using dual queue", queueMode)
		return NewDualQueueManager()
	}
}

// GetWorkerId 获取Worker的ID
func (w *Worker) GetWorkerId() int {
	return w.workerId
}

// SubmitEvent 提交事件到队列
func (w *Worker) SubmitEvent(e inf.IEvent) error {
	if w.closed.Load() {
		return def.ErrMailboxWorkerClosed
	}

	if w.queueManager == nil {
		return def.ErrMailboxWorkerChannelNotInit
	}

	// 提交到队列管理器
	err := w.queueManager.Submit(e)
	if err != nil {
		return err
	}
	// 增加事件计数
	w.count.Add(1)

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
//  1. 尝试从 queueManager.NextEvent() 获取下一个事件；
//  2. 若获取成功，则调用 safeExec 执行并继续下一轮；
//  3. 若当前没有事件，则调用 idler.Idle() 进行条件等待或退避；
//  4. 当 closed 标记为 true 时，循环退出，并在 defer 中通过 DrainAll 处理所有残留事件。
func (w *Worker) run() {
	defer w.wg.Done()

	// 退出时处理所有剩余消息
	defer func() {
		w.queueManager.DrainAll(func(e inf.IEvent) {
			w.safeExec(e)
		})
	}()
	//var backoff = 1
	//var maxBackoff = 4
	// 主处理循环
	for !w.closed.Load() {
		// 尝试获取下一个事件
		if e, ok := w.queueManager.NextEvent(); ok {
			w.safeExec(e)
			continue
		}

		// 队列为空，使用空闲控制器等待
		w.idler.Idle()
		//if backoff < maxBackoff {
		//	backoff *= 2
		//}
		//time.Sleep(time.Microsecond * time.Duration(backoff))
	}
}

// Stop 停止Worker
func (w *Worker) Stop() {
	// 标记为已关闭
	if !w.closed.CompareAndSwap(false, true) {
		return // 已经关闭过了
	}

	// 唤醒可能在等待的Worker
	if w.idler != nil {
		w.idler.Wake()
	}

	// 等待Worker完全退出
	w.wg.Wait()
	// 打印计数
	log.SysLogger.Infof("Worker %d processed %d events", w.workerId, w.count.Load())
}

// safeExec 在执行事件处理逻辑时提供 panic 保护和可选的性能分析：
//   - 捕获业务处理中的 panic，调用 invoker.EscalateFailure 上报错误；
//   - 可选地通过 profiler.Analyzer 记录每类事件的处理耗时；
//   - 在业务处理完成后，依次调用所有 mailbox 中间件的 MessageReceived 作为后置 hook。
func (w *Worker) safeExec(e inf.IEvent) {
	defer func() {
		if r := recover(); r != nil {
			log.SysLogger.WithContext(e.GetContext()).Errorf("exec error: %v\ntrace:%s", r, debug.Stack())

			// 双重保护：EscalateFailure 可能也会 panic
			func() {
				defer func() {
					if r2 := recover(); r2 != nil {
						log.SysLogger.WithContext(e.GetContext()).Errorf("EscalateFailure also panicked: %v\ntrace:%s", r2, debug.Stack())
					}
				}()
				w.pool.invoker.EscalateFailure(r, e)
			}()
		}
	}()

	var analyzer *profiler.Analyzer
	if w.pool.profiler != nil {
		analyzer = w.pool.profiler.Push(fmt.Sprintf("[ STATE ]%s", reflect.TypeOf(e).String()))
	}

	// 调用消息处理器
	w.pool.invoker.InvokeMessage(e)

	if analyzer != nil {
		analyzer.Pop()
		analyzer = nil
	}

	// 调用中间件
	for _, ms := range w.pool.middlewares {
		ms.MessageReceived(e)
	}
}

// GetMsgLen 获取队列中的消息总数
func (w *Worker) GetMsgLen() int {
	if w.queueManager == nil {
		return 0
	}
	return w.queueManager.GetMsgLen()
}
