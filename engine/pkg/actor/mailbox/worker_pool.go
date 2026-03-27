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
	"github.com/njtc406/emberengine/engine/pkg/profiler"
	"github.com/njtc406/emberengine/engine/pkg/utils/asynclib"
	"github.com/njtc406/emberengine/engine/pkg/utils/hashring"
)

// ErrRWDisableTimeout SetRWEnabled(false) 超时，有泄漏的读 goroutine
var ErrRWDisableTimeout = errors.New("RW disable timeout: leaked read goroutines prevent safe switch")

func formatFloat1(v float64) string {
	// keep it short: 1 decimal, no fmt
	neg := false
	if v < 0 {
		neg = true
		v = -v
	}
	whole := int(v)
	frac := int((v - float64(whole)) * 10)
	s := itoa(whole) + "." + itoa(frac)
	if neg {
		return "-" + s
	}
	return s
}

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
	profiler        *profiler.Profiler           // 性能分析
	autoScaler      IScaler                      // 自动扩容器
	logger          log.ILoggerX
	workerCount     atomic.Int32 // 当前 worker 数量（用于扩缩容），atomic 以支持 RW 模式下无锁读取

	// 停机时队列处理策略（由 Mailbox 下发）
	drainPolicy DrainPolicy

	// ---- RW 增强字段（Mailbox 级共享） ----
	enableRW       atomic.Bool   // 是否启用 RW 模式（atomic：支持运行时动态开关）
	rwMu           sync.RWMutex  // 全 Service 共享读写锁，所有 Worker 引用
	writeRequested atomic.Int32  // 正在等待写锁的 Writer 计数，读路径检查 >0 时让步避免写饥饿
	readSem        chan struct{} // 全 Service 读并发信号量（nil = 不限制）
	stopTimeout    time.Duration // Stop 时等待 in-flight 读 goroutine 的最大时间

	// ---- RW 可观测性指标（§10.7） ----
	rwReadTotal         atomic.Int64  // 累计读操作数
	rwWriteTotal        atomic.Int64  // 累计写操作数
	rwDrainDiscardTotal atomic.Int64  // StopTimeout 导致的 Job 丢弃数
	maxJobExecTime      time.Duration // Job 执行硬超时看门狗阈值（0=禁用）

	// ---- 读 goroutine 池（per-WorkerPool 独立池，资源隔离） ----
	readPool *asynclib.Pool // 仅在 EnableRWMode 时初始化，可为 nil

	// Debug-only dispatch distribution stats.
	statsEnabled  bool // 是否开启统计（仅在 Debug 模式下）
	statsInterval time.Duration
	dispatchCnt   map[int32]*atomic.Uint64 // 每个 worker 的事件计数
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
		//statsEnabled:    config.IsDebug(), // TODO 改成配置吧
		statsInterval: 10 * time.Second,
		dispatchCnt:   make(map[int32]*atomic.Uint64, conf.SchedulePolicy.InitialWorkerNum),
		stopTimeout:   conf.StopTimeout,
	}

	// ---- RW 共享状态初始化 ----
	pool.enableRW.Store(conf.EnableRWMode)
	if conf.EnableRWMode && conf.MaxConcurrentReads > 0 {
		pool.readSem = make(chan struct{}, conf.MaxConcurrentReads)
	}
	if pool.enableRW.Load() && pool.stopTimeout <= 0 {
		pool.stopTimeout = 10 * time.Second
	}
	// watchdog 执行时长阈值
	if conf.MaxJobExecutionTime > 0 {
		pool.maxJobExecTime = conf.MaxJobExecutionTime
	}

	// ---- 读 goroutine 池初始化（per-WorkerPool 独立池） ----
	if conf.EnableRWMode && conf.ReadPoolSize > 0 {
		rp, err := asynclib.NewPool(conf.ReadPoolSize)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("create read pool: %w", err)
		}
		pool.readPool = rp
	}

	return pool, nil
}

