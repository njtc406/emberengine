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

	// 【P1-3】预构的 RWContextInfo，SourceService 在 Pool 生命周期内是常量，
	// 避免 execRead 路径上每条 Job 重建 struct + 字符串拷贝。
	rwReadCtxInfo def.RWContextInfo
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

	// ---- ADR-3 / P0-3：RW 读路径解耦 ----
	// readCh: 主循环 dequeue 到读 Job 后非阻塞投递的通道；readPipeline 负责消费。
	// 容量满 = ADR-3 入队侧回压：主循环直接 OnJobDiscarded(ErrMailboxWorkerIsFull)，
	// 不再让读 Job 在主循环里同步等待 gate（writeRequested/readSem）。
	readCh         chan inf.IMailboxJob // nil = RW 未启用或运行时尚未初始化（execRead 走兜底同步路径）
	readPipelineWg sync.WaitGroup       // 跟踪 readPipeline goroutine 退出
	// readsDispatched：主循环投递到 readCh 的累计读 Job 数（含被 ADR-3 回压丢弃的）
	// readsLaunched：readPipeline 已完成 RLock + inflightReads.Add 的累计读 Job 数
	// 用途：execWrite 在 mu.WLock 之前必须先等待 readsLaunched.Load() 追平
	// 写 Job dequeue 时刻的 readsDispatched 快照，保证"先序读已注册到 inflightReads"，
	// 否则 mu.WLock 可能抢在 readPipeline 的 mu.RLock 之前，破坏 read-before-write 顺序。
	readsDispatched atomic.Uint64
	readsLaunched   atomic.Uint64
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

	// ADR-3 / P0-3：RW 启用时为读路径创建独立通道
	// 容量取 max(ReadDispatchChanCap, MaxConcurrentReads, 256)，留足缓冲避免无谓回压
	if conf.EnableRWMode {
		capN := conf.ReadDispatchChanCap
		if capN <= 0 {
			capN = conf.MaxConcurrentReads
		}
		if capN < 256 {
			capN = 256
		}
		w.readCh = make(chan inf.IMailboxJob, capN)
	}

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
	// ADR-3 / P0-3：RW 启用时启动专职的读派发流水线
	if w.readCh != nil {
		w.readPipelineWg.Add(1)
		go w.runReadPipeline()
	}
}

