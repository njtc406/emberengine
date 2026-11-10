// Package event
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2026/2/1 15:23
// 最后更新:  yr  2026/2/1 15:23
package event

import (
	"context"
	"reflect"

	"github.com/njtc406/emberengine/v2/engine/pkg/def"
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

// GetData 返回事件数据；nil 指针、map、slice、func、chan 等空值统一返回 nil 接口。
func (e *Event[T]) GetData() any {
	data := any(e.Data)
	if data == nil {
		return nil
	}
	value := reflect.ValueOf(data)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if value.IsNil() {
			return nil
		}
	}
	return data
}

type DiscoveryEvent struct {
	Event[*mvccpb.KeyValue]
}

func NewDiscoveryEvent() *DiscoveryEvent {
	return &DiscoveryEvent{}
}
