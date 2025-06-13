// Package mailbox
// @Title  服务的工作线程,接收并处理事件
// @Description  desc
// @Author  yr  2025/2/8
// @Update  yr  2025/2/8
package mailbox

import (
	"context"
	"fmt"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/utils/hashring"
	"github.com/njtc406/emberengine/engine/pkg/utils/mpsc"
	"reflect"
	"runtime/debug"
	"sync"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/config"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/profiler"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
)

type Scaler interface {
	ShouldResize(current int, workers []*Worker) (newSize int, reason string, ok bool)
}

type queue[T any] interface {
	Push(T) bool
	Pop() (T, bool)
	Empty() bool
	Len() int
}

type WorkerPool struct {
	conf        config.WorkerConf
	mu          sync.RWMutex
	wg          sync.WaitGroup
	ctx         context.Context
	cancel      context.CancelFunc
	workers     map[int]*Worker
	ring        *hashring.HashRing[int]  // 一致性哈希环，用于分派事件
	invoker     inf.IMessageInvoker      // 消息处理器
	middlewares []inf.IMailboxMiddleware // 中间件
	profiler    *profiler.Profiler       // 性能分析
	autoScaler  Scaler                   // 自动扩容器
}

func fixConf(conf *config.WorkerConf) *config.WorkerConf {
	if conf == nil {
		conf = &config.WorkerConf{
			DynamicWorkerScaling: false,
			SystemMailboxSize:    16,
			UserMailboxSize:      128,
			VirtualWorkerRate:    10,
			WorkerNum:            1,
			MaxWorkerNum:         1,
		}
		return conf
	}

	if conf.WorkerNum <= 0 {
		conf.WorkerNum = 1
	}
	if conf.MaxWorkerNum <= 0 {
		conf.MaxWorkerNum = 1
	}
	if conf.UserMailboxSize <= 0 {
		conf.UserMailboxSize = 128
	}
	if conf.SystemMailboxSize <= 0 {
		conf.SystemMailboxSize = 16
	}
	return conf
}

func NewWorkerPool(conf *config.WorkerConf, invoker inf.IMessageInvoker, middlewares ...inf.IMailboxMiddleware) *WorkerPool {
	conf = fixConf(conf)
	ctx, cancel := context.WithCancel(context.Background())
	return &WorkerPool{
		conf:        *conf,
		workers:     make(map[int]*Worker, conf.WorkerNum),
		invoker:     invoker,
		ring:        hashring.NewHashRing[int](conf.VirtualWorkerRate),
		middlewares: middlewares,
		ctx:         ctx,
		cancel:      cancel,
	}
}

func (p *WorkerPool) Start() {
	p.mu.Lock()
	for i := 0; i < p.conf.WorkerNum; i++ {
		worker := newWorker(p, i)
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
		worker.stop()
	}
	p.ring.Clear()
	p.workers = nil
}

func (p *WorkerPool) DispatchEvent(evt inf.IEvent) error {
	// 通过一致性哈希+虚拟节点解决 将事件分派给worker执行
	var worker *Worker
	var exists bool
	var workerID int

	p.mu.RLock()
	if len(p.workers) > 1 {
		var ok bool
		workerID, ok = p.ring.Get(evt.GetKey())
		if !ok {
			log.SysLogger.Errorf("No worker available in hash ring")
			p.mu.RUnlock()
			return def.ErrEventChannelIsFull
		}
		worker, exists = p.workers[workerID]
	} else {
		// 单线程时直接使用workerID=0
		workerID = 0
		worker, exists = p.workers[workerID]
	}
	p.mu.RUnlock()

	if !exists {
		log.SysLogger.Errorf("Worker %d not found", workerID)
		return def.ErrEventChannelIsFull
	}
	switch evt.GetPriority() {
	case def.PrioritySys:
		return worker.submitSysEvent(evt)
	default:
		return worker.submitUserEvent(evt)
	}
}

