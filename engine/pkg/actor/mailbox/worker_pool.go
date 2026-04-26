// Package mailbox
// 模块名: 工作线程池
// 功能描述: 描述
// 作者:  yr  2025/7/19 0019 21:37
// 最后更新:  yr  2025/7/19 0019 21:37
package mailbox

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/hashring"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
)

// ErrRWDisableTimeout SetRWEnabled(false) 超时，有泄漏的读 goroutine
var ErrRWDisableTimeout = errors.New("RW disable timeout: leaked read goroutines prevent safe switch")

type IScaler interface {
	ShouldResize(current int, workers []inf.IMailboxWorker) (newSize int32, reason string, ok bool)
}

type WorkerPool struct {
	conf            *config.MailboxConf
	mu              sync.RWMutex
	wg              sync.WaitGroup
	ctx             context.Context
	cancel          context.CancelFunc
	workers         map[int32]inf.IMailboxWorker // 工作线程
	ring            *hashring.HashRing[int32]    // 一致性哈希环，用于分派事件
	invoker         inf.IMessageInvoker          // 消息处理器
	middlewareChain *MiddlewareChain             // 中间件链
	autoScaler      IScaler                      // 自动扩容器
	logger          log.ILoggerX
	workerCount     atomic.Int32 // 当前 worker 数量（用于扩缩容），atomic 以支持 RW 模式下无锁读取

	// 停机时队列处理策略（由 Mailbox 下发）
	drainPolicy DrainPolicy

	// ---- RW 读写分离控制器 ----
	rw *RWController

	// Debug-only dispatch distribution stats.
	statsEnabled  bool // 是否开启统计（仅在 Debug 模式下）
	statsInterval time.Duration
	dispatchCnt   map[int32]*atomic.Uint64 // 每个 worker 的事件计数

	// ---- AutoScaler 事件驱动触发 ----
	scaleTrigger chan struct{} // 容量 1，非阻塞通知 autoScaleWorkers

	// ---- Worker ID 分配器（单调递增，避免缩-扩后 ID 复用导致监控差分异常）----
	nextWorkerID atomic.Int32

	// ---- watchdog 时间轮（per-WorkerPool，替代 per-Job time.AfterFunc）----
	// 仅在 MaxJobExecutionTime > 0 时初始化。
	// TODO: 后续可改为复用 Node 级共享 TimingWheel，减少 goroutine 开销。
	watchdogTW        *timingwheel.TimingWheel
	watchdogScheduler timingwheel.ITimerScheduler
	watchdogAdapter   watchdogCanceler // 供 WorkerEnv 传递给 Worker
	watchdogWg        sync.WaitGroup   // 独立跟踪 watchdog consumer goroutine。
	// 不走 p.wg：其 channel 要到 Wait() 阶段 scheduler.Stop() 才会关闭，
	// 如果走 p.wg 会导致 BeginStop 的 p.wg.Wait() 与 watchdog 形成死锁。
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
		workers:         make(map[int32]inf.IMailboxWorker, conf.SchedulePolicy.InitialWorkerNum),
		invoker:         invoker,
		ring:            hashring.NewHashRing[int32](conf.SchedulePolicy.VirtualWorkerRate),
		middlewareChain: NewMiddlewareChain(middlewares...),
		ctx:             ctx,
		cancel:          cancel,
		logger:          logger,
		statsInterval:   10 * time.Second,
		dispatchCnt:     make(map[int32]*atomic.Uint64, conf.SchedulePolicy.InitialWorkerNum),
		scaleTrigger:    make(chan struct{}, 1),
	}

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
		// 【P1-3】预构 RWContextInfo，SourceService 在 Pool 生命周期内不变
		rwReadCtxInfo: def.RWContextInfo{
			Mode:          def.RWModeRead,
			SourceService: p.invoker.GetServiceName(),
		},
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
	for n := int32(0); n < p.conf.SchedulePolicy.InitialWorkerNum; n++ {
		id := p.nextWorkerID.Add(1) - 1
		worker := newWorker(id, p.conf, p.workerEnv(), p.drainPolicy)
		p.workers[id] = worker
		worker.Start()
		// 将 worker 加入到哈希环中（这里每个都加进入,但是单线程时可能不会使用）
		p.ring.Add(id)
		if p.statsEnabled {
			p.dispatchCnt[id] = &atomic.Uint64{}
		}
	}
	p.workerCount.Store(int32(p.conf.SchedulePolicy.InitialWorkerNum))
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

