package event

import (
	"github.com/njtc406/emberengine/engine/pkg/dto"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
	"github.com/njtc406/emberengine/engine/pkg/xcontext"
	"sync/atomic"
)

type Event struct {
	dto.DataRef
	xcontext.XContext

	Type int32
	Data interface{}

	refCount atomic.Int32
}

var emptyEvent Event

func (e *Event) Reset() {
	*e = emptyEvent
}

func (e *Event) GetType() int32 {
	if e.IsRef() {
		return e.Type
	}
	return UnknownEvent
}

func (e *Event) IncRef() {
	e.refCount.Add(1)
}

func (e *Event) Release() {
	if e.refCount.Add(-1) == 0 {
		eventPool.Put(e)
	}
}

var eventPool = pool.NewPrePPoolEx(4096, func() pool.IPoolData {
	return &Event{}
})

// TODO 这个不能使用pool,因为使用者可能会循环的发给不同订阅者,一个订阅者处理完之后就会释放,可能会导致并发问题,所以直接new
func NewEvent() *Event {
	evt := eventPool.Get().(*Event)
	evt.XContext = xcontext.New(nil)
	return evt
}
