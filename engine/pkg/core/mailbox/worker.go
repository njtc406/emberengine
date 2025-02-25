// Package mailbox
// @Title  服务的工作线程,接收并处理事件
// @Description  desc
// @Author  yr  2025/2/8
// @Update  yr  2025/2/8
package mailbox

import (
	"fmt"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"hash/fnv"
	"math/rand"
	"reflect"
	"runtime/debug"
	"sort"
	"sync"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/config"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/profiler"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/mpmc"
)

var globRand *rand.Rand

const globalSalt = "SOME_UNIQUE_SALT_VALUE"

func init() {
	globRand = rand.New(rand.NewSource(time.Now().UnixNano()))
}

func hashEvent(key string) int {
	// key为空时,随机一个值
	if key == "" {
		return int(globRand.Int31())
	}
	// 使用 FNV-1a 哈希算法
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return int(h.Sum32())
}

type queue[T any] interface {
	Push(T) bool
	Pop() (T, bool)
	Empty() bool
}

// HashRing 表示一个带虚拟节点的一致性哈希环。
type HashRing struct {
	nodes    []int        // 排序后的虚拟节点哈希值
	ring     map[int]int  // 虚拟节点哈希值 -> 实际 workerID 的映射
	replicas int          // 每个节点的虚拟节点数量
	mu       sync.RWMutex // 保护 nodes 和 ring
}

func (h *HashRing) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nodes = nil
	h.ring = nil
	h.replicas = 0
}

type WorkerPool struct {
	conf        config.WorkerConf
	mu          sync.RWMutex
	workers     map[int]*Worker
	ring        *HashRing                // 一致性哈希环，用于分派事件
	invoker     inf.IMessageInvoker      // 消息处理器
	middlewares []inf.IMailboxMiddleware // 中间件
	profiler    *profiler.Profiler
}

func NewWorkerPool(conf *config.WorkerConf, invoker inf.IMessageInvoker, middlewares ...inf.IMailboxMiddleware) *WorkerPool {
	return &WorkerPool{
		conf:        *conf,
		workers:     make(map[int]*Worker, conf.WorkerNum),
		invoker:     invoker,
		ring:        NewHashRing(conf.VirtualWorkerRate),
		middlewares: middlewares,
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
		go p.autoScaleWorkers()
	}
}

func (p *WorkerPool) Stop() {
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
			return def.EventChannelIsFull
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
		return def.EventChannelIsFull
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
		// 增加 workers
		for i := p.conf.WorkerNum; i < newSize; i++ {
			worker := newWorker(p, i)
			p.workers[i] = worker
			worker.Start()
			p.ring.Add(i)
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
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// TODO: 根据配置的策略 调整 worker 数量
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
		userMailbox:   mpmc.NewQueue[inf.IEvent](int64(pool.conf.UserMailboxSize)),
		systemMailbox: mpmc.NewQueue[inf.IEvent](int64(pool.conf.SystemMailboxSize)),
	}
}

func (w *Worker) submitUserEvent(e inf.IEvent) error {
	if !w.userMailbox.Push(e) {
		return def.EventChannelIsFull
	}
	return nil
}

func (w *Worker) submitSysEvent(e inf.IEvent) error {
	if !w.systemMailbox.Push(e) {
		return def.EventChannelIsFull
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
}

func (w *Worker) safeExec(invokeFun func(inf.IEvent), e inf.IEvent) {
	defer func() {
		if r := recover(); r != nil {
			log.SysLogger.Errorf("exec error: %v\ntrace:%s", r, debug.Stack())
			w.pool.invoker.EscalateFailure(r, e)
		}
	}()

	// TODO analyzer可以使用缓存池
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

func NewHashRing(replicas int) *HashRing {
	return &HashRing{
		nodes:    []int{},
		ring:     make(map[int]int),
		replicas: replicas,
	}
}

// Add 将一个 worker（通过 workerID 标识）添加到哈希环中，并生成对应的虚拟节点。
func (h *HashRing) Add(workerID int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := 0; i < h.replicas; i++ {
		// 生成虚拟节点 key，例如 "workerID-副本编号"
		virtualNodeKey := fmt.Sprintf("%s-%d-%d", globalSalt, workerID, i)
		hash := hashEvent(virtualNodeKey)
		h.nodes = append(h.nodes, hash)
		h.ring[hash] = workerID
	}
	sort.Ints(h.nodes)
}

// Remove 将一个 worker 从哈希环中移除，其对应的所有虚拟节点都会被删除。
func (h *HashRing) Remove(workerID int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var newNodes []int
	for _, hash := range h.nodes {
		if h.ring[hash] == workerID {
			delete(h.ring, hash)
		} else {
			newNodes = append(newNodes, hash)
		}
	}
	h.nodes = newNodes
}

// Get 根据传入的 key 计算哈希值，并在哈希环中查找对应的 workerID。
func (h *HashRing) Get(key string) (int, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.nodes) == 0 {
		return 0, false
	}
	hash := hashEvent(key)
	// 二分查找第一个 >= hash 的虚拟节点
	idx := sort.Search(len(h.nodes), func(i int) bool {
		return h.nodes[i] >= hash
	})
	if idx == len(h.nodes) {
		idx = 0
	}
	workerID, ok := h.ring[h.nodes[idx]]

	return workerID, ok
}
