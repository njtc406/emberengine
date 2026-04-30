// Package mailbox
// 模块名: 工作线程池
// 功能描述: 维护一组 Worker 的拓扑（基于 jump consistent hash 的调度环 + COW 快照），负责 Job 派发、扩缩容、RW 模式安全切换与统计聚合。
// 作者:  yr  2025/7/19
// 最后更新:  yr  2026/4/27
package mailbox

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"runtime/debug"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
)

// ErrRWDisableTimeout SetRWEnabled(false) 超时，有泄漏的读 goroutine
var ErrRWDisableTimeout = errors.New("RW disable timeout: leaked read goroutines prevent safe switch")

// ErrRWDynamicEnableUnsupported 表示 RW 模式只支持通过配置在 Worker 创建时启用。
var ErrRWDynamicEnableUnsupported = errors.New("RW dynamic enable unsupported: enable RW mode in mailbox config before start")

// AutoScaler 采样间隔掩码：每 (mask+1) 次 dispatch 采样一次 worker.GetJobLen 决定是否触发扩容。
// 128 条 dispatch 间隔在 100k+ QPS 下采样频率 >700Hz，扩容延迟可控；ticker 兜底保证最坏延迟 ≤ ResizeCoolDown。
const dispatchSampleMask uint64 = 127

// panicRateLimiter 一个极简 token-bucket，仅用于 panic handler 的堆栈采集限速。
// 默认每秒最多 5 条堆栈，初始可立即输出 5 条；超额请求只输出 panic 信息不带 trace。
//
// 设计取舍：直接复用 golang.org/x/time/rate 会引入额外依赖与一次 Allow() 内的
// time.Now()，对 panic 这种本就低频路径过度；这里用 atomic + Unix nano 自旋。
type panicRateLimiter struct {
	lastNS    atomic.Int64 // 上次补充 token 的时间（纳秒）
	tokens    atomic.Int64 // 当前可用 token 数
	burstCap  int64        // 桶容量
	intervalN int64        // 每补一个 token 的时间间隔（纳秒）
}

func (l *panicRateLimiter) Allow() bool {
	if l.burstCap <= 0 {
		// 默认配置：5 token / 秒，桶容量 5
		l.burstCap = 5
		l.intervalN = int64(time.Second / 5)
		l.tokens.Store(l.burstCap)
		l.lastNS.Store(time.Now().UnixNano())
	}
	now := time.Now().UnixNano()
	last := l.lastNS.Load()
	if delta := now - last; delta >= l.intervalN {
		add := delta / l.intervalN
		// CAS 推进 lastNS，失败说明并发已推进，跳过补 token 也可接受
		if l.lastNS.CompareAndSwap(last, last+add*l.intervalN) {
			cur := l.tokens.Add(add)
			if cur > l.burstCap {
				l.tokens.Add(l.burstCap - cur)
			}
		}
	}
	for {
		cur := l.tokens.Load()
		if cur <= 0 {
			return false
		}
		if l.tokens.CompareAndSwap(cur, cur-1) {
			return true
		}
	}
}

type IScaler interface {
	ShouldResize(current int, workers []inf.IMailboxWorker) (newSize int32, reason string, ok bool)
}

// workersSnapshot 是 WorkerPool 拓扑（workers/ring/dispatchCnt）的不可变快照。
//
// 通过 atomic.Pointer[workersSnapshot] 发布，DispatchJob 等热路径
// 全程无锁；扩缩容由 WorkerPool.mu 串行化，构建好新的快照后一次性 Store 发布。
//
// 字段语义：
//   - workers    : 当前活跃 worker 集合（id -> worker），构建后只读；
//   - ring       : 调度环（jump consistent hash），构建后只读；
//   - dispatchCnt: 每 worker 的事件计数（仅 statsEnabled 时填充），计数器指针在
//     COW 替换时由旧快照复制到新快照，保证统计连续；
//   - sole/soleID: 单 worker 快路径缓存，省去 map 迭代；
//   - count      : len(workers)。
type workersSnapshot struct {
	workers     map[int32]inf.IMailboxWorker
	ring        *dispatchRing
	dispatchCnt map[int32]*atomic.Uint64
	sole        inf.IMailboxWorker
	soleID      int32
	count       int
}

