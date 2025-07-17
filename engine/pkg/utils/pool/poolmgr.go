// Package pool
// @Title  title
// @Description  desc
// @Author  yr  2025/7/17
// @Update  yr  2025/7/17
package pool

import (
	"runtime"
	"sync"
	"sync/atomic"
)

type Stats struct {
	Name        string
	CurrentSize int64
	HitCount    int64
	MissCount   int64
	TotalAlloc  int64
}
type SyncPoolWrapper[T any] struct {
	name      string
	pool      sync.Pool
	stats     Stats
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

func WithUnref[T any](f func(T)) Option[T] {
	return func(p *SyncPoolWrapper[T]) {
		p.unrefFunc = f
	}
}

// NewSyncPoolWrapper 构建一个缓存池
//
// 这是一个通用的缓存池, 几乎可以适用于大部分需要使用缓存的场景
func NewSyncPoolWrapper[T any](name string, newFunc func() T, opts ...Option[T]) *SyncPoolWrapper[T] {
	stats := Stats{Name: name}
	p := &SyncPoolWrapper[T]{
		name:    name,
		newFunc: newFunc,
		stats:   stats,
	}

	p.pool = sync.Pool{New: func() any {
		atomic.AddInt64(&p.stats.MissCount, 1)
		atomic.AddInt64(&p.stats.TotalAlloc, 1)
		return newFunc()
	}}

	for _, opt := range opts {
		opt(p)
	}

	return p
}
func (p *SyncPoolWrapper[T]) Get() T {
	val := p.pool.Get().(T)
	atomic.AddInt64(&p.stats.HitCount, 1)

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

	p.pool.Put(t)
	atomic.AddInt64(&p.stats.CurrentSize, 1)
}

// Stats 获取当前统计信息
func (p *SyncPoolWrapper[T]) Stats() Stats {
	return Stats{
		Name:        p.name,
		HitCount:    atomic.LoadInt64(&p.stats.HitCount),
		MissCount:   atomic.LoadInt64(&p.stats.MissCount),
		CurrentSize: atomic.LoadInt64(&p.stats.CurrentSize),
		TotalAlloc:  atomic.LoadInt64(&p.stats.TotalAlloc),
	}
}

type pPool[T any] struct {
	_        [cacheLineSize]byte
	queue    []T // 本地无锁队列
	_        [cacheLineSize]byte
	capacity int
	_        [cacheLineSize]byte
}

type PerPPoolWrapper[T any] struct {
	name       string
	stats      Stats
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
// 因此适合在单一 goroutine 中频繁复用对象，生命周期受控，性能要求极高的场景。
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
func NewPerPPoolWrapper[T any](name string, size int, newFunc func() T, options ...Option[T]) *PerPPoolWrapper[T] {
	numProcs := runtime.GOMAXPROCS(0) // 获取当前运行的CPU核数
	p := &PerPPoolWrapper[T]{
		name:       name,
		newFunc:    newFunc,
		localPools: make([]*pPool[T], numProcs),
	}
	for i := range p.localPools {
		p.localPools[i] = &pPool[T]{
			queue:    make([]T, 0, size/numProcs+1),
			capacity: size / numProcs,
		}
	}
}
