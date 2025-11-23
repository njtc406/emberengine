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

type queue[T any] interface {
	Push(T) bool
	Pop() (T, bool)
	BatchPop(int) []T
	Empty() bool
	Len() int
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
}

func fixConf(conf *config.MailboxConf) *config.MailboxConf {
	if conf == nil {
		conf = &config.MailboxConf{
			MailboxType:       "simple",
			WorkerNum:         1,  // 默认单线程
			VirtualWorkerRate: 24, // rate建议值稍微大一点,hash分布会更均匀
			DefaultConf: &config.DefaultMailboxConf{
				BackoffBaseDelay:  1 * time.Millisecond, // 退避基础时间(默认1毫秒)
				BackoffMaxDelay:   16 * time.Second,     // 最大退避时间(默认16秒)
				BackoffMaxRetries: 3,                    // 最大重试次数(默认3次)
			},
		}
		return conf
	}

	if conf.MailboxType == "" {
		conf.MailboxType = "simple"
	}

	if conf.WorkerNum <= 0 {
		conf.WorkerNum = 1
	}
	if conf.VirtualWorkerRate <= 0 {
		conf.VirtualWorkerRate = 24
	}
	if conf.DefaultConf == nil {
		conf.DefaultConf = &config.DefaultMailboxConf{
			BackoffBaseDelay:  1 * time.Millisecond, // 退避基础时间(默认1毫秒)
			BackoffMaxDelay:   16 * time.Second,     // 最大退避时间(默认16秒)
			BackoffMaxRetries: 3,                    // 最大重试次数(默认3次)
		}
	} else {
		if conf.DefaultConf.BackoffBaseDelay <= 0 {
			conf.DefaultConf.BackoffBaseDelay = 1 * time.Millisecond // 退避基础时间(默认1毫秒)
		}
		if conf.DefaultConf.BackoffMaxDelay <= 0 {
			conf.DefaultConf.BackoffMaxDelay = 16 * time.Second // 最大退避时间(默认16秒)
		}
		if conf.DefaultConf.BackoffMaxRetries <= 0 {
			conf.DefaultConf.BackoffMaxRetries = 3 // 最大重试次数(默认3次)
		}
	}
	return conf
}

func NewWorkerPool(conf *config.MailboxConf, logger log.ILogger, invoker inf.IMessageInvoker, middlewares ...inf.IMailboxMiddleware) *WorkerPool {
	conf = fixConf(conf)
	ctx, cancel := context.WithCancel(context.Background())
	return &WorkerPool{
		conf:        conf,
		workers:     make(map[int]inf.IMailboxWorker, conf.WorkerNum),
		invoker:     invoker,
		ring:        hashring.NewHashRing[int](conf.VirtualWorkerRate),
		middlewares: middlewares,
		ctx:         ctx,
		cancel:      cancel,
		logger:      logger,
	}
}

func (p *WorkerPool) Start() {
	p.logger.Debugf("Starting service[%s] mailbox workers:%d", p.invoker.GetServiceName(), p.conf.WorkerNum)
	p.mu.Lock()
	for i := 0; i < p.conf.WorkerNum; i++ {
		worker := newWorker(i, p.conf, p) // 使用配置的workerConfig
		if worker == nil {
			p.logger.Fatalf("service[%s] Failed to create worker, conf:%v", p.invoker.GetServiceName(), p.conf)
		}
		p.workers[i] = worker
		worker.Start()
		// 将 worker 加入到哈希环中（这里每个都加进入,但是单线程时可能不会使用）
		p.ring.Add(i)
	}

	p.mu.Unlock()

	for _, middleware := range p.middlewares {
		middleware.MailboxStarted()
	}

	if p.conf.DynamicWorkerScaling {
		p.wg.Add(1)
		go p.autoScaleWorkers()
	}

	p.logger.Debugf("Started service[%s] mailbox workers:%d", p.invoker.GetServiceName(), p.conf.WorkerNum)
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

func (p *WorkerPool) DispatchEvent(evt inf.IEvent) error {
	// 通过一致性哈希+虚拟节点解决 将事件分派给worker执行
	var worker inf.IMailboxWorker
	var exists bool
	var workerID int

	p.mu.RLock()
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
		// 单线程时直接使用workerID=0
		worker, exists = p.workers[workerID]
	}
	p.mu.RUnlock()

	if !exists {
		p.logger.WithContext(evt.GetContext()).Errorf("service[%s] Worker %d not found", p.invoker.GetServiceName(), workerID)
		return def.ErrMailboxWorkerIsFull
	}

	return worker.SubmitEvent(evt)
}

func (p *WorkerPool) resizeWorkers(newSize int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if newSize == p.conf.WorkerNum {
		return
	}

	if newSize > p.conf.WorkerNum {
		if newSize < p.conf.Strategy.MaxWorkerNum {
			// 增加 workers
			for i := p.conf.WorkerNum; i < newSize; i++ {
				worker := newWorker(i, p.conf, p) // 使用配置的workerConfig
				p.workers[i] = worker
				worker.Start()
				p.ring.Add(i)
			}
		}
	} else {
		// 减少 workers
		// TODO 这里应该只能减少空闲worker
		for i := newSize; i < p.conf.WorkerNum; i++ {
			if worker, exists := p.workers[i]; exists {
				worker.Stop()
				delete(p.workers, i)
				p.ring.Remove(i)
			}
		}
	}

	// 更新当前 worker 数量
	p.conf.WorkerNum = newSize
}

// 自动调整 worker 数量
func (p *WorkerPool) autoScaleWorkers() {
	defer p.wg.Done()
	if p.autoScaler == nil {
		strategy, err := BuildStrategy(p.conf.Strategy)
		if err != nil {
			log.SysLogger.Panic(err)
		}
		p.autoScaler = &AutoScaler{
			conf:     p.conf.Strategy,
			Strategy: strategy,
		}
	}

	// TODO 定时触发检查这部分先这么用吧,主要还没想到什么好的方式来为每种策略定制一个检查机制
	// TODO 主要是嵌套策略里面可能包含了自驱动和外部驱动两种类型的策略,不太好分开

	ticker := time.NewTicker(p.conf.Strategy.ResizeCoolDown) // 调整间隔
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