type WorkerPool struct {
	conf            *config.MailboxConf
	mu              sync.Mutex // 仅作 resize/publish 串行化，dispatch 不持有
	wg              sync.WaitGroup
	ctx             context.Context
	cancel          context.CancelFunc
	snap            atomic.Pointer[workersSnapshot] // 拓扑快照，COW 发布
	invoker         inf.IMessageInvoker             // 消息处理器
	middlewareChain *MiddlewareChain                // 中间件链
	autoScaler      IScaler                         // 自动扩容器
	logger          log.ILoggerX
	workerCount     atomic.Int32 // 当前 worker 数量（用于扩缩容），atomic 以支持 RW 模式下无锁读取

	// 停机时队列处理策略（由 Mailbox 下发）
	drainPolicy DrainPolicy

	// ---- RW 读写分离控制器 ----
	rw *RWController

	// Debug-only dispatch distribution stats.
	statsEnabled  bool // 是否开启统计（仅在 Debug 模式下）
	statsInterval time.Duration

	// ---- AutoScaler 事件驱动触发 ----
	scaleTrigger chan struct{} // 容量 1，非阻塞通知 autoScaleWorkers

	// DispatchJob 采样计数器，单调递增；与 dispatchSampleMask 配合用于
	// 降低 worker.GetJobLen 调用频率（每 mask+1 条采样一次）。
	dispatchSampleCnt atomic.Uint64

	// ---- Worker ID 分配器（单调递增，避免缩-扩后 ID 复用导致监控差分异常）----
	nextWorkerID atomic.Int32

	// ---- watchdog 时间轮（per-WorkerPool，替代 per-Job time.AfterFunc）----
	// 仅在 MaxJobExecutionTime > 0 时初始化。
	// 当前采用 WorkerPool 独立 TimingWheel，生命周期与 WorkerPool 对齐。
	watchdogTW        *timingwheel.TimingWheel
	watchdogScheduler timingwheel.ITimerScheduler
	watchdogAdapter   watchdogCanceler // 供 WorkerEnv 传递给 Worker
	watchdogWg        sync.WaitGroup   // 独立跟踪 watchdog consumer goroutine。
	// 不走 p.wg：其 channel 要到 Wait() 阶段 scheduler.Stop() 才会关闭，
	// 如果走 p.wg 会导致 BeginStop 的 p.wg.Wait() 与 watchdog 形成死锁。

	// panic 堆栈采集限速器，避免 panic storm 场景下 debug.Stack() 成为 CPU 热点。
	panicStackLimiter panicRateLimiter
}

func (p *WorkerPool) SetDrainPolicy(policy DrainPolicy) {
	p.drainPolicy = policy
}

func NewWorkerPool(conf *config.MailboxConf, logger log.ILoggerX, invoker inf.IMessageInvoker, middlewares ...inf.IMailboxMiddleware) (*WorkerPool, error) {
	if invoker == nil {
		return nil, errors.New("invoker is nil")
	}
	conf = fixConf(conf)
	ctx, cancel := context.WithCancel(context.Background())
	pool := &WorkerPool{
		conf:            conf,
		invoker:         invoker,
		middlewareChain: NewMiddlewareChain(middlewares...),
		ctx:             ctx,
		cancel:          cancel,
		logger:          logger,
		statsInterval:   10 * time.Second,
		scaleTrigger:    make(chan struct{}, 1),
	}
	pool.middlewareChain.SetPanicHandler(func(phase, middleware string, mctx inf.IMiddlewareContext, panicVal interface{}) {
		if pool.logger == nil {
			return
		}
		// debug.Stack() 自身开销可观（µs 级），panic storm 场景下高频触发会成为
		// CPU 热点；用 token-bucket 限速堆栈采集，超额只记 panic 信息不带堆栈。
		if pool.panicStackLimiter.Allow() {
			pool.logger.Errorf("mailbox middleware panic: phase=%s middleware=%s service=%s panic=%v\ntrace:%s",
				phase, middleware, invoker.GetServiceName(), panicVal, debug.Stack())
		} else {
			pool.logger.Errorf("mailbox middleware panic: phase=%s middleware=%s service=%s panic=%v (stack suppressed by rate limit)",
				phase, middleware, invoker.GetServiceName(), panicVal)
		}
	})

	// ---- RW 控制器初始化 ----
	rw, err := newRWController(conf)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create rw controller: %w", err)
	}
	pool.rw = rw

	return pool, nil
}

// workerEnv 构建当前 WorkerPool 对应的 WorkerEnv（Worker 的运行时依赖）
func (p *WorkerPool) workerEnv() *WorkerEnv {
	return &WorkerEnv{
		logger:            p.logger,
		invoker:           p.invoker,
		middlewareChain:   p.middlewareChain,
		rw:                p.rw,
		watchdogScheduler: p.watchdogAdapter,
		// 预构 RWContextInfo，SourceService 在 Pool 生命周期内不变
		rwReadCtxInfo: def.RWContextInfo{
			Mode:          def.RWModeRead,
			SourceService: p.invoker.GetServiceName(),
		},
		// SubmitJob 计数开关：与 dispatch 分布统计同步
		statsEnabled: p.statsEnabled,
	}
}

// watchdogTimerAdapter 将 timingwheel.ITimerScheduler 适配为 watchdogCanceler 接口。
type watchdogTimerAdapter struct {
	scheduler timingwheel.ITimerScheduler
}

func (a *watchdogTimerAdapter) Schedule(d time.Duration, onExpire func()) uint64 {
	if a == nil || a.scheduler == nil || d <= 0 {
		return 0
	}
	id, err := a.scheduler.AfterFunc(d, "watchdog", func(_ context.Context, _ *timingwheel.Timer, _ ...interface{}) error {
		onExpire()
		return nil
	})
	if err != nil {
		return 0
	}
	return id
}

