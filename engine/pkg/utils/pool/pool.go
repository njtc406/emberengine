package pool

import (
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/mpmc"
	"runtime"
	"sync"
	_ "unsafe"
)

const cacheLineSize = 64

type IPoolData interface {
	inf.IReset
	inf.IDataDef
}

type Pool[T inf.IReset] struct {
	_        [cacheLineSize]byte
	queue    *mpmc.Queue[T] // 热点数据缓存
	_        [cacheLineSize]byte
	syncPool sync.Pool
	_        [cacheLineSize]byte
}

func NewPool[T inf.IReset](queueSize int, newFun func() T) *Pool[T] {
	return &Pool[T]{
		queue: mpmc.NewQueue[T](int64(queueSize)),
		syncPool: sync.Pool{
			New: func() any {
				return newFun()
			},
		},
	}
}

func (pool *Pool[T]) Get() T {
	t, ok := pool.queue.Pop()
	if !ok {
		t = pool.syncPool.Get().(T)
	}
	t.Reset()
	return t
}

func (pool *Pool[T]) Put(data T) {
	data.Reset()
	if !pool.queue.Push(data) {
		pool.syncPool.Put(data)
	}
}

// ChannelPool 不推荐使用,单线程模式下比Pool慢一点点,多线程模式下比pool慢10倍以上
type ChannelPool[T inf.IReset] struct {
	_        [cacheLineSize]byte
	c        chan T
	_        [cacheLineSize]byte
	syncPool sync.Pool
	_        [cacheLineSize]byte
}

func NewChannelPool[T inf.IReset](size int, newFun func() T) *ChannelPool[T] {
	return &ChannelPool[T]{
		c: make(chan T, size),
		syncPool: sync.Pool{
			New: func() any {
				return newFun()
			},
		},
	}
}

func (pool *ChannelPool[T]) Get() T {
	select {
	case t := <-pool.c:
		t.Reset()
		return t
	default:
		t := pool.syncPool.Get().(T)
		t.Reset()
		return t
	}
}

func (pool *ChannelPool[T]) Put(data T) {
	data.Reset()
	select {
	case pool.c <- data:
	default:
		pool.syncPool.Put(data)
	}
}

type ExtendedPool[T IPoolData] struct {
	_        [cacheLineSize]byte
	queue    *mpmc.Queue[T] //最大缓存的数量
	_        [cacheLineSize]byte
	syncPool sync.Pool
	_        [cacheLineSize]byte
}

func NewExtendedPool[T IPoolData](queueSize int, newFun func() T) *ExtendedPool[T] {
	return &ExtendedPool[T]{
		queue: mpmc.NewQueue[T](int64(queueSize)),
		syncPool: sync.Pool{
			New: func() any { return newFun() },
		},
	}
}

func (pool *ExtendedPool[T]) Get() T {
	data, ok := pool.queue.Pop()
	if !ok {
		data = pool.syncPool.Get().(T)
	}

	if data.IsRef() {
		log.SysLogger.Panic("Pool data is in use.")
	}
	data.Reset()
	data.Ref()
	return data
}

func (pool *ExtendedPool[T]) Put(data T) {
	if data.IsRef() == false {
		//panic("Repeatedly freeing memory")
		log.SysLogger.Panic("Repeatedly freeing memory")
	}
	//提前解引用,防止递归释放
	data.UnRef()
	data.Reset()
	//再次解引用，防止Rest时错误标记
	data.UnRef()

	if !pool.queue.Push(data) {
		// 队列已满,放回syncPool
		pool.syncPool.Put(data)
	}
}

//go:linkname procPin runtime.procPin
func procPin() int

//go:linkname procUnpin runtime.procUnpin
func procUnpin()

type PrePPool[T inf.IReset] struct {
	_          [cacheLineSize]byte
	localPools []*localPool[T] // 每个P一个本地池
	_          [cacheLineSize]byte
	globalPool sync.Pool // 全局后备池
	_          [cacheLineSize]byte
}

type localPool[T inf.IReset] struct {
	_        [cacheLineSize]byte
	queue    []T // 本地无锁队列
	_        [cacheLineSize]byte
	capacity int
	_        [cacheLineSize]byte
}

