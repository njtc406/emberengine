// Package mailbox
// 模块名: 工作线程池
// 功能描述: 描述
// 作者:  yr  2025/7/19 0019 21:37
// 最后更新:  yr  2025/7/19 0019 21:37
package mailbox

import (
	"context"
	"sync"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/profiler"
	"github.com/njtc406/emberengine/engine/pkg/utils/hashring"
)

type IScaler interface {
	ShouldResize(current int, workers []inf.IMailboxWorker) (newSize int, reason string, ok bool)
}

type WorkerPool struct {
	conf        *config.MailboxConf
	mu          sync.RWMutex
	wg          sync.WaitGroup
	ctx         context.Context
	cancel      context.CancelFunc
	workers     map[int]inf.IMailboxWorker // 工作线程
	ring        *hashring.HashRing[int]    // 一致性哈希环，用于分派事件
	invoker     inf.IMessageInvoker        // 消息处理器
	middlewares []inf.IMailboxMiddleware   // 中间件
	profiler    *profiler.Profiler         // 性能分析（这个之后修改为性能数据采集器,只采集数据,分析放在采集器中自己去做）
	autoScaler  IScaler                    // 自动扩容器
	logger      log.ILogger
	workerCount int // 当前 worker 数量（用于扩缩容）
}

func NewWorkerPool(conf *config.MailboxConf, logger log.ILogger, invoker inf.IMessageInvoker, middlewares ...inf.IMailboxMiddleware) *WorkerPool {
	if invoker == nil {
		logger.Fatal("invoker is nil")
	}
	conf = fixConf(conf)
	ctx, cancel := context.WithCancel(context.Background())
	return &WorkerPool{
		conf:        conf,
		workers:     make(map[int]inf.IMailboxWorker, conf.SchedulePolicy.InitialWorkerNum),
		invoker:     invoker,
		ring:        hashring.NewHashRing[int](conf.SchedulePolicy.VirtualWorkerRate),
		middlewares: middlewares,
		ctx:         ctx,
		cancel:      cancel,
		logger:      logger,
	}
}

func (p *WorkerPool) Start() {
	p.mu.Lock()
	for i := 0; i < p.conf.SchedulePolicy.InitialWorkerNum; i++ {
		worker := newWorker(i, p.conf, p) // 使用配置的workerConfig
		if worker == nil {
			p.logger.Fatalf("service[%s] Failed to create worker, conf:%v", p.invoker.GetServiceName(), p.conf)
		}
		p.workers[i] = worker
		worker.Start()
		// 将 worker 加入到哈希环中（这里每个都加进入,但是单线程时可能不会使用）
		p.ring.Add(i)
	}
	p.workerCount = p.conf.SchedulePolicy.InitialWorkerNum
	p.mu.Unlock()

	for _, middleware := range p.middlewares {
		middleware.MailboxStarted()
	}

	if p.conf.SchedulePolicy.EnableAutoScaling {
		p.wg.Add(1)
		go p.autoScaleWorkers()
	}

	p.logger.Debugf("Started service[%s] mailbox workers:%d", p.invoker.GetServiceName(), p.conf.SchedulePolicy.InitialWorkerNum)
}

func (p *WorkerPool) Stop() {
	// 先关闭自动扩容
	p.cancel()
	p.wg.Wait()

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.workers == nil {
		return
	}

	for _, worker := range p.workers {
		worker.Stop()
	}
	p.ring.Clear()
	p.workers = nil
}

// DispatchEvent 将事件分派给具体 worker。
//
//   - 多 worker 模式：通过 ring.Get(evt.GetDispatcherKey()) 选择 worker，保证相同 dispatcherKey 的事件落到同一 worker；
//   - 单 worker 模式：固定使用 workerID=0，行为接近 Actor 模型的串行执行。
func (p *WorkerPool) DispatchEvent(evt inf.IEvent) error {
	// 通过一致性哈希+虚拟节点解决 将事件分派给worker执行
	var worker inf.IMailboxWorker
	var exists bool
	var workerID int

	p.mu.RLock() // 加个锁,防止在调整worker数量时,hash环还没有更新
	if len(p.workers) > 1 {
		var ok bool
		workerID, ok = p.ring.Get(evt.GetDispatcherKey())
		if !ok {
			p.logger.WithContext(evt.GetContext()).Errorf("No worker available in hash ring")
			p.mu.RUnlock()
			return def.ErrMailboxWorkerIsFull
		}
		worker, exists = p.workers[workerID]
	} else {
		// 单线程时直接使用 workerID=0
		workerID = 0
		worker, exists = p.workers[workerID]
	}
	p.mu.RUnlock()

	if !exists {
		p.logger.WithContext(evt.GetContext()).Errorf("service[%s] Worker %d not found", p.invoker.GetServiceName(), workerID)
		return def.ErrMailboxWorkerNotFound
	}

	return worker.SubmitEvent(evt)
}

func (p *WorkerPool) resizeWorkers(newSize int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if newSize == p.workerCount {
		return
	}

	if newSize > p.workerCount {
		// 扩容：新增 worker，并受 MaxWorkerNum 约束
		maxWorkers := 0
		if p.conf != nil && p.conf.SchedulePolicy != nil && p.conf.SchedulePolicy.ScalingStrategy != nil {
			maxWorkers = p.conf.SchedulePolicy.ScalingStrategy.MaxWorkerNum
		}
		// 如果未配置 MaxWorkerNum，则认为不限制上限
		if maxWorkers > 0 && newSize > maxWorkers {
			newSize = maxWorkers
		}

		for i := p.workerCount; i < newSize; i++ {
			worker := newWorker(i, p.conf, p) // 使用配置的workerConfig
			p.workers[i] = worker
			worker.Start()
			p.ring.Add(i)
		}
	} else {
		// 缩容：关闭并移除多余 worker
		removeMap := make(map[int]struct{}, p.workerCount-newSize)
		for i := newSize; i < p.workerCount; i++ {
			if worker, exists := p.workers[i]; exists {
				// 停止worker时会自动将队列中所有事件处理完成
				worker.Stop()
				delete(p.workers, i)
				removeMap[i] = struct{}{}
			}
		}
		// 一次性移除哈希环上的节点
		p.ring.RemoveMany(removeMap)
	}

	// 更新当前 worker 数量
	p.workerCount = newSize
}

// 自动调整 worker 数量
func (p *WorkerPool) autoScaleWorkers() {
	defer p.wg.Done()
	if p.autoScaler == nil {
		strategy, err := BuildStrategy(p.conf.SchedulePolicy.ScalingStrategy)
		if err != nil {
			p.logger.Panic(err)
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

	return conf
}
