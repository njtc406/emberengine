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
	"sync"
	"sync/atomic"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
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

// watchdogCanceler 看门狗调度器的最小接口，解耦 Worker 对 timingwheel 包的直接依赖。
// WorkerPool 在 Start 时创建适配器注入到 WorkerEnv。
type watchdogCanceler interface {
	// Schedule 注册一个超时回调，返回 ID 用于取消。d <= 0 或调度失败时返回 0。
	Schedule(d time.Duration, onExpire func()) uint64
	// Cancel 取消尚未触发的超时回调。id == 0 时为 no-op。
	Cancel(id uint64)
}

// Worker 统一的消息处理 Worker，实现 IMailboxWorker。
// WorkerEnv 封装 Worker 运行所需的外部依赖。
// 由 WorkerPool 在创建 Worker 时注入，Worker 不再直接引用 WorkerPool。
type WorkerEnv struct {
	logger            log.ILoggerX
	invoker           inf.IMessageInvoker
	middlewareChain   *MiddlewareChain
	rw                *RWController
	watchdogScheduler watchdogCanceler // 时间轮看门狗（nil = 使用 time.AfterFunc 降级）
}

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
	env             *WorkerEnv
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
func newWorker(workerId int32, conf *config.MailboxConf, env *WorkerEnv, drainPolicy DrainPolicy) inf.IMailboxWorker {
	w := &Worker{
		workerId:    workerId,
		env:         env,
		drainPolicy: drainPolicy,
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
		if w.env.rw.enabled.Load() {
			done := make(chan struct{})
			go func() {
				w.inflightReads.Wait() // per-Worker：仅等待本 Worker spawn 的读 goroutine
				close(done)
			}()
			select {
			case <-done:
				// 本 Worker 的所有读 goroutine 正常完成
			case <-time.After(w.env.rw.stopTimeout):
				// 超时：标记不安全关闭，强制继续
				stopTimedOut = true
				w.env.logger.Errorf("Worker %d: StopTimeout (%v) exceeded, "+
					"read goroutines still in-flight. "+
					"Drain forced to DrainDiscard to avoid data race with leaked goroutines.",
					w.workerId, w.env.rw.stopTimeout)
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

		// RW 启用时，Drain 必须与可能仍在飞行的读 goroutine 互斥访问 invoker。
		// - 正常关闭（未超时）：用阻塞式 Lock，等待读 goroutine 已全部 Done，可立即拿到；
		// - StopTimeout 超时：直接走 unsafe drain（不获取 WLock，跳过 invoker 调用），
		//   因为此时仍有泄漏读 goroutine 持有 RLock，强行 Lock 等同于无限阻塞。
		var (
			rwLockHeld    bool
			rwUnsafeDrain bool
		)
		if w.env.rw.enabled.Load() {
			if stopTimedOut {
				rwUnsafeDrain = true
				w.env.rw.unsafeDrainEvents.Add(1) // 事件级计数：进入 unsafe drain 路径
				w.env.logger.Errorf("Worker %d: stop timeout reached, drain in unsafe mode "+
					"(skip invoker.OnJobDiscarded) to avoid race with leaked read goroutines", w.workerId)
			} else {
				w.env.rw.mu.Lock()
				rwLockHeld = true
			}
		}

		switch effectiveDrainPolicy {
		case DrainDiscard:
			if w.pendingJob != nil {
				w.discardExec(w.pendingJob, rwUnsafeDrain)
				w.pendingJob = nil
			}
			w.queueManager.DrainAll(func(e inf.IMailboxJob) {
				w.discardExec(e, rwUnsafeDrain)
			})
		default:
			if w.pendingJob != nil {
				w.safeExec(w.pendingJob)
				w.pendingJob = nil
			}
			w.queueManager.DrainAll(func(e inf.IMailboxJob) {
				w.safeExec(e)
			})
			if rwLockHeld {
				w.env.rw.mu.Unlock()
				rwLockHeld = false
			}
		}
		// DrainDiscard 分支收尾释放 WLock
		if rwLockHeld {
			w.env.rw.mu.Unlock()
		}
	}()

	// 主处理循环
	for w.state.Load() != workerStateClosed {
		e, ok := w.queueManager.NextJob()
		if !ok {
			w.idler.Idle()
			continue
		}

		if w.env.rw.enabled.Load() {
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
	// 阶梯退避：先 Gosched，超过阈值后改用 microsecond 级 Sleep，避免长时间 CPU 燃烧。
	// 总体超时保护：避免上层死循环投递导致永久阻塞。
	const spinBudget = 1024
	deadline := time.Now().Add(w.env.rw.stopTimeout)
	spins := 0
	sleep := time.Duration(0)
	for w.submitters.Load() != 0 {
		if time.Now().After(deadline) {
			w.env.logger.Errorf("Worker %d: BeginStop deadline exceeded, submitters=%d still in-flight, forcing stop",
				w.workerId, w.submitters.Load())
			break
		}
		if spins < spinBudget {
			runtime.Gosched()
			spins++
			continue
		}
		if sleep == 0 {
			sleep = time.Microsecond
		} else if sleep < 100*time.Microsecond {
			sleep *= 2
		}
		time.Sleep(sleep)
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
	if w.env != nil && w.env.logger != nil {
		w.env.logger.Infof("Worker %d processed %d events", w.workerId, w.count.Load())
	}
}

// Stop 兼容接口：BeginStop + Wait。
func (w *Worker) Stop() {
	w.BeginStop()
	w.Wait()
}

// discardExec 在 DrainDiscard 策略下处理残留消息：不执行业务，仅触发 OnComplete 并回收引用。
//
// rwUnsafe=true 表示当前未持有 WLock 且 RW 模式启用（drain 抢锁超时降级），
// 此时跳过 invoker.OnJobDiscarded 调用，避免与可能仍在飞行的读 goroutine 形成
// 对 Service 共享状态的并发访问。Job/mctx 仍按正常路径释放。
func (w *Worker) discardExec(job inf.IMailboxJob, rwUnsafe bool) {
	ctx := job.GetContext()
	mctx := job.GetMiddlewareContext()
	defer func() {
		// rwUnsafe 路径下跳过中间件 OnComplete，避免自定义中间件回写 invoker 共享字段
		// 与泄漏读 goroutine 形成 race。内置中间件的统计损失可通过 drainDiscardTotal 感知。
		if mctx != nil && !rwUnsafe {
			w.env.middlewareChain.ExecuteOnComplete(mctx, def.ErrMailboxNotRunning, nil)
		} else if mctx != nil {
			// rwUnsafe: 仅归还 mctx 到池，不执行中间件回调
			w.env.middlewareChain.ReturnContext(mctx)
		}
		// 不执行业务，直接释放 job
		if job != nil {
			job.Release()
		}
	}()

	// 记录日志
	w.env.logger.WithContext(ctx).Warnf("Worker %d discard job %v (rwUnsafe=%v)", w.workerId, job, rwUnsafe)
	// RW 可观测性：丢弃计数
	w.env.rw.drainDiscardTotal.Add(1)
	if rwUnsafe {
		// 不通知业务层，避免 race；上层应通过 drainDiscardTotal 与日志感知
		return
	}
	// 通知业务层 Job 被丢弃
	w.env.invoker.OnJobDiscarded(job, def.ErrMailboxNotRunning)
}

// safeExec 在执行事件处理逻辑时提供 panic 保护（向后兼容，skipReadCtx=false）。
func (w *Worker) safeExec(job inf.IMailboxJob) {
	w.safeExecInternal(job, false)
}

// safeExecSkipProfiler RW 模式下读 goroutine 专用（跳过同步运行路径上共享的状态）。
// 保留函数名不变避免调用点迁移；skipShared=true 供读并发路径使用。
func (w *Worker) safeExecSkipProfiler(job inf.IMailboxJob) {
	w.safeExecInternal(job, true)
}

// safeExecInternal 统一的 Job 执行逻辑：panic 恢复 + 中间件 OnComplete + Job Release。
// skipShared=true 供 RW 模式读 goroutine 调用：注入 RW 读上下文、采集读时长。
func (w *Worker) safeExecInternal(job inf.IMailboxJob, skipShared bool) {
	ctx := job.GetContext()
	mctx := job.GetMiddlewareContext()
	var execErr error
	var panicVal interface{}

	// 读 goroutine 路径：向 context 中注入 RWContextInfo，
	// 业务层可通过 ctx.Value(def.RWContextKey) 检测当前是否在 ReadOnly 上下文中执行。
	// 框架层在 Service.PostJob 中检测此标记，拒绝 ReadOnly handler 的自投递。
	if skipShared && w.env.rw.enabled.Load() {
		ctx = context.WithValue(ctx, def.RWContextKey, def.RWContextInfo{
			Mode:          def.RWModeRead,
			SourceService: w.env.invoker.GetServiceName(),
		})
	}

	defer func() {
		if r := recover(); r != nil {
			panicVal = r
			w.env.logger.WithContext(ctx).Errorf("exec error: %v\ntrace:%s", r, debug.Stack())

			// 双重保护：EscalateFailure 可能也会 panic
			func() {
				defer func() {
					if r2 := recover(); r2 != nil {
						w.env.logger.WithContext(ctx).Errorf("EscalateFailure also panicked: %v\ntrace:%s", r2, debug.Stack())
					}
				}()
				w.env.invoker.EscalateFailure(ctx, r, job)
			}()
		}

		// 调用中间件链的 OnComplete（逆序执行）
		if mctx != nil {
			w.env.middlewareChain.ExecuteOnComplete(mctx, execErr, panicVal)
		}

		// job 执行后需要释放
		if job != nil {
			job.Release()
		}
	}()

	// ---------- watchdog: 单 Job 执行超时告警 ----------
	if maxExec := w.env.rw.maxJobExecTime; maxExec > 0 {
		onExpire := func() {
			w.env.rw.longJobTotal.Add(1)
			w.env.logger.WithContext(ctx).Warnf(
				"Worker %d job execution exceeds %v: %v",
				w.workerId, maxExec, job,
			)
		}
		if wd := w.env.watchdogScheduler; wd != nil {
			// 优先使用时间轮（O(1) 入队/取消，消除 per-Job timer 堆操作）
			wdId := wd.Schedule(maxExec, onExpire)
			defer wd.Cancel(wdId)
		} else {
			// 降级：时间轮未就绪时仍使用标准库 AfterFunc
			timer := time.AfterFunc(maxExec, onExpire)
			defer timer.Stop()
		}
	}

	// ---------- 执行 Job + 读时长采集 ----------
	var readStart time.Time
	if skipShared {
		readStart = time.Now()
	}

	// 调用消息处理器
	if err := w.env.invoker.ExecuteJob(ctx, job); err != nil {
		execErr = err
	}

	if skipShared {
		elapsed := time.Since(readStart).Nanoseconds()
		w.rwReadDurationSum.Add(elapsed)
		w.rwReadCount.Add(1)
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
	sleepBo := idle.NewSpinBackoff(5 * time.Millisecond)
	for {
		// ① 优先检查 Stop 状态
		if w.state.Load() == workerStateClosed {
			w.pendingJob = job
			return
		}
		// ② 若有 pending writer，退避让步（让已有的读 goroutine 完成 RUnlock）
		// 使用 runtime.Gosched() 循环替代 time.Sleep，避免 Windows 下 15ms 最小精度问题
		if w.env.rw.writeRequested.Load() > 0 {
			for i := 0; i < yieldCount; i++ {
				runtime.Gosched()
				// 内层 spin 也响应 closed 状态，避免 Stop 延迟
				if w.state.Load() == workerStateClosed {
					w.pendingJob = job
					return
				}
				if w.env.rw.writeRequested.Load() == 0 {
					break
				}
			}
			if yieldCount < maxYieldCount {
				yieldCount *= 2
			}
			continue
		}
		// ③ 信号量令牌获取（非阻塞尝试，失败则退避后重新进入循环）
		if w.env.rw.readSem != nil {
			select {
			case w.env.rw.readSem <- struct{}{}:
				// 获取令牌成功
			default:
				// 令牌已满，先 Gosched 让步，达到上限后改用阶梯 sleep，避免 CPU 燃烧
				if yieldCount < maxYieldCount {
					for i := 0; i < yieldCount; i++ {
						runtime.Gosched()
						if w.state.Load() == workerStateClosed {
							w.pendingJob = job
							return
						}
					}
					yieldCount *= 2
				} else {
					sleepBo.Sleep()
				}
				continue
			}
		}
		break
	}

	// 获取读锁
	w.env.rw.mu.RLock()

	// 【关键：RLock-after-check 重检查（§10.14 动态开关安全协议）】
	// 防止在 SetRWEnabled(false) 切换窗口内误 spawn 读 goroutine
	if !w.env.rw.enabled.Load() {
		w.env.rw.mu.RUnlock()
		// 必须释放轮询循环中已获取的信号量令牌
		if w.env.rw.readSem != nil {
			<-w.env.rw.readSem
		}
		w.safeExec(job) // 降级为串行执行
		return
	}

	// 【关键时序】inflightReads.Add 必须在 spawn goroutine 之前、
	// 在 RLock + enableRW 重检查之后同步执行
	w.inflightReads.Add(1)
	w.inflightReadCnt.Add(1)  // 可观测性：暂存当前 in-flight 读数量
	w.env.rw.readTotal.Add(1) // 全局读计数

	readFunc := func() {
		// WaitGroup + 锁 + 信号量泄漏防护
		var rlockReleased atomic.Bool
		defer func() {
			// 归还信号量令牌
			if w.env.rw.readSem != nil {
				<-w.env.rw.readSem
			}
			// 释放读锁（必须在 Done 之前）
			if rlockReleased.CompareAndSwap(false, true) {
				w.env.rw.mu.RUnlock()
			}
			// 可观测性：in-flight 读数量递减
			w.inflightReadCnt.Add(-1)
			// WaitGroup Done（最后释放）
			w.inflightReads.Done()
		}()

		w.safeExecSkipProfiler(job) // 读 goroutine 跳过共享 Profiler
	}

	// 优先使用读 goroutine 池，失败则 fallback 到裸 goroutine
	if w.env.rw.readPool != nil {
		if err := w.env.rw.readPool.Go(readFunc); err != nil {
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
	w.env.rw.writeTotal.Add(1)   // 全局写计数
	writeWaitStart := time.Now() // 写等待计时开始
	w.env.rw.writeRequested.Add(1)
	bo := idle.NewSpinBackoff(5 * time.Millisecond)
	for !w.env.rw.mu.TryLock() {
		if w.state.Load() == workerStateClosed {
			w.env.rw.writeRequested.Add(-1)
			// Worker 正在停止但无法获取写锁，暂存到 pendingJob 由 Drain 处理
			w.pendingJob = job
			return
		}
		bo.Backoff()
	}
	// 写等待耗时指标
	writeWait := time.Since(writeWaitStart)
	w.rwWriteWaitSum.Add(writeWait.Nanoseconds())
	w.rwWriteWaitCount.Add(1)
	if writeWait > 100*time.Millisecond {
		w.env.logger.Warnf("Worker %d write lock wait %v (>100ms), possible long-running readers",
			w.workerId, writeWait)
	}
	// defer 保证 Unlock 在 Add(-1) 之前执行（LIFO）
	// 确保 writeRequested.Add(-1) 在 Unlock 之后：
	// 读路径看到 writeRequested==0 时 WLock 必定已释放
	defer w.env.rw.writeRequested.Add(-1)
	defer w.env.rw.mu.Unlock()
	w.safeExec(job)
}

// GetJobLen 获取队列中的任务总数
func (w *Worker) GetJobLen() int {
	if w.queueManager == nil {
		return 0
	}
	return w.queueManager.GetJobLen()
}