func (a *watchdogTimerAdapter) Cancel(id uint64) {
	if a == nil || a.scheduler == nil || id == 0 {
		return
	}
	a.scheduler.CancelTimer(id)
}

func (p *WorkerPool) Start() error {
	// ---- watchdog 时间轮初始化 ----
	if p.rw.maxJobExecTime > 0 {
		tw := timingwheel.NewTimingWheel(100*time.Millisecond, 64, p.logger)
		tw.Start()
		p.watchdogTW = tw
		scheduler, err := timingwheel.NewJobScheduler("mailbox-watchdog", 4096, 4, tw, p.logger)
		if err != nil {
			p.logger.Warnf("Failed to create watchdog scheduler: %v, fallback to time.AfterFunc", err)
		} else {
			p.watchdogScheduler = scheduler
			p.watchdogAdapter = &watchdogTimerAdapter{scheduler: scheduler}
			// 消费者 goroutine：从 channel 取出过期 Timer 并执行回调。
			// 【关闭顺序修复】不挂在 p.wg 上：channel 由 Wait() 阶段的
			// scheduler.Stop() 才关闭，如果挂在 p.wg 上会让 BeginStop 的
			// p.wg.Wait() 与 watchdog 形成死锁（BeginStop 等 consumer 退出，
			// consumer 等 channel 关闭，channel 关闭要等 Wait 阶段）。
			// 改用独立 watchdogWg，在 Wait() 里 scheduler.Stop() 之后再 join。
			p.watchdogWg.Add(1)
			go func() {
				defer p.watchdogWg.Done()
				ch := scheduler.GetTimerCbChannel()
				for t := range ch {
					t.Do(p.ctx)
				}
			}()
		}
	}

	p.mu.Lock()
	initial := p.conf.SchedulePolicy.InitialWorkerNum
	workers := make(map[int32]inf.IMailboxWorker, initial)
	dispatchCnt := make(map[int32]*atomic.Uint64, initial)
	ids := make([]int32, 0, initial)
	for n := int32(0); n < initial; n++ {
		id := p.nextWorkerID.Add(1) - 1
		worker := newWorker(id, p.conf, p.workerEnv(), p.drainPolicy)
		workers[id] = worker
		worker.Start()
		ids = append(ids, id)
		if p.statsEnabled {
			dispatchCnt[id] = &atomic.Uint64{}
		}
	}
	p.workerCount.Store(initial)
	p.publishSnapshotLocked(workers, newDispatchRing(ids), dispatchCnt)
	p.mu.Unlock()

	// 启动中间件链
	p.middlewareChain.Start()

	if p.conf.SchedulePolicy.EnableAutoScaling {
		p.wg.Add(1)
		go p.autoScaleWorkers()
	}

	if p.statsEnabled {
		p.wg.Add(1)
		go p.logDispatchStatsLoop()
	}

	p.logger.Debugf("Started service[%s] mailbox workers:%d", p.invoker.GetServiceName(), p.conf.SchedulePolicy.InitialWorkerNum)
	return nil
}

// BeginStop 发起停止：先等待后台协程退出，再通知 workers 退出。
func (p *WorkerPool) BeginStop() {
	// 先关闭自动扩容/统计等后台协程
	p.cancel()
	// 等待后台协程退出，避免与 shrink/dispatch 等竞争（不涉及 worker 自等问题）
	p.wg.Wait()

	snap := p.snap.Load()
	if snap == nil {
		return
	}
	for _, w := range snap.workers {
		w.BeginStop()
	}
}

// Wait 等待 worker 全部退出，并完成中间件链停止与资源清理。
func (p *WorkerPool) Wait() {
	snap := p.snap.Load()
	if snap == nil {
		return
	}
	for _, w := range snap.workers {
		w.Wait()
	}

	// 停止中间件链（需要在 workers 完全退出后，避免 DrainDiscard 时仍调用 OnComplete）
	p.middlewareChain.Stop()

	// 在锁内把快照清空并取出 readPool 引用，锁外再调用 Release。
	// 这样避免在持锁状态下执行 ants Release 的内部逻辑。
	p.mu.Lock()
	if p.snap.Load() == nil {
		p.mu.Unlock()
		return
	}
	p.snap.Store(nil)
	releasePool := p.rw.readPool
	p.rw.readPool = nil
	p.mu.Unlock()

	if releasePool != nil {
		releasePool.Release()
	}

	// 停止 watchdog 时间轮（先 Stop scheduler 关闭 channel，consumer goroutine 自动退出）
	if p.watchdogScheduler != nil {
		p.watchdogScheduler.Stop()
		// channel 已关闭，等待 consumer goroutine 退出后再清理引用
		p.watchdogWg.Wait()
		p.watchdogScheduler = nil
		p.watchdogAdapter = nil
	}
	if p.watchdogTW != nil {
		p.watchdogTW.Stop()
		p.watchdogTW = nil
	}
}

// Stop 便捷方法：BeginStop + Wait。
func (p *WorkerPool) Stop() {
	p.BeginStop()
	p.Wait()
}

