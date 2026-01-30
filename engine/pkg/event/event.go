package event

import (
	"sync"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
)

type Event struct {
	dto.DataRef

	Type          def.EventType
	Priority      def.Priority
	DispatcherKey string
	Data          interface{}
}

func (e *Event) Reset() {
	e.Type = UnknownEvent
	e.Priority = def.PriorityNormal
	e.DispatcherKey = ""
	e.Data = nil
}

func (e *Event) GetEventType() def.EventType {
	if e.IsRef() {
		return e.Type
	}
	return UnknownEvent
}

func (e *Event) GetData() any {
	return e.Data
}

func (e *Event) GetPriority() def.Priority {
	return e.Priority
}

func (e *Event) GetDispatcherKey() string {
	return e.DispatcherKey
}

func (e *Event) Release() {
	// 仅回收对象池创建的 Event；外部自行 new 的对象不做回收。
	// Put 会触发 WithUnRef，将 IsRef 置回 false，因此重复 Release 会自然变成 no-op。
	if !e.IsRef() {
		return
	}
	getEventPool().Put(e)
}

// Clone 创建一个“用于投递”的浅拷贝：
// - 复制 Type/Priority/DispatcherKey/Data。
//
// 设计意图：默认不在多个 service 间共享同一个 *Event，广播时每个订阅者拿到独立 Event，
// 从而避免引用计数/共享可变数据带来的复杂度与误用。
func (e *Event) Clone() *Event {
	if e == nil {
		return nil
	}
	ne := NewEvent()
	ne.Type = e.Type
	ne.Priority = e.Priority
	ne.DispatcherKey = e.DispatcherKey
	ne.Data = e.Data
	return ne
}

var eventPool pool.IPool[*Event]
var eventPoolOnce sync.Once

func getEventPool() pool.IPool[*Event] {
	eventPoolOnce.Do(func() {
		eventPool = pool.NewSyncPoolWrapper(
			func() *Event {
				return &Event{}
			},
			func() pool.IStatsRecorder {
				if config.IsDebug() {
					return pool.NewStatsRecorder("eventPool")
				} else {
					return pool.NewNoStatsRecorder()
				}
			}(),
			pool.WithReset(func(t *Event) {
				t.Reset()
			}),
			pool.WithRef(func(t *Event) {
				t.Ref()
			}),
			pool.WithUnRef(func(t *Event) bool {
				return t.UnRef()
			}),
		)
	})

	return eventPool
}

func NewEvent() *Event {
	evt := getEventPool().Get()
	return evt
}

// MasterStateData 用于 ServiceBecomeMaster/ServiceLoseMaster/ServiceBecomeSlaver/ServiceDisconnected 事件。
// 存储在 Event.Data 中，替代原来通过 header 传递 epoch 信息的方式。
type MasterStateData struct {
	OldStateIsMaster bool  // 旧状态是否是 master
	PrevEpoch        int64 // 前一个 epoch（fencing token）
	NewEpoch         int64 // 新 epoch（仅 BecomeMaster 有效）
}
