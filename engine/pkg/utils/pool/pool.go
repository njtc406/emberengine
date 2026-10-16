// Package pool
// @Title  title
// @Description  desc
// @Author  yr  2025/7/17
// @Update  yr  2025/7/17
package pool

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	_ "unsafe"
)

const cacheLineSize = 64

var (
	_                    IPool[any] = (*SyncPoolWrapper[any])(nil)
	defaultStatsRegistry            = NewPoolStatsRegistry()
)

type PoolStatsRegistry struct {
	mu     sync.RWMutex
	states map[string]IStatsRecorder
}

func NewPoolStatsRegistry() *PoolStatsRegistry {
	return &PoolStatsRegistry{states: make(map[string]IStatsRecorder)}
}

func (r *PoolStatsRegistry) Register(name string, recorder IStatsRecorder) {
	if r == nil || recorder == nil || name == "" {
		return
	}
	r.mu.Lock()
	r.states[name] = recorder
	r.mu.Unlock()
}

func (r *PoolStatsRegistry) GetStats() string {
	if r == nil {
		return ""
	}
	r.mu.RLock()
	names := make([]string, 0, len(r.states))
	for name := range r.states {
		names = append(names, name)
	}
	sort.Strings(names)
	stats := make([]string, 0, len(names))
	for _, name := range names {
		stat := r.states[name].String()
		if stat == "" {
			continue
		}
		stats = append(stats, stat)
	}
	r.mu.RUnlock()
	return fmt.Sprintf("%s", strings.Join(stats, "\n"))
}

func SetDefaultPoolStatsRegistry(r *PoolStatsRegistry) {
	if r == nil {
		return
	}
	defaultStatsRegistry = r
}

func GetDefaultPoolStatsRegistry() *PoolStatsRegistry {
	return defaultStatsRegistry
}

func GetPoolStats() string {
	return defaultStatsRegistry.GetStats()
}

type SyncPoolWrapper[T any] struct {
	pool      sync.Pool
	recorder  IStatsRecorder
	newFunc   func() T
	resetFunc func(T)
	refFunc   func(T)
	unrefFunc func(T) bool
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

func WithUnRef[T any](f func(T) bool) Option[T] {
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

	// 注意：sync.Pool 无法直接得知“Get 是否命中”。
	// 这里显式判断 nil（表示本轮需新分配），从而统计 hit/miss。
	// current 用作“in-use”（未归还数），用于泄露观测：正常情况下最终应回到 0。
	p.pool = sync.Pool{}

	for _, opt := range opts {
		opt(p)
	}

	// 注册统计记录器
	defaultStatsRegistry.Register(p.recorder.stats().Name, p.recorder)

	return p
}
func (p *SyncPoolWrapper[T]) Get() T {
	raw := p.pool.Get()
	var val T
	if raw == nil {
		p.recorder.incTotalAlloc()
		p.recorder.incMiss()
		val = p.newFunc()
	} else {
		p.recorder.incHit()
		val = raw.(T)
	}
	p.recorder.incCurrentSize() // syncPool: in-use（未归还数）

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
		if !p.unrefFunc(t) { // 这里是为了防止reset中误标记
			return
		}
	}
	if p.resetFunc != nil {
		p.resetFunc(t)
	}

	p.recorder.decCurrentSize()
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

func WithPUnref[T any](f func(T) bool) POption[T] {
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
	unrefFunc  func(T) bool
	localPools []*pPool[T] // 每个P一个本地池
	globalPool sync.Pool   // 全局后备池
}

// NewPerPPoolWrapper 创建依赖 Go runtime 内部 pin 机制的 per-P 对象缓存。
// size 必须非负，表示每个 P 的本地槽位上限，不是池的对象总数或内存硬上限。
//
// 使用契约：
//   - 构造完成后安全发布；options 和回调配置不得在使用中并发修改。
//   - 构造后 GOMAXPROCS 不得超过初始化值；本地数组不扩容，越界可能在 pin 时 fatal。
//   - Get 后对象由调用方独占，Put 后不得再访问，也不得重复归还。
//     可以通过 channel 等同步手段完整转移所有权，由另一 goroutine 归还；
//     同 goroutine 配对并非必要条件，也不保证两次调用处于同一个 P。
//   - new/reset/ref/unref 回调在 pin 区间外执行；回调共享状态须自行同步，
//     不得发布尚在初始化/重置的对象或重入归还同一对象。
//
// 实现边界：所有本地 slice 访问须保持在短小、不可阻塞的 pin 区间内。
// 本地访问不加互斥锁，但后备 sync.Pool、分配及 GC 不提供整体 lock-free 保证。
// pin 使用内部运行时接口，不是公开的 Go 同步契约；升级工具链须重新验证。
// 本实现未提供 TSan 可见的本地交接同步：-race 告警不能单独证明并行破坏，
// 也不能据此忽略对象所有权或回调中的真实竞争。
//
// 内存边界：本地缓存中的对象会保留引用，不随 GC 自动清理；弹出时清空槽位。
// 被引用对象可继续持有任意大小的对象图。全局后备 sync.Pool 则允许 GC 清理。
// 适合 P 上限稳定、所有权清晰且愿意保留缓存的高频临时对象；
// 需要动态扩 P 或标准工具链同步支持时，优先使用 SyncPoolWrapper。
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

	// 注册统计记录器
	defaultStatsRegistry.Register(p.recorder.stats().Name, p.recorder)
	return p
}

// Get 从当前逻辑 P 对应的本地池中获取一个对象。
// 若本地池为空，则退回到全局 sync.Pool 获取（可能发生 GC 分配）。
//
// 返回对象的独占所有权；本地槽位访问在 pin 区间内，回调在 unpin 后执行。
// 弹出时清空底层槽位，避免缓存继续持有已转交调用方的对象。
func (p *PerPPoolWrapper[T]) Get() T {
	pid := procPin()
	local := p.localPools[pid]

	if len(local.queue) > 0 {
		last := len(local.queue) - 1
		obj := local.queue[last]
		var zero T
		local.queue[last] = zero
		local.queue = local.queue[:last]
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
// 调用方必须拥有对象的独占归还权，且调用后不再访问对象。
// 归还使用当前 P，不要求与 Get 的 P 或 goroutine 相同；跨 goroutine 须同步转移所有权。
func (p *PerPPoolWrapper[T]) Put(obj T) {
	if p.unrefFunc != nil {
		if !p.unrefFunc(obj) {
			return
		}
	}
	if p.resetFunc != nil {
		p.resetFunc(obj)
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
