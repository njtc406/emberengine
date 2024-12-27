package event

import (
	"github.com/njtc406/emberengine/engine/dto"
	"github.com/njtc406/emberengine/engine/inf"
	"github.com/njtc406/emberengine/engine/utils/pool"
)

type Event struct {
	dto.DataRef
	Type inf.EventType
	Data interface{}
}

var emptyEvent Event

func (e *Event) Reset() {
	*e = emptyEvent
}

func (e *Event) GetType() inf.EventType {
	return e.Type
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
