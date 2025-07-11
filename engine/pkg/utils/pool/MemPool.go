package pool

import (
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"sync"
)

type Pool struct {
	C        chan interface{} //最大缓存的数量
	syncPool sync.Pool
}

type IPoolData interface {
	inf.IReset
	inf.IDataDef
}

type PoolEx struct {
	C        chan IPoolData //最大缓存的数量
	syncPool sync.Pool
}

func (pool *Pool) Get() interface{} {
	select {
	case d := <-pool.C:
		return d
	default:
		return pool.syncPool.Get()
	}

	return nil
}

func (pool *Pool) Put(data interface{}) {
	select {
	case pool.C <- data:
	default:
		pool.syncPool.Put(data)
	}

}

func NewPool(C chan interface{}, New func() interface{}) *Pool {
	var p Pool
	p.C = C
	p.syncPool.New = New
	return &p
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