// DispatchJob 将任务分派给具体 worker。
//
//   - 多 worker 模式：通过 ring.Get(evt.GetDispatcherKey()) 选择 worker，保证相同 dispatcherKey 的事件落到同一 worker；
//   - 单 worker 模式：通过 snapshot.sole 直接命中，行为接近 Actor 模型的串行执行。
//   - mctx: 中间件上下文，用于在消息处理完成后调用 OnComplete 回调。
//
// 走 atomic.Pointer[workersSnapshot] 全程无锁；扩缩容期间快照尚未替换前
// 仍按当前快照投递，新快照 publish 后立即对新的 dispatch 生效。
// 缩容路径遵循“BeginStop+Wait 老 worker → publish 移除其 id”的发布协议，
// 保证 per-key 顺序契约。
func (p *WorkerPool) DispatchJob(job inf.IMailboxJob) error {
	snap := p.snap.Load()
	if snap == nil || snap.count == 0 {
		p.logger.WithContext(job.GetContext()).Errorf("WorkerPool not started or already stopped")
		return def.ErrMailboxWorkerNotFound
	}

	var worker inf.IMailboxWorker
	var workerID int32
	if snap.count == 1 {
		// 单 worker 快路径：避免 hashring 计算与 map 查找
		worker = snap.sole
		workerID = snap.soleID
	} else {
		var ok bool
		workerID, ok = snap.ring.Get(job.GetDispatcherKey())
		if !ok {
			p.logger.WithContext(job.GetContext()).Errorf("No worker available in hash ring")
			return def.ErrMailboxWorkerIsFull
		}
		worker, ok = snap.workers[workerID]
		if !ok {
			p.logger.WithContext(job.GetContext()).Errorf("Worker %d not found", workerID)
			return def.ErrMailboxWorkerNotFound
		}
	}

	if p.statsEnabled {
		if cnt := snap.dispatchCnt[workerID]; cnt != nil {
			cnt.Add(1)
		}
	}

	// AutoScaler 事件驱动采用采样触发：每 dispatchSampleMask+1 条采样一次（128 条），
	// 定时 ticker 兜底保证扩容延迟上限。
	// 取舍：积压响应延迟 ≈ 127 条 dispatch + 0.x 个 ticker 周期，对扩容决策可接受；
	// 减少 99% 的 GetJobLen 调用与 scaleTrigger 竞争。
	if p.conf.SchedulePolicy.EnableAutoScaling {
		if p.dispatchSampleCnt.Add(1)&dispatchSampleMask == 0 && worker.GetJobLen() > 0 {
			select {
			case p.scaleTrigger <- struct{}{}:
			default: // 已有信号待处理，跳过
			}
		}
	}

	return worker.SubmitJob(job)
}

// publishSnapshotLocked 由调用方持 p.mu 的写流程使用，构建 workersSnapshot 并发布。
//
// 入参 workers / dispatchCnt 在调用后归 snapshot 所有，调用方不应再修改；ring 同理。
// 单 worker 时填充 sole/soleID 快路径字段。
func (p *WorkerPool) publishSnapshotLocked(
	workers map[int32]inf.IMailboxWorker,
	ring *dispatchRing,
	dispatchCnt map[int32]*atomic.Uint64,
) {
	snap := &workersSnapshot{
		workers:     workers,
		ring:        ring,
		dispatchCnt: dispatchCnt,
		count:       len(workers),
	}
	if snap.count == 1 {
		for id, w := range workers {
			snap.sole = w
			snap.soleID = id
			break
		}
	}
	p.snap.Store(snap)
}