func (p *WorkerPool) Start() error {
	p.mu.Lock()
	for i := int32(0); i < p.conf.SchedulePolicy.InitialWorkerNum; i++ {
		worker := newWorker(i, p.conf, p) // 使用配置的workerConfig
		if worker == nil {
			p.mu.Unlock()
			return fmt.Errorf("service[%s] failed to create worker, conf:%v", p.invoker.GetServiceName(), p.conf)
		}
		p.workers[i] = worker
		worker.Start()
		// 将 worker 加入到哈希环中（这里每个都加进入,但是单线程时可能不会使用）
		p.ring.Add(i)
		if p.statsEnabled {
			p.dispatchCnt[i] = &atomic.Uint64{}
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

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.workers == nil {
		return
	}
	p.ring.Clear()
	p.workers = nil

	// 释放读 goroutine 池
	if p.readPool != nil {
		p.readPool.Release()
		p.readPool = nil
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
		// 单线程时直接使用 workerID=0
		worker, exists = p.workers[workerID]
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

		for i := currentCount; i < newSize; i++ {
			worker := newWorker(i, p.conf, p) // 使用配置的workerConfig
			p.workers[i] = worker
			worker.Start()
			p.ring.Add(i)
			if p.statsEnabled {
				p.dispatchCnt[i] = &atomic.Uint64{}
			}
		}
		p.workerCount.Store(newSize)
		p.mu.Unlock()
	} else {
		// 缩容：【P0 修复】先 Unlock 后 Stop + 原地排空 Drain，消除死锁风险。
		// 流程：Lock → 从 hash ring 移除 → 取出 Worker 引用 → Unlock → Stop → Lock → 清理 map
		p.mu.Lock()
		removeMap := make(map[int32]struct{}, currentCount-newSize)
		removedWorkers := make([]inf.IMailboxWorker, 0, currentCount-newSize)
		removedIds := make([]int32, 0, currentCount-newSize)
		for i := newSize; i < currentCount; i++ {
			if worker, exists := p.workers[i]; exists {
				removedWorkers = append(removedWorkers, worker)
				removedIds = append(removedIds, i)
				removeMap[i] = struct{}{}
			}
		}
		// 从哈希环移除（新 Job 不再路由到这些 Worker）
		p.ring.RemoveMany(removeMap)
		p.workerCount.Store(int32(newSize))
		p.mu.Unlock() // ← 先释放 pool 锁，避免 Drain handler 自投递死锁

		// 在 pool 锁外停止 Worker（Worker 会原地排空 Drain 队列残留 Job）
		for _, w := range removedWorkers {
			w.BeginStop()
		}
		for _, w := range removedWorkers {
			w.Wait()
		}

		// 重新获取锁清理 map 数据结构
		p.mu.Lock()
		for _, id := range removedIds {
			delete(p.workers, id)
			if p.statsEnabled {
				delete(p.dispatchCnt, id)
			}
		}
		p.mu.Unlock()
	}
}

// IsRWEnabled 返回当前 RW 模式是否启用
func (p *WorkerPool) IsRWEnabled() bool {
	return p.enableRW.Load()
}

// GetEnableRWPtr 返回 enableRW 的指针，供 MethodMgr 等外部组件引用。
// 仅在服务初始化阶段调用一次，用于建立跨组件引用关系。
func (p *WorkerPool) GetEnableRWPtr() *atomic.Bool {
	return &p.enableRW
}

// SetRWEnabled 运行时动态开关 RW 模式（§10.14 安全协议）
// 关闭时通过 rwMu.Lock() + RLock-after-check 协议保证切换窗口无数据竞争
func (p *WorkerPool) SetRWEnabled(enabled bool) error {
	if !enabled && p.enableRW.Load() {
		// 关闭 RW 模式：获取 WLock，等待所有 RLock 释放 + 阻止新的 RLock 进入
		deadline := time.Now().Add(p.stopTimeout)
		for !p.rwMu.TryLock() {
			if time.Now().After(deadline) {
				return ErrRWDisableTimeout
			}
			runtime.Gosched()
		}
		// 持有 WLock 期间翻转标志——此刻无任何 goroutine 访问共享状态
		p.enableRW.Store(false)
		p.rwMu.Unlock()
		p.logger.Warnf("RW mode disabled at runtime")
	} else if enabled && !p.enableRW.Load() {
		// 开启 RW 模式：翻转标志前确保 readSem 已初始化
		p.mu.Lock()
		if p.readSem == nil {
			maxReads := p.conf.MaxConcurrentReads
			// 初始 EnableRWMode=false 时 fixConf 会将 MaxConcurrentReads 设为 0，
			// 运行时启用需要计算默认值
			if maxReads <= 0 {
				maxReads = runtime.NumCPU() * 4
				if maxReads > 64 {
					maxReads = 64
				}
			}
			p.readSem = make(chan struct{}, maxReads)
			p.conf.MaxConcurrentReads = maxReads // 回写，供后续 SetRWEnabled 使用
		}
		p.mu.Unlock()
		p.enableRW.Store(true)
		p.logger.Warnf("RW mode enabled at runtime, readSem initialized with cap=%d",
			cap(p.readSem))
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
}

func fixConf(conf *config.MailboxConf) *config.MailboxConf {
	if conf == nil {
		conf = &config.MailboxConf{}
	}

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

// ---- RW 可观测性指标查询 ----

// RWMetrics RW 模式可观测性快照
type RWMetrics struct {
	ReadTotal         int64         // 累计读操作数
	WriteTotal        int64         // 累计写操作数
	DrainDiscardTotal int64         // Drain 阶段丢弃 Job 数
	InflightReads     int64         // 当前 in-flight 读 goroutine 数
	AvgReadDuration   time.Duration // 平均读执行耗时
	AvgWriteWait      time.Duration // 平均写锁等待耗时
}

// GetRWMetrics 返回当前 RW 可观测性指标快照（无锁聚合，允许微小误差）
func (p *WorkerPool) GetRWMetrics() RWMetrics {
	m := RWMetrics{
		ReadTotal:         p.rwReadTotal.Load(),
		WriteTotal:        p.rwWriteTotal.Load(),
		DrainDiscardTotal: p.rwDrainDiscardTotal.Load(),
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
