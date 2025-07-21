package event

import (
	"github.com/njtc406/emberengine/engine/pkg/dto"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
	"github.com/njtc406/emberengine/engine/pkg/utils/xcontext"
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
	if e.refCount.Add(-1) <= 0 { // 有的地方可能不需要inc,这里-1就会变成负数
		eventPool.Put(e)
	}
}

var eventPool = pool.NewSyncPoolWrapper(
	func() *Event {
		return &Event{}
	},
	pool.NewStatsRecorder("eventPool"),
	pool.WithRef(func(t *Event) {
		t.Ref()
	}),
	pool.WithUnref(func(t *Event) {
		t.UnRef()
	}),
	pool.WithReset(func(t *Event) {
		t.Reset()
	}),
)

func NewEvent() *Event {
	evt := eventPool.Get() // 事件使用时,会使用incRef来保证所有地方都释放之后才真正释放
	evt.XContext = xcontext.New(nil)
	return evt
}

func GetEventPoolStats() *pool.Stats {
	return eventPool.Stats()
}