// resizeWorkers 调整 worker 数量。整个流程持 p.mu，但 dispatch 路径
// 走 snapshot.Load 不受影响；扩容/缩容均通过构建新快照后一次 publish 替换。
//
// 关于 p.mu 全程持有：
//   - dispatch 全程无锁，因此持 mu 不会阻塞热路径；
//   - autoScaler 也走 p.mu，与本流程互斥，避免并发 resize；
//   - SetRWEnabled 与 resize 同样走 p.mu 串行（RW 仅支持运行时关闭）。
func (p *WorkerPool) resizeWorkers(newSize int32) {
	currentCount := p.workerCount.Load()
	if newSize == currentCount {
		return
	}

	if newSize > currentCount {
		// 扩容：构建新快照（复制旧 workers/dispatchCnt + 新增 worker + 重建 ring）
		p.mu.Lock()
		defer p.mu.Unlock()

		old := p.snap.Load()
		if old == nil {
			return // 已停止
		}

		maxWorkers := int32(0)
		if p.conf != nil && p.conf.SchedulePolicy != nil && p.conf.SchedulePolicy.ScalingStrategy != nil {
			maxWorkers = p.conf.SchedulePolicy.ScalingStrategy.MaxWorkerNum
		}
		if maxWorkers > 0 && newSize > maxWorkers {
			newSize = maxWorkers
		}
		if newSize <= currentCount {
			return // clamp 后无需扩容
		}

		newWorkers := make(map[int32]inf.IMailboxWorker, int(newSize))
		newDispatchCnt := make(map[int32]*atomic.Uint64, int(newSize))
		for id, w := range old.workers {
			newWorkers[id] = w
		}
		for id, c := range old.dispatchCnt {
			newDispatchCnt[id] = c
		}
		newIDs := make([]int32, 0, int(newSize))
		for id := range old.workers {
			newIDs = append(newIDs, id)
		}
		for n := newSize - currentCount; n > 0; n-- {
			id := p.nextWorkerID.Add(1) - 1
			worker := newWorker(id, p.conf, p.workerEnv(), p.drainPolicy)
			newWorkers[id] = worker
			worker.Start()
			newIDs = append(newIDs, id)
			if p.statsEnabled {
				newDispatchCnt[id] = &atomic.Uint64{}
			}
		}
		p.workerCount.Store(newSize)
		p.publishSnapshotLocked(newWorkers, newDispatchRing(newIDs), newDispatchCnt)
		return
	}

	// 缩容：保证 dispatcherKey 顺序契约 + 全流程
	// 串行化但不阻塞 dispatch。
	//
	// 流程：
	//   ① 持 p.mu 选定淘汰 ids（dispatch 走 snapshot 不受影响）；
	//   ② 仍持 p.mu：BeginStop → Wait 老 worker drain 完毕。这期间 snapshot
	//      未替换，DispatchJob 仍可能命中老 worker，老 worker 处于 closing
	//      状态，SubmitJob 返回 ErrMailboxWorkerClosed，由 Mailbox.PostJob
	//      统一走 OnJobDiscarded（业务可感知，不会与残留并发）；
	//   ③ 构建剔除淘汰 ids 后的新 workers/ring/dispatchCnt 快照并 publish；
	//      此后新 Job 才被 rehash 到新 owner——老 key 的执行已彻底结束。
	//
	// 持 mu 全程的开销：仅阻塞其他 resize 与 SetRWEnabled，dispatch 完全不受影响。
	p.mu.Lock()
	defer p.mu.Unlock()

	old := p.snap.Load()
	if old == nil {
		return
	}

	toRemove := int(currentCount - newSize)
	ids := make([]int32, 0, len(old.workers))
	for id := range old.workers {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if len(ids) > int(newSize) {
		ids = ids[newSize:]
	} else {
		ids = nil
	}
	if len(ids) > toRemove {
		ids = ids[:toRemove]
	}

	removedWorkers := make([]inf.IMailboxWorker, 0, len(ids))
	removedSet := make(map[int32]struct{}, len(ids))
	for _, id := range ids {
		if worker, exists := old.workers[id]; exists {
			removedWorkers = append(removedWorkers, worker)
			removedSet[id] = struct{}{}
		}
	}

	if len(removedWorkers) == 0 {
		return
	}

	// ① 通知老 worker 拒绝新 SubmitJob（CAS 到 closing 状态）。
	for _, w := range removedWorkers {
		w.BeginStop()
	}
	// ② 等待 drain 完成（按 DrainPolicy：DrainExecute 串行执行 / DrainDiscard 仅回收）。
	for _, w := range removedWorkers {
		w.Wait()
	}

	// ③ 老 worker 已彻底退出后构建新快照（剔除 removedSet）。
	newWorkers := make(map[int32]inf.IMailboxWorker, len(old.workers)-len(removedSet))
	newDispatchCnt := make(map[int32]*atomic.Uint64, len(old.dispatchCnt))
	for id, w := range old.workers {
		if _, drop := removedSet[id]; drop {
			continue
		}
		newWorkers[id] = w
	}
	for id, c := range old.dispatchCnt {
		if _, drop := removedSet[id]; drop {
			continue
		}
		newDispatchCnt[id] = c
	}
	newIDs := make([]int32, 0, len(newWorkers))
	for id := range newWorkers {
		newIDs = append(newIDs, id)
	}
	p.workerCount.Store(int32(newSize))
	p.publishSnapshotLocked(newWorkers, newDispatchRing(newIDs), newDispatchCnt)
}

// IsRWEnabled 返回当前 RW 模式是否启用
func (p *WorkerPool) IsRWEnabled() bool {
	return p.rw.IsEnabled()
}

// SetRWEnabled 运行时动态开关 RW 模式（§10.14 安全协议）。
//
// 关闭时通过 rwMu.Lock() + RLock-after-check 协议保证切换窗口无数据竞争。
//
// Disable 返回后再显式 inflightReads.Wait()（per-Worker，带 stopTimeout 兜底），
// 保证「SetRWEnabled(false) 成功返回 ⇒ 所有读 goroutine 已彻底退出（含 defer 链最后的 inflightReads.Done）」
// 的强契约：
//   - Disable 内部 TryLock+WLock 翻转 enabled，返回时 RLock 已全部释放（RWMutex 语义保证）；
//   - 但 readFunc 的 defer 链顺序是 RUnlock → inflightReadCnt.Add(-1) → inflightReads.Done()，
//     RUnlock 之后 Done 之前仍有数纳秒级窗口，外部观察者（如希望立刻转移 invoker 状态）会看到
//     inflightReads >0 的瞬态；
//   - 此处的 inflightReads.Wait 把这个窗口收口，配合 main run serial path 的 inflightReads.Wait
//     形成「接口侧 + 主循环侧」双重保护。
//
// 失败回滚：若任一 Worker 等待超时，重新 Enable() 恢复原状态，避免半切换。
func (p *WorkerPool) SetRWEnabled(enabled bool) error {
	if !enabled && p.rw.IsEnabled() {
		if err := p.rw.Disable(); err != nil {
			return err
		}
		// 显式等待 per-Worker inflightReads 完整归零（含 defer 链尾）。
		// Disable 已确保 mu.WLock 拿到（无 RLock 持有），剩余 defer 仅几条 atomic
		// 指令，正常情况下 Wait 立即返回；带 stopTimeout 兜底防止 readFunc 长尾。
		if err := p.waitInflightReadsDone(p.rw.stopTimeout); err != nil {
			// 回滚：恢复 RW 模式，避免外部观察到"已 Disable 但 inflightReads 仍 >0"的半切换状态。
			p.rw.Enable()
			p.logger.Errorf("SetRWEnabled(false): waitInflightReadsDone failed: %v, rolled back to enabled", err)
			return err
		}
		p.logger.Warnf("RW mode disabled at runtime")
	} else if enabled && !p.rw.IsEnabled() {
		return ErrRWDynamicEnableUnsupported
	}
	return nil
}

// waitInflightReadsDone 等待所有 Worker 的 inflightReads 计数归零，带超时兜底。
//
// 调用方应已保证此后不会有新的读 goroutine spawn（即 enabled 已翻为 false 且 mu.WLock
// 已经被 Disable 持有过——RLock-after-check 协议保证新的 launchRead 走降级路径）。
//
// 实现：轮询每个 Worker 的 inflightReadCnt，全部归零后再同步 WaitGroup.Wait 收口。
// 不创建不可取消的等待 goroutine，避免超时返回后仍持有 snapshot / worker 引用。
func (p *WorkerPool) waitInflightReadsDone(timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	snap := p.snap.Load()
	if snap == nil || snap.count == 0 {
		return nil
	}
	deadline := time.Now().Add(timeout)
	for {
		allDone := true
		for _, w := range snap.workers {
			if mw, ok := w.(*Worker); ok && mw.inflightReadCnt.Load() != 0 {
				allDone = false
				break
			}
		}
		if allDone {
			for _, w := range snap.workers {
				if mw, ok := w.(*Worker); ok {
					mw.inflightReads.Wait()
				}
			}
			return nil
		}
		if time.Now().After(deadline) {
			return ErrRWDisableTimeout
		}
		// 先 Gosched 让其他可运行 goroutine 推进；再 1ms 退避，避免长时间
		// 持续占用 P 与业务读 goroutine 抢调度（stopTimeout 默认 10s 时尤其重要）。
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
}

func (p *WorkerPool) logDispatchStatsLoop() {
	defer p.wg.Done()
	interval := p.statsInterval
	if interval <= 0 {
		interval = 10 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			p.logDispatchStatsOnce()
			return
		case <-ticker.C:
			p.logDispatchStatsOnce()
		}
	}
}

// logDispatchStatsOnce 打印一次 dispatch 统计
func (p *WorkerPool) logDispatchStatsOnce() {
	if p == nil || p.logger == nil || !p.statsEnabled {
		return
	}

	// 走 snapshot 读端，无锁
	snap := p.snap.Load()
	if snap == nil || len(snap.dispatchCnt) == 0 {
		return
	}

	type wc struct {
		id    int32
		count uint64
	}
	items := make([]wc, 0, len(snap.dispatchCnt))
	var total uint64
	var max uint64
	var min uint64
	var idle int
	first := true
	for id, c := range snap.dispatchCnt {
		v := uint64(0)
		if c != nil {
			// per-interval stats: take-and-reset
			v = c.Swap(0)
		}
		items = append(items, wc{id: id, count: v})
		total += v
		if v == 0 {
			idle++
		}
		if first {
			min = v
			max = v
			first = false
		} else {
			if v < min {
				min = v
			}
			if v > max {
				max = v
			}
		}
	}
	workerN := len(items)

	if workerN == 0 {
		return
	}

	// Sort descending to show hot workers.
	sort.Slice(items, func(i, j int) bool { return items[i].count > items[j].count })

	avg := float64(total) / float64(workerN)
	msg := "dispatch worker dist: workers=" + itoa(workerN) + " active=" + itoa(workerN-idle) + " idle=" + itoa(idle) + " total=" + itoaU64(total) + " max=" + itoaU64(max) + " min=" + itoaU64(min) + " avg=" + formatFloat1(avg)

	// Append top 5.
	limit := 5
	if workerN < limit {
		limit = workerN
	}
	msg += " top="
	for i := 0; i < limit; i++ {
		if i > 0 {
			msg += ", "
		}
		msg += "w" + itoa(int(items[i].id)) + "=" + itoaU64(items[i].count)
	}

	//p.logger.Infof(msg)
	p.logger.Debugf(msg)
}

// 自动调整 worker 数量
func (p *WorkerPool) autoScaleWorkers() {
	defer p.wg.Done()
	if p.autoScaler == nil {
		strategy, err := BuildStrategy(p.conf.SchedulePolicy.ScalingStrategy)
		if err != nil {
			p.logger.Errorf("build scaling strategy failed: %v", err)
			return
		}
		p.autoScaler = &AutoScaler{
			conf:     p.conf.SchedulePolicy.ScalingStrategy,
			Strategy: strategy,
		}
	}

	// 扩缩容由统一调度循环触发：定时检查提供兜底，scaleTrigger 提供事件驱动唤醒。
	// 两类触发都会进入同一个 AutoScaler.ShouldResize 决策入口。
	ticker := time.NewTicker(p.conf.SchedulePolicy.ScalingStrategy.ResizeCoolDown) // 调整间隔
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			// 定时兜底
		case <-p.scaleTrigger:
			// 事件驱动，仍受 CoolDown 限制（ticker 不重置）
		}

		// 走 snapshot 读端，无锁
		snap := p.snap.Load()
		if snap == nil || snap.count == 0 {
			continue
		}
		workers := make([]inf.IMailboxWorker, 0, snap.count)
		for _, w := range snap.workers {
			workers = append(workers, w)
		}
		current := snap.count

		if newSize, reason, ok := p.autoScaler.ShouldResize(current, workers); ok {
			p.logger.Debugf("resizing from %d -> %d: %s", current, newSize, reason)
			p.resizeWorkers(newSize)
		}
	}
}

