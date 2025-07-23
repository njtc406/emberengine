// Package pool
// @Title  title
// @Description  desc
// @Author  yr  2025/7/17
// @Update  yr  2025/7/17
package pool

import (
	"runtime"
	"sync"
	_ "unsafe"
)

const cacheLineSize = 64

type SyncPoolWrapper[T any] struct {
	pool      sync.Pool
	recorder  IStatsRecorder
	newFunc   func() T
	resetFunc func(T)
	refFunc   func(T)
	unrefFunc func(T)
}

type Option[T any] func(p *SyncPoolWrapper[T])

func WithReset[T any](f func(T)) Option[T] {
	return func(p *SyncPoolWrapper[T]) {
		p.resetFunc = f
	}
}

func WithRef[T any](f func(T)) Option[T] {
	return func(p *SyncPoolWrapper[T]) {
		p.refFunc = f
	}
}

func WithUnRef[T any](f func(T)) Option[T] {
	return func(p *SyncPoolWrapper[T]) {
		p.unrefFunc = f
	}
}

// NewSyncPoolWrapper 构建一个缓存池
//
// 这是一个通用的缓存池, 几乎可以适用于大部分需要使用缓存的场景
func NewSyncPoolWrapper[T any](newFunc func() T, recorder IStatsRecorder, opts ...Option[T]) IPool[T] {
	if recorder == nil {
		recorder = &nopStats{}
	}
	p := &SyncPoolWrapper[T]{
		newFunc:  newFunc,
		recorder: recorder,
	}

	p.pool = sync.Pool{New: func() any {
		p.recorder.incTotalAlloc()
		p.recorder.incMiss()
		return newFunc()
	}}

	for _, opt := range opts {
		opt(p)
	}

	return p
}
func (p *SyncPoolWrapper[T]) Get() T {
	val := p.pool.Get().(T)
	p.recorder.decCurrentSize() // 这个地方的计数只是代表借出了多少个,配合put来查看有没有泄露

	if p.resetFunc != nil {
		p.resetFunc(val)
	}
	if p.refFunc != nil {
		p.refFunc(val)
	}

	return val
}

func (p *SyncPoolWrapper[T]) Put(t T) {
	if p.unrefFunc != nil {
		p.unrefFunc(t)
	}
	if p.resetFunc != nil {
		p.resetFunc(t)
	}
	if p.unrefFunc != nil {
		p.unrefFunc(t) // 这里是为了防止reset中误标记
	}

	p.recorder.incCurrentSize()
	p.pool.Put(t)
}

// Stats 获取当前统计信息
func (p *SyncPoolWrapper[T]) Stats() *Stats {
	out := p.recorder.stats()
	return &out
}

//go:linkname procPin runtime.procPin
func procPin() int

//go:linkname procUnpin runtime.procUnpin
func procUnpin()

type POption[T any] func(p *PerPPoolWrapper[T])

func WithPReset[T any](f func(T)) POption[T] {
	return func(p *PerPPoolWrapper[T]) {
		p.resetFunc = f
	}
}

func WithPRef[T any](f func(T)) POption[T] {
	return func(p *PerPPoolWrapper[T]) {
		p.refFunc = f
	}
}

func WithPUnref[T any](f func(T)) POption[T] {
	return func(p *PerPPoolWrapper[T]) {
		p.unrefFunc = f
	}
}

type pPool[T any] struct {
	_        [cacheLineSize]byte
	queue    []T // 本地无锁队列
	capacity int
	_        [cacheLineSize]byte
}

type PerPPoolWrapper[T any] struct {
	recorder   IStatsRecorder
	newFunc    func() T
	resetFunc  func(T)
	refFunc    func(T)
	unrefFunc  func(T)
	localPools []*pPool[T] // 每个P一个本地池
	globalPool sync.Pool   // 全局后备池
}

