package event

import (
	"github.com/njtc406/emberengine/engine/pkg/dto"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
	"sync/atomic"
)

type Event struct {
	dto.DataRef
	Type     int32
	Key      string
	Priority int32
	Data     interface{}

	IntExt    []int64
	StringExt []string
	AnyExt    []any

	refCount atomic.Int32
}

var emptyEvent Event

func (e *Event) Reset() {
	*e = emptyEvent
}

func (e *Event) GetType() int32 {
	return e.Type
}

func (e *Event) GetKey() string {
	return e.Key
}

func (e *Event) GetPriority() int32 {
	return e.Priority
}

func (e *Event) IncRef() {
	e.refCount.Add(1)
}

func (e *Event) Release() {
	if e.refCount.Add(-1) == 0 {
		eventPool.Put(e)
	}
}

var eventPool = pool.NewPoolEx(make(chan pool.IPoolData, 10240), func() pool.IPoolData {
	return &Event{}
})

// TODO 这个不能使用pool,因为使用者可能会循环的发给不同订阅者,一个订阅者处理完之后就会释放,可能会导致并发问题,所以直接new
func NewEvent() *Event {
	return eventPool.Get().(*Event)
}