func fixConf(conf *config.MailboxConf) *config.MailboxConf {
	// fixConf 会改写 conf.EnableRWMode / MaxConcurrentReads / MaxJobExecutionTime 等
	// 字段；如果上层把同一份 *MailboxConf 模板共享给多个 Service，第一个 Service 启动时
	// 的副作用会污染其他 Service 的初始化（典型场景：单 worker Service 把模板的
	// EnableRWMode 改成 false，多 worker Service 再用就丢了 RW 配置）。
	// 这里做一次按需 deep-copy，保证 fixConf 完全无副作用。
	conf = cloneMailboxConfForFix(conf)

	// 设置默认队列模式
	if conf.QueueMode == "" {
		conf.QueueMode = "dual" // 默认双队列模式
	}

	if conf.SchedulePolicy == nil {
		conf.SchedulePolicy = &config.WorkerSchedulePolicy{}
	}

	if conf.SchedulePolicy.InitialWorkerNum <= 0 {
		conf.SchedulePolicy.InitialWorkerNum = 1
	}
	// 调度环使用 jump consistent hash，不需要虚拟节点；VirtualWorkerRate 不参与调度计算。
	if conf.SchedulePolicy.IdlerConf == nil {
		conf.SchedulePolicy.IdlerConf = &config.WorkerIdlerConf{
			EnableCond: true,
		}
	}
	if conf.SchedulePolicy.IdlerConf.BackoffBaseDelay <= 0 {
		conf.SchedulePolicy.IdlerConf.BackoffBaseDelay = time.Microsecond
	}
	if conf.SchedulePolicy.IdlerConf.BackoffMaxDelay <= 0 {
		conf.SchedulePolicy.IdlerConf.BackoffMaxDelay = 16 * time.Microsecond
	}
	if conf.SchedulePolicy.IdlerConf.BackoffMaxRetries <= 0 {
		conf.SchedulePolicy.IdlerConf.BackoffMaxRetries = 3
	}
	if conf.SchedulePolicy.IdlerConf.MaxIdleBeforeBackoff <= 0 {
		conf.SchedulePolicy.IdlerConf.MaxIdleBeforeBackoff = 1000
	}

	// ---- 扩缩容因子校验 ----
	if conf.SchedulePolicy.ScalingStrategy != nil {
		sc := conf.SchedulePolicy.ScalingStrategy
		if sc.GrowthFactor <= 0 {
			sc.GrowthFactor = 0.5
		}
		if sc.ShrinkFactor <= 0 || sc.ShrinkFactor > 0.5 {
			sc.ShrinkFactor = 0.25
		}
		// MinWorkerNum 必须 >= 1，否则缩容可能导致 Worker 数降为 0 而无法处理消息
		if sc.MinWorkerNum < 1 {
			sc.MinWorkerNum = 1
		}
		// MaxWorkerNum 必须 >= MinWorkerNum
		if sc.MaxWorkerNum < sc.MinWorkerNum {
			sc.MaxWorkerNum = sc.MinWorkerNum
		}
	}

	// ---- RW 模式配置校验（单 worker 场景不允许开启 RW 模式） ----
	// 单 Worker 下读写分离无意义：主循环串行处理 Job，spawn 的读 goroutine
	// 反而引入额外并发开销和锁竞争，且无法通过多 Worker 主循环消费写 Job
	// 来掩盖读 goroutine 的排空等待。强制关闭并忽略相关配置。
	if conf.EnableRWMode && conf.SchedulePolicy.InitialWorkerNum <= 1 {
		conf.EnableRWMode = false
		conf.MaxConcurrentReads = 0
		conf.StopTimeout = 0
	}
	if conf.EnableRWMode {
		// 默认值: min(runtime.NumCPU() * 4, 64)，避免大核机器上默认值过高
		if conf.MaxConcurrentReads <= 0 {
			conf.MaxConcurrentReads = runtime.NumCPU() * 4
			if conf.MaxConcurrentReads > 64 {
				conf.MaxConcurrentReads = 64
			}
		}
		// StopTimeout 默认 10s
		if conf.StopTimeout <= 0 {
			conf.StopTimeout = 10 * time.Second
		}
		// MaxJobExecutionTime 默认 0（关闭）：watchdog 在每条 Job 上 Schedule + Cancel
		// 时间轮 Timer，对短任务高 QPS 场景是纯成本（每条多 2 次原子 + 时间轮链表操作）。
		// 仅在用户显式配置 >0 时启用，按需开启长任务监控。
		// ReadPoolSize 默认等于 MaxConcurrentReads
		if conf.ReadPoolSize <= 0 {
			conf.ReadPoolSize = conf.MaxConcurrentReads
		}
	} else {
		// 未启用 RW 时忽略相关配置
		conf.MaxConcurrentReads = 0
	}

	return conf
}

