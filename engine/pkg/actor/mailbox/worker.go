// Package mailbox
// @Title  统一Worker实现
// @Description  统一的消息处理Worker，支持双队列和多优先级队列两种模式
// @Author  yr  2025/11/27
// @Update  yr  2025/11/27
package mailbox

import (
	"context"
	"runtime"
	"runtime/debug"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

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

const (
	workerStateRunning int32 = iota
	workerStateClosing
	workerStateClosed
)

// Worker 统一的消息处理 Worker，实现 IMailboxWorker。
//
// 职责：
//   - 接收 SubmitEvent 调用，将事件提交至内部队列管理器（IQueueManager）；
//   - 在独立 goroutine 中循环从队列获取事件并执行；
//   - 使用 idle.AdaptiveController 在队列为空时进行条件等待或退避，避免空转占用 CPU；
//   - 在 Stop 时，通过 queueManager.DrainAll 将队列中剩余事件处理完毕，保证关闭过程无消息丢失。
type Worker struct {
	workerId        int32
	state           atomic.Int32 // workerStateRunning / workerStateClosing / workerStateClosed
	submitters      atomic.Int64
	pool            *WorkerPool
	wg              sync.WaitGroup
	inflightReads   sync.WaitGroup  // per-Worker：仅跟踪本 Worker spawn 的读 goroutine
	inflightReadCnt atomic.Int64    // per-Worker：当前 in-flight 读 goroutine 数（WaitGroup 无法查询计数）
	pendingJob      inf.IMailboxJob // 单值字段：TryLock/读路径轮询检测到 closed=true 时暂存已出队未执行 Job
	queueManager    IQueueManager   // 队列管理器（可以是双队列或多优先级队列）
	// per-Worker RW 指标（跨 goroutine 安全，WorkerPool 汇总时遍历累加）
	rwReadDurationSum atomic.Int64             // 读操作累计耗时（纳秒），除以 rwReadCount 得平均值
	rwReadCount       atomic.Int64             // 本 Worker 累计读操作数（用于计算平均读耗时）
	rwWriteWaitSum    atomic.Int64             // 写操作累计等待耗时（纳秒）
	rwWriteWaitCount  atomic.Int64             // 本 Worker 累计写等待次数
	idler             *idle.AdaptiveController // 自适应空闲控制器
	count             atomic.Int64
	drainPolicy       DrainPolicy
}

// newWorker 创建统一Worker
func newWorker(workerId int32, conf *config.MailboxConf, pool *WorkerPool) inf.IMailboxWorker {
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
func (w *Worker) GetWorkerId() int32 {
	return w.workerId
}

// SubmitJob 提交任务到队列
func (w *Worker) SubmitJob(job inf.IMailboxJob) error {
	// Lock-free stop gate: prevent "submit after drain" without introducing mutex on hot path.
	if w.state.Load() != workerStateRunning {
		return def.ErrMailboxWorkerClosed
	}
	w.submitters.Add(1)
	// If Stop flipped state concurrently, back out and refuse.
	if w.state.Load() != workerStateRunning {
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
//  2. 若获取成功，根据 enableRW 决定走 RW 或串行路径；
//  3. 若当前没有事件，则调用 idler.Idle() 进行条件等待或退避；
//  4. 当 closed 标记为 true 时，循环退出，并在 defer 中处理所有残留事件。
func (w *Worker) run() {
	defer w.wg.Done()

	// 退出时：先等待本 Worker 的 in-flight 读完成（带超时兜底），再在 WLock 下 drain 残留消息
	defer func() {
		// ① 等待本 Worker 的 in-flight 读 goroutine 完成（带超时保护）
		stopTimedOut := false
		if w.pool.enableRW.Load() {
			done := make(chan struct{})
			go func() {
				w.inflightReads.Wait() // per-Worker：仅等待本 Worker spawn 的读 goroutine
				close(done)
			}()
			select {
			case <-done:
				// 本 Worker 的所有读 goroutine 正常完成
			case <-time.After(w.pool.stopTimeout):
				// 超时：标记不安全关闭，强制继续
				stopTimedOut = true
				w.pool.logger.Errorf("Worker %d: StopTimeout (%v) exceeded, "+
					"read goroutines still in-flight. "+
					"Drain forced to DrainDiscard to avoid data race with leaked goroutines.",
					w.workerId, w.pool.stopTimeout)
			}
		}

		if w.queueManager == nil {
			return
		}

		// 确定有效的 Drain 策略（超时后强制降级为 DrainDiscard）
		effectiveDrainPolicy := w.drainPolicy
		if stopTimedOut {
			effectiveDrainPolicy = DrainDiscard
		}

		switch effectiveDrainPolicy {
		case DrainDiscard:
			// DrainDiscard 不需要 WLock（不执行业务逻辑，不访问服务共享状态）
			if w.pendingJob != nil {
				w.discardExec(w.pendingJob)
				w.pendingJob = nil
			}
			w.queueManager.DrainAll(func(e inf.IMailboxJob) {
				w.discardExec(e)
			})
		default:
			// ② Drain 阶段获取 WLock，保证跨 Worker Drain 串行 + 与读 goroutine 互斥
			if w.pool.enableRW.Load() {
				w.pool.rwMu.Lock()
			}
			if w.pendingJob != nil {
				w.safeExec(w.pendingJob)
				w.pendingJob = nil
			}
			w.queueManager.DrainAll(func(e inf.IMailboxJob) {
				w.safeExec(e)
			})
			if w.pool.enableRW.Load() {
				w.pool.rwMu.Unlock()
			}
		}
	}()

	// 主处理循环
	for w.state.Load() != workerStateClosed {
		e, ok := w.queueManager.NextJob()
		if !ok {
			w.idler.Idle()
			continue
		}

		if w.pool.enableRW.Load() {
			w.execWithRW(e)
		} else {
			w.safeExec(e) // 未启用 RW，保持原有串行行为
		}
	}
}

// BeginStop 发起停止（非阻塞）。
func (w *Worker) BeginStop() {
	// First, stop accepting new submissions.
	if !w.state.CompareAndSwap(workerStateRunning, workerStateClosing) {
		return // already stopping/stopped
	}

	// Wait for in-flight SubmitJob calls to finish.
	for w.submitters.Load() != 0 {
		runtime.Gosched()
	}

	// Now stop the run loop.
	w.state.Store(workerStateClosed)

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
		// 不执行业务，直接释放 job
		if job != nil {
			job.Release()
		}
	}()

	// 记录日志
	w.pool.logger.WithContext(ctx).Warnf("Worker %d discard job %v", w.workerId, job)
	// RW 可观测性：丢弃计数
	w.pool.rwDrainDiscardTotal.Add(1)
	// 通知业务层 Job 被丢弃
	w.pool.invoker.OnJobDiscarded(job, def.ErrMailboxNotRunning)
}

// safeExec 在执行事件处理逻辑时提供 panic 保护和可选的性能分析（向后兼容，skipProfiler=false）。
func (w *Worker) safeExec(job inf.IMailboxJob) {
	w.safeExecInternal(job, false)
}

// safeExecSkipProfiler RW 模式下读 goroutine 专用（跳过共享 Profiler，避免并发安全问题）
func (w *Worker) safeExecSkipProfiler(job inf.IMailboxJob) {
	w.safeExecInternal(job, true)
}

// safeExecInternal 统一的 Job 执行逻辑：panic 恢复 + 可选 Profiler + 中间件 OnComplete + Job Release。
// skipProfiler=true 时跳过共享 Profiler（读 goroutine 专用），避免并发安全问题和 stack 语义破坏。
func (w *Worker) safeExecInternal(job inf.IMailboxJob, skipProfiler bool) {
	ctx := job.GetContext()
	mctx := job.GetMiddlewareContext()
	var execErr error
	var panicVal interface{}

	// 读 goroutine 路径：向 context 中注入 RWContextInfo，
	// 业务层可通过 ctx.Value(def.RWContextKey) 检测当前是否在 ReadOnly 上下文中执行。
	// 框架层在 Service.PostJob 中检测此标记，拒绝 ReadOnly handler 的自投递。
	if skipProfiler && w.pool.enableRW.Load() {
		ctx = context.WithValue(ctx, def.RWContextKey, def.RWContextInfo{
			Mode:          def.RWModeRead,
			SourceService: w.pool.invoker.GetServiceName(),
		})
	}

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

		// job 执行后需要释放
		if job != nil {
			job.Release()
		}
	}()

	// ---------- watchdog: 单 Job 执行超时告警 ----------
	if maxExec := w.pool.maxJobExecTime; maxExec > 0 {
		timer := time.AfterFunc(maxExec, func() {
			w.pool.logger.WithContext(ctx).Warnf(
				"Worker %d job execution exceeds %v: %v",
				w.workerId, maxExec, job,
			)
		})
		defer timer.Stop()
	}

	var analyzer *profiler.Analyzer
	// skipProfiler=true 时跳过共享 Profiler，避免并发安全问题和 stack 语义破坏
	if w.pool.profiler != nil && !skipProfiler {
		analyzer = w.pool.profiler.Push("[ STATE ]job_type_" + strconv.Itoa(int(job.GetType())))
	}

	// ---------- 执行 Job + 读时长采集 ----------
	var readStart time.Time
	if skipProfiler {
		readStart = time.Now()
	}

	// 调用消息处理器
	if err := w.pool.invoker.ExecuteJob(ctx, job); err != nil {
		execErr = err
	}

	if skipProfiler {
		elapsed := time.Since(readStart).Nanoseconds()
		w.rwReadDurationSum.Add(elapsed)
		w.rwReadCount.Add(1)
	}

	if analyzer != nil {
		analyzer.Pop()
		analyzer = nil
	}
}

// ---- RW 读写分离核心方法 ----

// getRWMode 从 Job 中安全获取 RWMode（兜底为 RWModeWrite）
func getRWMode(job inf.IMailboxJob) def.RWMode {
	if rwJob, ok := job.(inf.IRWModeJob); ok {
		return rwJob.GetRWMode()
	}
	return def.RWModeWrite // 未实现 IRWModeJob 的 Job 默认为写
}

// execWithRW 根据 Job 的 RWMode 执行读写分离逻辑
func (w *Worker) execWithRW(job inf.IMailboxJob) {
	if getRWMode(job) == def.RWModeRead {
		w.execRead(job)
	} else {
		w.execWrite(job)
	}
}

// execRead 读操作：统一轮询获取前置条件后 spawn goroutine 并发执行。
//
// 统一轮询循环完成三项前置检查：
// ① closed 检查（响应 Stop）；② writeRequested 自旋让步（防止写饥饿）；
// ③ 信号量令牌获取（MaxConcurrentReads 硬上限）。
func (w *Worker) execRead(job inf.IMailboxJob) {
	yieldCount := 1
	const maxYieldCount = 64 // 上限，避免空转过多
	for {
		// ① 优先检查 Stop 状态
		if w.state.Load() == workerStateClosed {
			w.pendingJob = job
			return
		}
		// ② 若有 pending writer，退避让步（让已有的读 goroutine 完成 RUnlock）
		// 使用 runtime.Gosched() 循环替代 time.Sleep，避免 Windows 下 15ms 最小精度问题
		if w.pool.writeRequested.Load() > 0 {
			for i := 0; i < yieldCount; i++ {
				runtime.Gosched()
				if w.pool.writeRequested.Load() == 0 {
					break
				}
			}
			if yieldCount < maxYieldCount {
				yieldCount *= 2
			}
			continue
		}
		// ③ 信号量令牌获取（非阻塞尝试，失败则退避后重新进入循环）
		if w.pool.readSem != nil {
			select {
			case w.pool.readSem <- struct{}{}:
				// 获取令牌成功
			default:
				// 令牌已满，Gosched 退避后重试
				for i := 0; i < yieldCount; i++ {
					runtime.Gosched()
				}
				if yieldCount < maxYieldCount {
					yieldCount *= 2
				}
				continue
			}
		}
		break
	}

	// 获取读锁
	w.pool.rwMu.RLock()

	// 【关键：RLock-after-check 重检查（§10.14 动态开关安全协议）】
	// 防止在 SetRWEnabled(false) 切换窗口内误 spawn 读 goroutine
	if !w.pool.enableRW.Load() {
		w.pool.rwMu.RUnlock()
		// 必须释放轮询循环中已获取的信号量令牌
		if w.pool.readSem != nil {
			<-w.pool.readSem
		}
		w.safeExec(job) // 降级为串行执行
		return
	}

	// 【关键时序】inflightReads.Add 必须在 spawn goroutine 之前、
	// 在 RLock + enableRW 重检查之后同步执行
	w.inflightReads.Add(1)
	w.inflightReadCnt.Add(1)  // 可观测性：暂存当前 in-flight 读数量
	w.pool.rwReadTotal.Add(1) // 全局读计数

	readFunc := func() {
		// WaitGroup + 锁 + 信号量泄漏防护
		var rlockReleased atomic.Bool
		defer func() {
			// 归还信号量令牌
			if w.pool.readSem != nil {
				<-w.pool.readSem
			}
			// 释放读锁（必须在 Done 之前）
			if rlockReleased.CompareAndSwap(false, true) {
				w.pool.rwMu.RUnlock()
			}
			// 可观测性：in-flight 读数量递减
			w.inflightReadCnt.Add(-1)
			// WaitGroup Done（最后释放）
			w.inflightReads.Done()
		}()

		w.safeExecSkipProfiler(job) // 读 goroutine 跳过共享 Profiler
	}

	// 优先使用读 goroutine 池，失败则 fallback 到裸 goroutine
	if w.pool.readPool != nil {
		if err := w.pool.readPool.Go(readFunc); err != nil {
			go readFunc()
		}
	} else {
		go readFunc()
	}
}

// execWrite 写操作：等待所有 in-flight 读完成，然后独占执行。
//
// 使用 TryLock 而非 Lock，允许在等待写锁期间检查 closed 标志，
// 保证 BeginStop() 能在有限时间内让主循环退出。
func (w *Worker) execWrite(job inf.IMailboxJob) {
	w.pool.rwWriteTotal.Add(1)   // 全局写计数
	writeWaitStart := time.Now() // 写等待计时开始
	w.pool.writeRequested.Add(1)
	backoff := time.Duration(0)
	const maxBackoff = 5 * time.Millisecond
	for !w.pool.rwMu.TryLock() {
		if w.state.Load() == workerStateClosed {
			w.pool.writeRequested.Add(-1)
			// Worker 正在停止但无法获取写锁，暂存到 pendingJob 由 Drain 处理
			w.pendingJob = job
			return
		}
		if backoff == 0 {
			runtime.Gosched()
			backoff = time.Microsecond
		} else {
			time.Sleep(backoff)
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
	// 写等待耗时指标
	writeWait := time.Since(writeWaitStart)
	w.rwWriteWaitSum.Add(writeWait.Nanoseconds())
	w.rwWriteWaitCount.Add(1)
	if writeWait > 100*time.Millisecond {
		w.pool.logger.Warnf("Worker %d write lock wait %v (>100ms), possible long-running readers",
			w.workerId, writeWait)
	}
	// defer 保证 Unlock 在 Add(-1) 之前执行（LIFO）
	// 确保 writeRequested.Add(-1) 在 Unlock 之后：
	// 读路径看到 writeRequested==0 时 WLock 必定已释放
	defer w.pool.writeRequested.Add(-1)
	defer w.pool.rwMu.Unlock()
	w.safeExec(job)
}

// GetJobLen 获取队列中的任务总数
func (w *Worker) GetJobLen() int {
	if w.queueManager == nil {
		return 0
	}
	return w.queueManager.GetJobLen()
}
