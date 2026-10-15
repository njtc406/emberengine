// Package event
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2026/2/1 15:23
// 最后更新:  yr  2026/2/1 15:23
package event

import (
	"context"

	"github.com/njtc406/emberengine/engine/pkg/def"
	"go.etcd.io/etcd/api/v3/mvccpb"
)

type Event[T any] struct {
	Context   context.Context
	EventType def.EventType
	Data      T
}

func (e *Event[T]) GetContext() context.Context {
	return e.Context
}
func (e *Event[T]) GetEventType() def.EventType {
	return e.EventType
}

func (e *Event[T]) GetData() any {
	return e.Data
}

type DiscoveryEvent struct {
	Event[*mvccpb.KeyValue]
}

func NewDiscoveryEvent() *DiscoveryEvent {
	return &DiscoveryEvent{}
}