// cloneMailboxConfForFix 为 fixConf 准备一份"按需 deep-copy"的 MailboxConf。
//
// 仅深拷贝 fixConf 真正会写入字段的子结构，其余共享指针保持共享，控制开销：
//   - SchedulePolicy（fixConf 直接改 InitialWorkerNum / VirtualWorkerRate 等）；
//   - SchedulePolicy.IdlerConf（可能新建并改字段）；
//   - SchedulePolicy.ScalingStrategy（可能改 GrowthFactor / MinWorkerNum 等）。
//
// 顶层结构本身做一次浅拷贝，使 fixConf 修改 EnableRWMode / MaxConcurrentReads /
// StopTimeout / MaxJobExecutionTime 等标量字段不会泄漏回模板。
func cloneMailboxConfForFix(src *config.MailboxConf) *config.MailboxConf {
	if src == nil {
		return &config.MailboxConf{}
	}
	dst := *src // 浅拷贝顶层标量字段
	if src.SchedulePolicy != nil {
		sp := *src.SchedulePolicy
		if src.SchedulePolicy.IdlerConf != nil {
			ic := *src.SchedulePolicy.IdlerConf
			sp.IdlerConf = &ic
		}
		if src.SchedulePolicy.ScalingStrategy != nil {
			ss := *src.SchedulePolicy.ScalingStrategy
			sp.ScalingStrategy = &ss
		}
		dst.SchedulePolicy = &sp
	}
	return &dst
}