// BeginStop 发起停止（非阻塞）：停止后台协程并通知 workers 退出，但不等待。
func (p *WorkerPool) BeginStop() {
	// 先关闭自动扩容/统计等后台协程
	p.cancel()
	// 等待后台协程退出，避免与 shrink/dispatch 等竞争（不涉及 worker 自等问题）
	p.wg.Wait()

	p.mu.RLock()
	if p.workers == nil {
		p.mu.RUnlock()
		return
	}
	workers := make([]inf.IMailboxWorker, 0, len(p.workers))
	for _, w := range p.workers {
		workers = append(workers, w)
	}
	p.mu.RUnlock()

	for _, w := range workers {
		w.BeginStop()
	}
}

// Wait 等待 worker 全部退出，并完成中间件链停止与资源清理。
func (p *WorkerPool) Wait() {
	p.mu.Lock()
	if p.workers == nil {
		p.mu.Unlock()
		return
	}
	workers := make([]inf.IMailboxWorker, 0, len(p.workers))
	for _, w := range p.workers {
		workers = append(workers, w)
	}
	p.mu.Unlock()

	for _, w := range workers {
		w.Wait()
	}

	// 停止中间件链（需要在 workers 完全退出后，避免 DrainDiscard 时仍调用 OnComplete）
	p.middlewareChain.Stop()

	// 先在锁内把 workers / ring 清空并取出 readPool 引用，锁外再调用 Release。
	// 这样避免在持锁状态下执行 ants Release 的内部逻辑，也让 readPool 生命周期
	// 语义更清晰：仅当 WorkerPool 结束运行后才释放。
	p.mu.Lock()
	if p.workers == nil {
		p.mu.Unlock()
		return
	}
	p.ring.Clear()
	p.workers = nil
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

// Stop 兼容接口：BeginStop + Wait。
func (p *WorkerPool) Stop() {
	p.BeginStop()
	p.Wait()
}

// DispatchJob 将任务分派给具体 worker。
//
//   - 多 worker 模式：通过 ring.Get(evt.GetDispatcherKey()) 选择 worker，保证相同 dispatcherKey 的事件落到同一 worker；
//   - 单 worker 模式：固定使用 workerID=0，行为接近 Actor 模型的串行执行。
//   - mctx: 中间件上下文，用于在消息处理完成后调用 OnComplete 回调。
func (p *WorkerPool) DispatchJob(job inf.IMailboxJob) error {
	// 通过一致性哈希+虚拟节点解决 将事件分派给worker执行
	var worker inf.IMailboxWorker
	var exists bool
	var workerID int32
	ctx := job.GetContext()
	p.mu.RLock() // 加个锁,防止在调整worker数量时,hash环还没有更新
	if len(p.workers) > 1 {
		var ok bool
		workerID, ok = p.ring.Get(job.GetDispatcherKey())
		if !ok {
			p.logger.WithContext(ctx).Errorf("No worker available in hash ring")
			p.mu.RUnlock()
			return def.ErrMailboxWorkerIsFull
		}
		worker, exists = p.workers[workerID]
	} else {
		// 单 worker 时直接取 map 中唯一的 entry（ID 不再固定为 0）
		for id, w := range p.workers {
			workerID = id
			worker = w
			exists = true
			break
		}
	}

	if !exists {
		p.logger.WithContext(ctx).Errorf("Worker %d not found", workerID)
		p.mu.RUnlock()
		return def.ErrMailboxWorkerNotFound
	}

	if p.statsEnabled {
		cnt := p.dispatchCnt[workerID]
		if cnt != nil {
			cnt.Add(1)
		}
	}
	p.mu.RUnlock()

	// AutoScaler 事件驱动：当 worker 队列有积压时，非阻塞通知扩缩容协程
	if p.conf.SchedulePolicy.EnableAutoScaling && worker.GetJobLen() > 0 {
		select {
		case p.scaleTrigger <- struct{}{}:
		default: // 已有信号待处理，跳过
		}
	}

	return worker.SubmitJob(job)
}

func (p *WorkerPool) resizeWorkers(newSize int32) {
	currentCount := p.workerCount.Load()
	if newSize == currentCount {
		return
	}

	if newSize > currentCount {
		// 扩容：新增 worker，并受 MaxWorkerNum 约束
		p.mu.Lock()
		maxWorkers := int32(0)
		if p.conf != nil && p.conf.SchedulePolicy != nil && p.conf.SchedulePolicy.ScalingStrategy != nil {
			maxWorkers = p.conf.SchedulePolicy.ScalingStrategy.MaxWorkerNum
		}
		// 如果未配置 MaxWorkerNum，则认为不限制上限
		if maxWorkers > 0 && newSize > maxWorkers {
			newSize = maxWorkers
		}

		for n := newSize - currentCount; n > 0; n-- {
			id := p.nextWorkerID.Add(1) - 1
			worker := newWorker(id, p.conf, p.workerEnv(), p.drainPolicy)
			p.workers[id] = worker
			worker.Start()
			p.ring.Add(id)
			if p.statsEnabled {
				p.dispatchCnt[id] = &atomic.Uint64{}
			}
		}
		p.workerCount.Store(newSize)
		p.mu.Unlock()
	} else {
		// 缩容【P0-2 修复】：保证 dispatcherKey 顺序契约。
		//
		// 旧实现："先从 ring 删除 → 再 Stop+Wait" 会让同 dispatcherKey 的新 Job 立刻 rehash
		// 到另一个 worker 与老 worker 的残留 Job 并发执行，等价于"同一 actor 实体被两个 actor 处理"，
		// 直接破坏 mailbox 对外承诺的"按 dispatcherKey 顺序执行"契约。
		//
		// 新流程："先 BeginStop+Wait → 再从 ring/workers 删除"：
		//   1. 选定淘汰 ids；
		//   2. BeginStop：老 worker 拒绝新 SubmitJob（新到达的同 key Job 会拿到
		//      ErrMailboxWorkerClosed，由 ADR-4 / Mailbox.PostJob 统一走 OnJobDiscarded，
		//      业务可感知，而不会被并发执行）；
		//   3. Wait：老 worker 串行 drain 完队列残留（DrainPolicy 决定执行 / 丢弃），
		//      保证残留 Job 之间的 per-key 顺序；
		//   4. 拿锁从 ring/workers map 移除并更新 workerCount —— 此后新 Job 才被 rehash 到
		//      新 owner，且老 worker 上对该 key 的执行已彻底结束。
		//
		// 取舍：缩容窗口（约等于老 worker drain 时长）内同 key 新 Job 可能被拒（伴随
		// OnJobDiscarded 通知），但永远不会"老队列还在跑、新消息又被并发处理"——
		// 顺序契约优先，丢失 / 拒收对调用方明确可感知。
		//
		// 按 ID 升序保留前 newSize 个，淘汰末尾 N 个（单调递增 ID 后活跃 ID 不再连续）。
		p.mu.Lock()
		toRemove := int(currentCount - newSize)
		ids := make([]int32, 0, len(p.workers))
		for id := range p.workers {
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
		removedIDs := make([]int32, 0, len(ids))
		for _, id := range ids {
			if worker, exists := p.workers[id]; exists {
				removedWorkers = append(removedWorkers, worker)
				removedIDs = append(removedIDs, id)
			}
		}
		p.mu.Unlock() // 释放 pool 锁，避免 BeginStop/Wait 期间长时间阻塞 dispatch

		if len(removedWorkers) == 0 {
			return
		}

		// ① 老 worker 拒绝新 SubmitJob（CAS 到 closing 状态）。
		//    此时 ring 仍指向它们，DispatchJob 选中后 SubmitJob 立即返回 ErrMailboxWorkerClosed，
		//    经 ADR-4 在 Mailbox.PostJob 路径统一回调 OnJobDiscarded。
		for _, w := range removedWorkers {
			w.BeginStop()
		}
		// ② 等待 drain 完成（按 DrainPolicy：DrainExecute 串行执行残留 / DrainDiscard 仅回收）。
		//    drain 是老 worker 自己的 run goroutine 串行进行，per-key 顺序天然保留。
		for _, w := range removedWorkers {
			w.Wait()
		}

		// ③ 老 worker 已彻底退出后才从 ring + workers map 移除。
		//    在此之前，DispatchJob 命中老 worker 都返回 closed 错误（业务可感知），
		//    在此之后，新 Job 才被 rehash 到新 owner —— 对老 key 的"先后执行"不会跨 worker 并发。
		p.mu.Lock()
		removeMap := make(map[int32]struct{}, len(removedIDs))
		for _, id := range removedIDs {
			if _, exists := p.workers[id]; exists {
				removeMap[id] = struct{}{}
				delete(p.workers, id)
				if p.statsEnabled {
					delete(p.dispatchCnt, id)
				}
			}
		}
		p.ring.RemoveMany(removeMap)
		p.workerCount.Store(int32(newSize))
		p.mu.Unlock()
	}
}

// IsRWEnabled 返回当前 RW 模式是否启用
func (p *WorkerPool) IsRWEnabled() bool {
	return p.rw.IsEnabled()
}

// GetEnableRWPtr 返回 enableRW 的指针，供 MethodMgr 等外部组件引用。
// 仅在服务初始化阶段调用一次，用于建立跨组件引用关系。
func (p *WorkerPool) GetEnableRWPtr() *atomic.Bool {
	return p.rw.EnabledPtr()
}

// SetRWEnabled 运行时动态开关 RW 模式（§10.14 安全协议）
// 关闭时通过 rwMu.Lock() + RLock-after-check 协议保证切换窗口无数据竞争
func (p *WorkerPool) SetRWEnabled(enabled bool) error {
	if !enabled && p.rw.IsEnabled() {
		if err := p.rw.Disable(); err != nil {
			return err
		}
		p.logger.Warnf("RW mode disabled at runtime")
	} else if enabled && !p.rw.IsEnabled() {
		p.mu.Lock()
		p.rw.EnsureReadResources(p.conf, p.logger)
		p.mu.Unlock()
		p.rw.Enable()
		p.logger.Warnf("RW mode enabled at runtime, readSem initialized with cap=%d",
			cap(p.rw.readSem))
	}
	return nil
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

	p.mu.RLock()
	if len(p.dispatchCnt) == 0 {
		p.mu.RUnlock()
		return
	}

	type wc struct {
		id    int32
		count uint64
	}
	items := make([]wc, 0, len(p.dispatchCnt))
	var total uint64
	var max uint64
	var min uint64
	var idle int
	first := true
	for id, c := range p.dispatchCnt {
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
	p.mu.RUnlock()

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

	// TODO 定时触发检查这部分先这么用吧,主要还没想到什么好的方式来为每种策略定制一个检查机制
	// TODO 主要是嵌套策略里面可能包含了自驱动和外部驱动两种类型的策略,不太好分开
	// TODO 下一步的改动可能是把触发时机抽离出来,这里只是一个触发入口,自动触发的放入独立的
	// 自动调度器中,非自动触发的,由他自己来调用触发接口触发?

	ticker := time.NewTicker(p.conf.SchedulePolicy.ScalingStrategy.ResizeCoolDown) // 调整间隔
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			// 定时兆底
		case <-p.scaleTrigger:
			// 事件驱动，仍受 CoolDown 限制（ticker 不重置）
		}

		p.mu.RLock()
		if len(p.workers) == 0 {
			p.mu.RUnlock()
			continue
		}

		workers := make([]inf.IMailboxWorker, 0, len(p.workers))
		for _, w := range p.workers {
			workers = append(workers, w)
		}
		current := len(workers)
		p.mu.RUnlock()

		if newSize, reason, ok := p.autoScaler.ShouldResize(current, workers); ok {
			p.logger.Debugf("resizing from %d -> %d: %s", current, newSize, reason)
			p.resizeWorkers(newSize)
		}
	}
}

func fixConf(conf *config.MailboxConf) *config.MailboxConf {
	// 【P1-9】fixConf 会改写 conf.EnableRWMode / MaxConcurrentReads / MaxJobExecutionTime 等
	// 字段；如果上层把同一份 *MailboxConf 模板共享给多个 Service，第一个 Service 启动时
	// 的副作用会污染后续 Service 的初始化（典型场景：单 worker Service 把模板的
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
	if conf.SchedulePolicy.VirtualWorkerRate <= 0 {
		conf.SchedulePolicy.VirtualWorkerRate = 24
	}
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
		// MaxJobExecutionTime 默认 30s
		if conf.MaxJobExecutionTime <= 0 {
			conf.MaxJobExecutionTime = 30 * time.Second
		}
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

// cloneMailboxConfForFix 为 fixConf 准备一份"按需 deep-copy"的 MailboxConf（P1-9）。
//
// 仅深拷贝 fixConf 真正会写入字段的子结构，其余共享指针保留，控制开销：
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

	// 聚合各 Worker 的 per-Worker 指标
	p.mu.RLock()
	var totalReadDur, totalReadCnt int64
	var totalWriteWait, totalWriteCnt int64
	for _, w := range p.workers {
		if mw, ok := w.(*Worker); ok {
			m.InflightReads += mw.inflightReadCnt.Load()
			totalReadDur += mw.rwReadDurationSum.Load()
			totalReadCnt += mw.rwReadCount.Load()
			totalWriteWait += mw.rwWriteWaitSum.Load()
			totalWriteCnt += mw.rwWriteWaitCount.Load()
		}
	}
	p.mu.RUnlock()

	if totalReadCnt > 0 {
		m.AvgReadDuration = time.Duration(totalReadDur / totalReadCnt)
	}
	if totalWriteCnt > 0 {
		m.AvgWriteWait = time.Duration(totalWriteWait / totalWriteCnt)
	}
	return m
}