func (p *WorkerPool) resizeWorkers(newSize int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if newSize == p.conf.WorkerNum {
		return
	}

	if newSize > p.conf.WorkerNum {
		if newSize < p.conf.MaxWorkerNum {
			// 增加 workers
			for i := p.conf.WorkerNum; i < newSize; i++ {
				worker := newWorker(p, i)
				p.workers[i] = worker
				worker.Start()
				p.ring.Add(i)
			}
		}
	} else {
		// 减少 workers
		for i := newSize; i < p.conf.WorkerNum; i++ {
			if worker, exists := p.workers[i]; exists {
				worker.stop()
				delete(p.workers, i)
				p.ring.Remove(i)
			}
		}
	}

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
			MinWorkers:     p.conf.WorkerNum,
			MaxWorkers:     p.conf.MaxWorkerNum,
			GrowthFactor:   p.conf.GrowthFactor,
			ShrinkFactor:   p.conf.ShrinkFactor,
			ResizeCoolDown: p.conf.ResizeCoolDown,
			Strategy:       strategy,
		}
	}

	// TODO 定时触发检查这部分先这么用吧,主要还没想到什么好的方式来为每种策略定制一个检查机制
	// TODO 主要是嵌套策略里面可能包含了自驱动和外部驱动两种类型的策略,不太好分开

	ticker := time.NewTicker(10 * time.Second)
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

			workers := make([]*Worker, 0, len(p.workers))
			for _, w := range p.workers {
				workers = append(workers, w)
			}
			current := len(workers)
			p.mu.RUnlock()

			if newSize, reason, ok := p.autoScaler.ShouldResize(current, workers); ok {
				log.SysLogger.Infof("resizing from %d -> %d: %s", current, newSize, reason)
				p.resizeWorkers(newSize)
			}
		}
	}
}

type Worker struct {
	workerId      int
	closed        bool
	pool          *WorkerPool
	wg            sync.WaitGroup
	userMailbox   queue[inf.IEvent] // 用户消息
	systemMailbox queue[inf.IEvent] // 系统消息(区分出不同级别的消息,这样可以使用系统消息来控制service行为,同时也保证系统消息有更高的执行优先级)
}

func newWorker(pool *WorkerPool, id int) *Worker {
	return &Worker{
		workerId:      id,
		pool:          pool,
		userMailbox:   mpsc.New[inf.IEvent](),
		systemMailbox: mpsc.New[inf.IEvent](),
	}
}

func (w *Worker) submitUserEvent(e inf.IEvent) error {
	if !w.userMailbox.Push(e) {
		return def.ErrEventChannelIsFull
	}
	return nil
}

func (w *Worker) submitSysEvent(e inf.IEvent) error {
	if !w.systemMailbox.Push(e) {
		return def.ErrEventChannelIsFull
	}
	return nil
}

func (w *Worker) Start() {
	w.wg.Add(1)
	go w.run()
}

func (w *Worker) run() {
	//log.SysLogger.Debugf("worker %d start", w.workerId)
	defer w.wg.Done()

	var e inf.IEvent
	var ok bool

	// 这里暂时先屏蔽,后面已经处理过panic了
	//defer func() {
	//	if r := recover(); r != nil {
	//		w.invoker.EscalateFailure(r, e)
	//	}
	//	// 重启listen
	//	w.wg.Add(1)
	//	go w.listen()
	//}()

	defer func() {
		// 退出时检查业务是否处理完成
		for !w.systemMailbox.Empty() {
			if e, ok = w.systemMailbox.Pop(); ok {
				w.safeExec(w.pool.invoker.InvokeSystemMessage, e)
			}
		}

		for !w.userMailbox.Empty() {
			if e, ok = w.userMailbox.Pop(); ok {
				w.safeExec(w.pool.invoker.InvokeUserMessage, e)
			}
		}
	}()

	var backoff = 1
	var maxBackoff = 4
	for !w.closed {
		// 优先处理系统消息
		if e, ok = w.systemMailbox.Pop(); ok {
			w.safeExec(w.pool.invoker.InvokeSystemMessage, e)
			continue
		}

		if e, ok = w.userMailbox.Pop(); ok {
			// 交由业务处理消息
			w.safeExec(w.pool.invoker.InvokeUserMessage, e)
			continue
		}

		// 使用指数退避来减少忙等开销
		if backoff < maxBackoff {
			backoff *= 2
		}
		time.Sleep(time.Microsecond * time.Duration(backoff))

		//runtime.Gosched()
	}
	//log.SysLogger.Debugf("worker %d stopped", w.workerId)
}

func (w *Worker) stop() {
	w.closed = true
	w.wg.Wait()
	w.userMailbox = nil
	w.systemMailbox = nil
	w.pool = nil
	w.workerId = 0
}

func (w *Worker) safeExec(invokeFun func(inf.IEvent), e inf.IEvent) {
	defer func() {
		if r := recover(); r != nil {
			log.SysLogger.Errorf("exec error: %v\ntrace:%s", r, debug.Stack())
			w.pool.invoker.EscalateFailure(r, e)
		}
	}()

	var analyzer *profiler.Analyzer
	if w.pool.profiler != nil {
		analyzer = w.pool.profiler.Push(fmt.Sprintf("[ STATE ]%s", reflect.TypeOf(e).String()))
	}
	invokeFun(e)
	if analyzer != nil {
		analyzer.Pop()
		analyzer = nil
	}

	for _, ms := range w.pool.middlewares {
		ms.MessageReceived(e)
	}
}

func (w *Worker) GetMsgLen() int {
	if w.userMailbox == nil {
		return 0
	}
	return w.userMailbox.Len()
}
