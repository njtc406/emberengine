// Package interfaces
// @Title  内部同步事件触发器接口
// @Description  用于服务内部的同步事件触发，不涉及异步处理
// @Author  yr  2026/1/31
// @Update  yr  2026/1/31
package interfaces

import (
	"context"
	"reflect"

	"github.com/njtc406/emberengine/engine/pkg/def"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// TriggerCallback 泛型事件回调，data为具体类型
type TriggerCallback[T any] func(ctx context.Context, eventType def.EventType, data T)

// TriggerCallbackAny 内部使用的非泛型回调（用于存储）
type TriggerCallbackAny func(ctx context.Context, eventType def.EventType, data any)

// ITrigger 同步事件触发器接口（服务级别，类似Processor）
// 用于服务内部的同步事件分发，所有handler会在同一个goroutine中依次执行
type ITrigger interface {
	// Trigger 触发事件，同步依次调用所有对应类型的handler
	Trigger(ctx context.Context, eventType def.EventType, data any)

	// HasHandler 检查是否有某个事件类型的处理器
	HasHandler(eventType def.EventType) bool

	// Clear 清除所有注册的处理器
	Clear()

	// BindHandler 绑定handler（由ITriggerHandler调用）
	BindHandler(eventType def.EventType, name string, handler ITriggerHandler, callback TriggerCallbackAny)
	// UnbindHandler 解绑handler
	UnbindHandler(eventType def.EventType, name string, handler ITriggerHandler)

	// ========== 集群事件（通过EventBus） ==========

	// BindGlobalHandler 绑定全局事件handler（会注册到EventBus）
	BindGlobalHandler(eventType def.EventType, name string, handler ITriggerHandler, callback TriggerCallbackAny)
	// UnbindGlobalHandler 解绑全局事件handler
	UnbindGlobalHandler(eventType def.EventType, name string, handler ITriggerHandler)

	// BindServerHandler 绑定服务器事件handler（会注册到EventBus）
	BindServerHandler(eventType def.EventType, name string, handler ITriggerHandler, callback TriggerCallbackAny)
	// UnbindServerHandler 解绑服务器事件handler
	UnbindServerHandler(eventType def.EventType, name string, handler ITriggerHandler)

	// BindSpecificHandler 绑定特定服务事件handler（会注册到EventBus）
	BindSpecificHandler(eventType def.EventType, serviceUid string, name string, handler ITriggerHandler, callback TriggerCallbackAny)
	// UnbindSpecificHandler 解绑特定服务事件handler
	UnbindSpecificHandler(eventType def.EventType, serviceUid string, name string, handler ITriggerHandler)

	// PublishGlobal 发布全局事件
	PublishGlobal(ctx context.Context, eventType def.EventType, data proto.Message) error
	// PublishServer 发布服务器事件
	PublishServer(ctx context.Context, eventType def.EventType, data proto.Message) error
	// PublishSpecific 发布特定服务事件
	PublishSpecific(ctx context.Context, eventType def.EventType, serviceUid string, data proto.Message) error
}

// ITriggerHandler 事件处理器接口（module级别，类似Handler）
// 每个module持有一个，用于管理自己注册的事件
type ITriggerHandler interface {
	// GetTrigger 获取关联的Trigger
	GetTrigger() ITrigger

	// RegisterAny 注册事件处理器（非泛型版本，内部使用）
	RegisterAny(eventType def.EventType, name string, callback TriggerCallbackAny)

	// Unregister 取消注册指定名称的处理器
	Unregister(eventType def.EventType, name string)

	// UnregisterAll 取消注册该handler下所有事件
	UnregisterAll()

	// Trigger 触发事件（快捷方法，调用关联Trigger的Trigger）
	Trigger(ctx context.Context, eventType def.EventType, data any)

	// GetRegisteredEvents 获取已注册的事件列表（用于调试）
	GetRegisteredEvents() map[def.EventType][]string

	// Destroy 销毁，取消所有注册
	Destroy()

	// ========== 集群事件注册（非泛型版本） ==========

	// RegisterGlobalAny 注册全局事件处理器
	RegisterGlobalAny(eventType def.EventType, name string, callback TriggerCallbackAny)
	// UnregisterGlobal 取消注册全局事件处理器
	UnregisterGlobal(eventType def.EventType, name string)

	// RegisterServerAny 注册服务器事件处理器
	RegisterServerAny(eventType def.EventType, name string, callback TriggerCallbackAny)
	// UnregisterServer 取消注册服务器事件处理器
	UnregisterServer(eventType def.EventType, name string)

	// RegisterSpecificAny 注册特定服务事件处理器
	RegisterSpecificAny(eventType def.EventType, serviceUid string, name string, callback TriggerCallbackAny)
	// UnregisterSpecific 取消注册特定服务事件处理器
	UnregisterSpecific(eventType def.EventType, serviceUid string, name string)
}

// Register 泛型注册函数，回调中data为具体类型T（本地事件）
func Register[T any](handler ITriggerHandler, eventType def.EventType, name string, callback TriggerCallback[T]) {
	wrapper := func(ctx context.Context, et def.EventType, data any) {
		if typed, ok := data.(T); ok {
			callback(ctx, et, typed)
		}
	}
	handler.RegisterAny(eventType, name, wrapper)
}

// RegisterGlobal 泛型注册全局事件，回调中data为具体类型T
func RegisterGlobal[T any](handler ITriggerHandler, eventType def.EventType, name string, callback TriggerCallback[T]) {
	wrapper := func(ctx context.Context, et def.EventType, data any) {
		if typed, ok := data.(T); ok {
			callback(ctx, et, typed)
		}
	}
	handler.RegisterGlobalAny(eventType, name, wrapper)
}

// RegisterServer 泛型注册服务器事件，回调中data为具体类型T
func RegisterServer[T any](handler ITriggerHandler, eventType def.EventType, name string, callback TriggerCallback[T]) {
	wrapper := func(ctx context.Context, et def.EventType, data any) {
		if typed, ok := data.(T); ok {
			callback(ctx, et, typed)
		}
	}
	handler.RegisterServerAny(eventType, name, wrapper)
}

// RegisterSpecific 泛型注册特定服务事件，回调中data为具体类型T
func RegisterSpecific[T any](handler ITriggerHandler, eventType def.EventType, serviceUid string, name string, callback TriggerCallback[T]) {
	wrapper := func(ctx context.Context, et def.EventType, data any) {
		if typed, ok := data.(T); ok {
			callback(ctx, et, typed)
		}
	}
	handler.RegisterSpecificAny(eventType, serviceUid, name, wrapper)
}

type payloadGetter interface {
	GetPayload() *anypb.Any
}

func newProtoMessage[T proto.Message]() (T, bool) {
	var zero T
	t := reflect.TypeOf(zero)
	if t == nil {
		return zero, false
	}
	if t.Kind() != reflect.Ptr {
		return zero, false
	}
	v := reflect.New(t.Elem())
	msg, ok := v.Interface().(T)
	return msg, ok
}

// RegisterGlobalPB 注册全局事件处理器（集群事件），从 *actor.Event.Payload 自动解码到具体 proto 类型 T。
func RegisterGlobalPB[T proto.Message](handler ITriggerHandler, eventType def.EventType, name string, callback TriggerCallback[T]) {
	wrapper := func(ctx context.Context, et def.EventType, data any) {
		pg, ok := data.(payloadGetter)
		if !ok {
			return
		}
		payload := pg.GetPayload()
		if payload == nil {
			return
		}
		msg, ok := newProtoMessage[T]()
		if !ok {
			return
		}
		if err := payload.UnmarshalTo(msg); err != nil {
			return
		}
		callback(ctx, et, msg)
	}
	handler.RegisterGlobalAny(eventType, name, wrapper)
}

// RegisterServerPB 注册服务器事件处理器（集群事件），从 *actor.Event.Payload 自动解码到具体 proto 类型 T。
func RegisterServerPB[T proto.Message](handler ITriggerHandler, eventType def.EventType, name string, callback TriggerCallback[T]) {
	wrapper := func(ctx context.Context, et def.EventType, data any) {
		pg, ok := data.(payloadGetter)
		if !ok {
			return
		}
		payload := pg.GetPayload()
		if payload == nil {
			return
		}
		msg, ok := newProtoMessage[T]()
		if !ok {
			return
		}
		if err := payload.UnmarshalTo(msg); err != nil {
			return
		}
		callback(ctx, et, msg)
	}
	handler.RegisterServerAny(eventType, name, wrapper)
}

// RegisterSpecificPB 注册特定服务事件处理器（集群事件），从 *actor.Event.Payload 自动解码到具体 proto 类型 T。
func RegisterSpecificPB[T proto.Message](handler ITriggerHandler, eventType def.EventType, serviceUid string, name string, callback TriggerCallback[T]) {
	wrapper := func(ctx context.Context, et def.EventType, data any) {
		pg, ok := data.(payloadGetter)
		if !ok {
			return
		}
		payload := pg.GetPayload()
		if payload == nil {
			return
		}
		msg, ok := newProtoMessage[T]()
		if !ok {
			return
		}
		if err := payload.UnmarshalTo(msg); err != nil {
			return
		}
		callback(ctx, et, msg)
	}
	handler.RegisterSpecificAny(eventType, serviceUid, name, wrapper)
}
