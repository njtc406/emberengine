package pool

import (
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/mpmc"
	"sync"
)

type Pool[T any] struct {
	queue    *mpmc.Queue[T] //最大缓存的数量
	syncPool sync.Pool
}

type IPoolData interface {
	inf.IReset
	inf.IDataDef
}

type PoolEx struct {
	queue    *mpmc.Queue[IPoolData] //最大缓存的数量
	syncPool sync.Pool
}

func (pool *Pool[T]) Get() T {
	t, ok := pool.queue.Pop()
	if ok {
		return t
	}
	return pool.syncPool.Get().(T)
}

func (pool *Pool[T]) Put(data T) {
	if !pool.queue.Push(data) {
		pool.syncPool.Put(data)
	}
}

func NewPool[T any](queueSize int, New func() interface{}) *Pool[T] {
	//var p Pool
	//p.queue = mpmc.NewQueue[T](int64(queueSize))
	//p.syncPool.New = New
	return &Pool[T]{
		queue: mpmc.NewQueue(int64(queueSize)),
		syncPool: sync.Pool{
			New: New,
		},
	}
}

func NewPoolEx(C chan IPoolData, New func() IPoolData) *PoolEx {
	var pool PoolEx
	pool.C = C
	pool.syncPool.New = func() interface{} {
		return New()
	}
	return &pool
}

func (pool *PoolEx) Get() IPoolData {
	select {
	case d := <-pool.C:
		if d.IsRef() {
			panic("Pool data is in use.")
		}

		d.Reset()
		d.Ref()
		return d
	default:
		data := pool.syncPool.Get().(IPoolData)
		if data.IsRef() {
			panic("Pool data is in use.")
		}
		data.Reset()
		data.Ref()
		return data
	}

	return nil
}

func (pool *PoolEx) Put(data IPoolData) {
	if data.IsRef() == false {
		//panic("Repeatedly freeing memory")
		log.SysLogger.Panic("Repeatedly freeing memory")
	}
	//提前解引用,防止递归释放
	data.UnRef()
	data.Reset()
	//再次解引用，防止Rest时错误标记
	data.UnRef()
	select {
	case pool.C <- data:
	default:
		pool.syncPool.Put(data)
	}
}
