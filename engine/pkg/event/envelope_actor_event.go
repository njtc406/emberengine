// Package event
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2026/1/5 00:17
// 最后更新:  yr  2026/1/5 00:17
package event

import (
	"sync"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
)

type ActorEventEnvelope struct {
	InternalEnvelope[*actor.Event]
}

var actorEventEnvelopePool pool.IPool[*ActorEventEnvelope]
var actorEventEnvelopePoolOnce sync.Once

func getActorEventEnvelopePool() pool.IPool[*ActorEventEnvelope] {
	actorEventEnvelopePoolOnce.Do(func() {
		actorEventEnvelopePool = pool.NewSyncPoolWrapper(
			func() *ActorEventEnvelope { return &ActorEventEnvelope{} },
			func() pool.IStatsRecorder {
				if config.IsDebug() {
					return pool.NewStatsRecorder("actorEventEnvelopePool")
				}
				return pool.NewNoStatsRecorder()
			}(),
			pool.WithRef(func(e *ActorEventEnvelope) { e.Ref() }),
			pool.WithUnRef(func(e *ActorEventEnvelope) bool { return e.UnRef() }),
			pool.WithReset(func(e *ActorEventEnvelope) { e.Reset() }),
		)
	})
	return actorEventEnvelopePool
}

// NewActorEventEnvelope 创建并发回调事件包装
//
// 参数：
//   - callback: 回调实例
func NewActorEventEnvelope(event *actor.Event) *ActorEventEnvelope {
	e := getActorEventEnvelopePool().Get()
	// 直接使用业务事件类型，让事件进入 Processor 的常规分发链路，
	// 从而避免 core.handleGlobalEvent 再次封装/转发。
	e.Type = event.GetType()
	e.Priority = def.Priority(event.GetPriority())
	e.DispatcherKey = event.GetDispatcherKey()
	e.Payload = event
	return e
}

func (e *ActorEventEnvelope) Release() {
	actorEventEnvelopePool.Put(e)
}