// NewPrePPool pre-P缓存池,为每个P分配一个本地缓存池,减少并发竞争(推荐需要高并发的地方使用,测试数据可以看test)
func NewPrePPool[T inf.IReset](size int, newFunc func() T) *PrePPool[T] {
	numProcs := runtime.GOMAXPROCS(0) // 获取当前运行的CPU核数
	p := &PrePPool[T]{
		localPools: make([]*localPool[T], numProcs), // 为每个线程创建一个本地缓存池
		globalPool: sync.Pool{
			New: func() interface{} { return newFunc() },
		},
	}

	for i := range p.localPools {
		p.localPools[i] = &localPool[T]{
			queue:    make([]T, 0, size/numProcs+1),
			capacity: size / numProcs,
		}
	}
	return p
}

func (p *PrePPool[T]) Get() T {
	// 1. 获取当前P的本地池
	pid := procPin()
	local := p.localPools[pid]

	// 2. 从本地池获取
	if len(local.queue) > 0 {
		obj := local.queue[len(local.queue)-1]
		local.queue = local.queue[:len(local.queue)-1]
		procUnpin()
		return obj
	}
	procUnpin()

	// 3. 从全局池获取
	obj := p.globalPool.Get().(T)
	obj.Reset()
	return obj
}

func (p *PrePPool[T]) Put(obj T) {
	obj.Reset()

	// 1. 尝试放回本地池
	pid := procPin()
	local := p.localPools[pid]

	if len(local.queue) < local.capacity {
		local.queue = append(local.queue, obj)
		procUnpin()
		return
	}
	procUnpin()

	// 2. 放回全局池
	p.globalPool.Put(obj)
}

type PrePPoolEx[T IPoolData] struct {
	_          [cacheLineSize]byte
	localPools []*localPoolEx[T] // 每个P一个本地池
	_          [cacheLineSize]byte
	globalPool sync.Pool // 全局后备池
	_          [cacheLineSize]byte
}

type localPoolEx[T IPoolData] struct {
	_        [cacheLineSize]byte
	queue    []T // 本地无锁队列
	_        [cacheLineSize]byte
	capacity int
	_        [cacheLineSize]byte
}

// NewPrePPoolEx pre-P缓存池,为每个P分配一个本地缓存池,减少并发竞争(推荐需要高并发的地方使用,测试数据可以看test)
func NewPrePPoolEx[T IPoolData](size int, newFunc func() T) *PrePPoolEx[T] {
	numProcs := runtime.GOMAXPROCS(0) // 获取当前运行的CPU核数
	p := &PrePPoolEx[T]{
		localPools: make([]*localPoolEx[T], numProcs), // 为每个线程创建一个本地缓存池
		globalPool: sync.Pool{
			New: func() interface{} { return newFunc() },
		},
	}

	for i := range p.localPools {
		p.localPools[i] = &localPoolEx[T]{
			queue:    make([]T, 0, size/numProcs+1),
			capacity: size / numProcs,
		}
	}
	return p
}

func (p *PrePPoolEx[T]) Get() T {
	// 1. 获取当前P的本地池
	pid := procPin()
	local := p.localPools[pid]

	// 2. 从本地池获取
	if len(local.queue) > 0 {
		obj := local.queue[len(local.queue)-1]
		local.queue = local.queue[:len(local.queue)-1]
		procUnpin()
		obj.Reset()
		obj.Ref()
		return obj
	}
	procUnpin()

	// 3. 从全局池获取
	obj := p.globalPool.Get().(T)

	obj.Reset()
	obj.Ref()
	return obj
}

func (p *PrePPoolEx[T]) Put(obj T) {
	obj.UnRef()
	obj.Reset()
	//再次解引用，防止Rest时错误标记
	obj.UnRef()

	// 1. 尝试放回本地池
	pid := procPin()
	local := p.localPools[pid]

	if len(local.queue) < local.capacity {
		local.queue = append(local.queue, obj)
		procUnpin()
		return
	}
	procUnpin()

	// 2. 放回全局池
	p.globalPool.Put(obj)
}