// NewPerPPoolWrapper 创建一个基于 per-P 局部缓存的对象池，适用于高性能、低延迟且对 GC 极度敏感的场景
//
// 该对象池为每个逻辑 P 分配一个本地池（非线程安全），避免全局锁竞争与原子操作开销，
// 因此适合在单一 goroutine 中频繁复用对象，生命周期受控，对GC频率极度敏感，性能要求极高的场景。
// 极度不建议 在不同 goroutine 中配对调用 Get 与 Put，否则可能导致缓存失效、内存突增或逻辑错误
//
// 适用场景：
//   - 游戏逻辑帧中高频创建/复用的临时对象（如消息体、组件快照等）；
//   - rpc消息message结构等单goroutine频繁调用结构（请一定确保对象不会跨 goroutine!!）；
//   - 对 GC 敏感的大对象池（避免被 GC 回收带来的性能抖动）；
//
// 不适用场景：
//   - 对象在多个 goroutine 之间传递、释放或复用（例如 channel、异步任务）；
//   - 对象生命周期难以控制或不明确；
//   - 异步任务释放、跨模块释放等情况（推荐使用 sync.Pool）；
//
// 注意事项：
//   - 本池设计为 **goroutine 局部缓存**，**禁止跨 goroutine 使用 Get 和 Put**；
//   - Put 到错误 P 的本地池会导致缓存污染和性能下降；
//   - 池中的对象不会被 GC 自动清理，需考虑内存占用长期增长的风险；
func NewPerPPoolWrapper[T any](size int, newFunc func() T, recorder IStatsRecorder, options ...POption[T]) IPool[T] {
	if recorder == nil {
		recorder = &nopStats{}
	}
	numProcs := runtime.GOMAXPROCS(0) // 获取当前运行的CPU核数
	p := &PerPPoolWrapper[T]{
		newFunc:    newFunc,
		recorder:   recorder,
		localPools: make([]*pPool[T], numProcs),
	}
	p.globalPool = sync.Pool{
		New: func() interface{} {
			p.recorder.incTotalAlloc()
			return newFunc()
		},
	}
	for i := range p.localPools {
		p.localPools[i] = &pPool[T]{
			queue:    make([]T, 0, size),
			capacity: size,
		}
	}
	for _, opt := range options {
		opt(p)
	}
	return p
}

// Get 从当前逻辑 P 对应的本地池中获取一个对象。
// 若本地池为空，则退回到全局 sync.Pool 获取（可能发生 GC 分配）。
//
// 本方法为 goroutine 局部缓存设计，不应跨 goroutine 获取对象。
// 不遵守此限制可能导致缓存命中率下降，甚至破坏池内部结构
func (p *PerPPoolWrapper[T]) Get() T {
	pid := procPin()
	local := p.localPools[pid]

	if len(local.queue) > 0 {
		obj := local.queue[len(local.queue)-1]
		local.queue = local.queue[:len(local.queue)-1]
		procUnpin()
		p.recorder.incHit()
		p.recorder.decCurrentSize()

		if p.resetFunc != nil {
			p.resetFunc(obj)
		}
		if p.refFunc != nil {
			p.refFunc(obj)
		}
		return obj
	}
	procUnpin()

	// 本地 miss，从全局池取
	obj := p.globalPool.Get().(T)
	p.recorder.incMiss()

	if p.resetFunc != nil {
		p.resetFunc(obj)
	}
	if p.refFunc != nil {
		p.refFunc(obj)
	}

	return obj
}

// Put 将对象放回当前逻辑 P 的本地池中，以备后续复用。
// 若本地池已满，则放入全局 sync.Pool 作为后备缓存。
//
// 注意：仅允许将对象放回获取它的 goroutine 所在的本地池。
// 跨 goroutine 释放对象将导致缓存污染，增加误命中风险或内存泄露。
func (p *PerPPoolWrapper[T]) Put(obj T) {
	if p.unrefFunc != nil {
		p.unrefFunc(obj)
	}
	if p.resetFunc != nil {
		p.resetFunc(obj)
	}
	if p.unrefFunc != nil {
		p.unrefFunc(obj) // 防止 reset 中标记失效
	}

	pid := procPin()
	local := p.localPools[pid]
	if len(local.queue) < local.capacity {
		local.queue = append(local.queue, obj)
		procUnpin()
		p.recorder.incCurrentSize()
		return
	}
	procUnpin()

	// TODO 这里后面考虑一下有没必要做扩容策略
	// 回退到全局池（本地缓存失败）
	p.globalPool.Put(obj)
	p.recorder.incOverflow()
}

func (p *PerPPoolWrapper[T]) Stats() *Stats {
	out := p.recorder.stats()
	return &out
}
