package event

import (
	"github.com/njtc406/emberengine/engine/dto"
	"github.com/njtc406/emberengine/engine/utils/pool"
)

type Event struct {
	dto.DataRef
	Type int32
	Key  string
	Data interface{}
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

var eventPool = pool.NewPoolEx(make(chan pool.IPoolData, 10240), func() pool.IPoolData {
	return &Event{}
})

func NewEvent() *Event {
	return eventPool.Get().(*Event)
}

func ReleaseEvent(e *Event) {
	eventPool.Put(e)
}