// ---- RW 可观测性指标查询 ----

// RWMetrics RW 模式可观测性快照
type RWMetrics struct {
	ReadTotal         int64         // 累计读操作数
	WriteTotal        int64         // 累计写操作数
	DrainDiscardTotal int64         // Drain 阶段丢弃 Job 数（per-job 累计）
	UnsafeDrainEvents int64         // unsafe drain 事件次数（StopTimeout 触发，事件级）
	LongJobTotal      int64         // watchdog 触发次数（Job 执行超过 maxJobExecTime）
	InflightReads     int64         // 当前 in-flight 读 goroutine 数
	AvgReadDuration   time.Duration // 平均读执行耗时
	AvgWriteWait      time.Duration // 平均写锁等待耗时
}

// GetRWMetrics 返回当前 RW 可观测性指标快照（无锁聚合，允许微小误差）
func (p *WorkerPool) GetRWMetrics() RWMetrics {
	m := RWMetrics{
		ReadTotal:         p.rw.readTotal.Load(),
		WriteTotal:        p.rw.writeTotal.Load(),
		DrainDiscardTotal: p.rw.drainDiscardTotal.Load(),
		UnsafeDrainEvents: p.rw.unsafeDrainEvents.Load(),
		LongJobTotal:      p.rw.longJobTotal.Load(),
	}

	// 聚合各 Worker 的 per-Worker 指标走 snapshot 读端，无锁
	var totalReadDur, totalReadCnt int64
	var totalWriteWait, totalWriteCnt int64
	if snap := p.snap.Load(); snap != nil {
		for _, w := range snap.workers {
			if mw, ok := w.(*Worker); ok {
				m.InflightReads += mw.inflightReadCnt.Load()
				totalReadDur += mw.rwReadDurationSum.Load()
				totalReadCnt += mw.rwReadCount.Load()
				totalWriteWait += mw.rwWriteWaitSum.Load()
				totalWriteCnt += mw.rwWriteWaitCount.Load()
			}
		}
	}

	if totalReadCnt > 0 {
		m.AvgReadDuration = time.Duration(totalReadDur / totalReadCnt)
	}
	if totalWriteCnt > 0 {
		m.AvgWriteWait = time.Duration(totalWriteWait / totalWriteCnt)
	}
	return m
}