// run 是 Worker 的主循环。
//
// 循环逻辑：
//  1. 尝试从 queueManager.NextJob() 获取下一个事件；
//  2. 若获取成功，根据 enableRW 决定走 RW 或串行路径；
//  3. 若当前没有事件，则调用 idler.Idle() 进行条件等待或退避；
//  4. 当 closed 标记为 true 时，循环退出，并在 defer 中处理所有残留事件。
//
// 【ADR-3 / P0-3 重构】RW 模式下，主循环不再同步执行读 Job 的 gate spin / RLock /
// goroutine spawn——而是把读 Job 投递到 readCh 由 readPipeline goroutine 异步处理。
// 主循环仅负责：① 写 Job 同步执行（走 execWrite，内部等待先序读注册后取 WLock）；
// ② 读 Job 投递到 readCh（满则按 ADR-3 入队侧回压语义直接 OnJobDiscarded）；
// ③ 系统消息（priority queue 优先返回）由主循环立即处理，不再被 read gate 阻塞。
func (w *Worker) run() {
	defer w.wg.Done()

	// 退出时：先关闭 readCh 让 readPipeline 排空 + 退出，
	// 再等待本 Worker 的 in-flight 读完成（带超时兜底），最后在 WLock 下 drain 残留消息
	defer func() {
		// ① 【ADR-3】关闭 readCh，等待 readPipeline 处理完通道里残留的读 Job 后退出。
		//    readPipeline 在 launchRead 内部检测到 state==closed 时会按 drainPolicy
		//    discardExec 这些已投递但未注册的读 Job，保证 OnJobDiscarded 对称（ADR-4）。
		if w.readCh != nil {
			close(w.readCh)
			w.readPipelineWg.Wait()
		}

		// ② 等待本 Worker 的 in-flight 读 goroutine 完成（带超时保护）
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
	// 【P1-3】复用 WorkerEnv 中预构的 RWContextInfo，避免热路径 struct 重建。
	if skipShared && w.env.rw.enabled.Load() {
		ctx = context.WithValue(ctx, def.RWContextKey, w.env.rwReadCtxInfo)
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

// execWithRW 根据 Job 的 RWMode 走读派发或同步写。
// 【ADR-3 / P0-3】读 Job 不再在主循环里同步取 gate/RLock，改为投递到 readPipeline。
func (w *Worker) execWithRW(job inf.IMailboxJob) {
	if getRWMode(job) == def.RWModeRead {
		w.dispatchRead(job)
	} else {
		w.execWrite(job)
	}
}

// dispatchRead 主循环侧：把读 Job 投递到 readPipeline。
//
// 【ADR-3 关键】不在此处取 gate（writeRequested/readSem）/RLock/spawn，
// 让出主循环 CPU 给后续写 Job 与系统消息（priority queue 优先返回 SysCtl 等）。
//
// 顺序契约（read-vs-write）：
//   - 同 dispatcherKey 的 Job 由 hashring 路由到同一 Worker；
//   - 主循环按 mpsc 出队顺序处理：Read1（投 readCh）→ Write2（execWrite 同步）；
//   - execWrite 在 mu.WLock 之前先等待 readsLaunched 追平 readsDispatched 快照，
//     即"先序读已完成 inflightReads.Add + mu.RLock"，避免写抢在读 spawn 之前；
//   - 这保证 Read1 看到 Write2 写入之前的状态，Write2 看到 Read1 完成之后的状态。
//
// 回压（readCh 满）：按 ADR-3 入队侧失败语义直接 OnJobDiscarded(ErrMailboxWorkerIsFull)，
// 由 ADR-4 调用方 invoker 感知，不再让主循环热自旋。
func (w *Worker) dispatchRead(job inf.IMailboxJob) {
	// 先尝试非阻塞投递（绝大多数情况下 readCh 不会满）
	select {
	case w.readCh <- job:
		w.readsDispatched.Add(1)
		return
	default:
	}

	// readCh 满 = ADR-3 入队侧回压：直接丢弃 + 通知业务（OnJobDiscarded 与 ADR-4 对称）
	w.env.rw.drainDiscardTotal.Add(1)
	w.env.logger.WithContext(job.GetContext()).Warnf(
		"Worker %d: readCh full (cap=%d), discard read job (ADR-3 backpressure)",
		w.workerId, cap(w.readCh))

	// OnJobDiscarded 必须在 mctx 归池之前调用，避免业务读 mctx 时已被回收
	mctx := job.GetMiddlewareContext()
	w.env.invoker.OnJobDiscarded(job, def.ErrMailboxWorkerIsFull)
	if mctx != nil {
		w.env.middlewareChain.ExecuteOnComplete(mctx, def.ErrMailboxWorkerIsFull, nil)
	}
	job.Release()
}

// runReadPipeline 是 readPipeline goroutine 主循环。
//
// 【ADR-3】专职处理读 Job 的 gate spin（writeRequested / readSem）+ RLock + spawn 真正执行 goroutine。
// 主循环退出时会 close(readCh)，本 goroutine 依次处理通道里残留的读 Job 后退出；
// 残留读 Job 的处理：launchRead 内部检测到 state==closed 时按 drainPolicy 走 discardExec，
// 与 ADR-4 OnJobDiscarded 契约保持对称。
func (w *Worker) runReadPipeline() {
	defer w.readPipelineWg.Done()
	for job := range w.readCh {
		w.launchRead(job)
	}
}

// launchRead readPipeline 侧：执行原 execRead 的 gate spin + RLock + spawn 全部逻辑。
//
// 关键不变量：
//   - inflightReads.Add(1) + readsLaunched.Add(1) 必须在 mu.RLock 之后、spawn goroutine 之前
//     完成，让 execWrite 能可靠等待"先序读已注册"；
//   - readsLaunched 单调递增，与 readsDispatched 配对（包括降级路径也要算入，
//     否则 execWrite 永远等不到追平）。
//
// Stop 处理（DrainExecute 语义）：
//   - main run defer 在 readPipelineWg.Wait 之前会 close(readCh)，使 runReadPipeline 排空通道里所有残留读 Job 再退出；
//   - launchRead 在 stop 后**不做早退**：main run 退出时 writer 一定已释放（writeRequested→0），
//     gate ① 立刻通过；readSem 由已 spawn 的 readFunc 归还，gate ② 至多 backoff 至 readDuration；
//     最终所有残留读都被 spawn → main run defer 的 inflightReads.Wait（带 stopTimeout 兜底）等其完成。
//   - 这与 ADR-3 + DrainExecute 语义一致：通道残留的读不丢；只有 stopTimeout 超时才会进入 unsafe drain 路径。
func (w *Worker) launchRead(job inf.IMailboxJob) {
	bo := idle.NewSpinBackoff(5 * time.Millisecond)
	for {
		// gate ①：让步 pending writer，避免写饥饿。
		// stop 后 main run 已退出，无新 writer；剩余 writeRequested 会在原 execWrite 释放 WLock 后归 0。
		if w.env.rw.writeRequested.Load() > 0 {
			bo.Backoff()
			continue
		}
		// gate ②：信号量令牌（非阻塞 try-send）。
		// readSem 由前序已 spawn 的 readFunc 归还，最长等待 ≈ 一次 read 业务执行时长。
		if w.env.rw.readSem != nil {
			select {
			case w.env.rw.readSem <- struct{}{}:
				// 获取令牌成功
			default:
				bo.Backoff()
				continue
			}
		}
		break
	}

	// 获取读锁
	w.env.rw.mu.RLock()

	// 【关键：RLock-after-check 重检查（§10.14 动态开关安全协议）】
	// 防止在 SetRWEnabled(false) 切换窗口内误 spawn 读 goroutine。
	// 降级路径仍要 +1 readsLaunched（保持单调性，详见函数注释）。
	if !w.env.rw.enabled.Load() {
		w.env.rw.mu.RUnlock()
		if w.env.rw.readSem != nil {
			<-w.env.rw.readSem
		}
		w.safeExec(job)
		w.readsLaunched.Add(1)
		return
	}

	// 【关键时序】inflightReads.Add 必须在 spawn goroutine 之前、
	// 在 RLock + enableRW 重检查之后同步执行。
	// readsLaunched.Add 与 inflightReads.Add 紧邻：execWrite 看到 readsLaunched
	// 追平后，再 mu.WLock 即可正确等待这些 inflight readers。
	w.inflightReads.Add(1)
	w.inflightReadCnt.Add(1)
	w.env.rw.readTotal.Add(1)
	w.readsLaunched.Add(1)

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

// execWrite 写操作：等待先序读注册 → mu.WLock → 独占执行。
//
// 【ADR-3 顺序保证】在 mu.WLock 之前必须先等待 readsLaunched.Load() 追平
// 进入函数时刻的 readsDispatched 快照，否则 mu.WLock 可能抢在 readPipeline
// 的 mu.RLock 之前，破坏 read-before-write 的提交顺序。
//
// 等待的代价：通常远小于 RLock + 业务 Read 执行时长（读 Job 已 dequeue 仅差
// readPipeline 的 gate 阶段）；read gate 内部最多停留 5ms（SpinBackoff 上限）。
func (w *Worker) execWrite(job inf.IMailboxJob) {
	// ① 等待主循环投递到 readPipeline 的所有先序读完成 RLock + inflightReads.Add
	target := w.readsDispatched.Load()
	if target > w.readsLaunched.Load() {
		bo := idle.NewSpinBackoff(5 * time.Millisecond)
		for w.readsLaunched.Load() < target {
			if w.state.Load() == workerStateClosed {
				w.pendingJob = job
				return
			}
			bo.Backoff()
		}
	}

	// ② 取 mu.WLock，沿用原 TryLock + 退避 + closed 兜底语义
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
