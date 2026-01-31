// Package interfaces
// @Title  内部同步事件触发器接口
// @Description  用于服务内部的同步事件触发，不涉及异步处理
// @Author  yr  2026/1/31
// @Update  yr  2026/1/31
package interfaces

import (
	"context"

	"github.com/njtc406/emberengine/engine/pkg/def"
	"google.golang.org/protobuf/proto"
)

// EventHandler 泛型事件回调，data为具体类型
type EventHandler[T any] func(ctx context.Context, data T) error

// EventHandlerAny 内部使用的非泛型回调（用于存储）
type EventHandlerAny func(ctx context.Context, data any) error

// IEventProcessor 同步事件触发器接口（服务级别，类似Processor）
// 用于服务内部的同步事件分发，所有handler会在同一个goroutine中依次执行
type IEventProcessor interface {
	// Trigger 触发事件，同步依次调用所有对应类型的handler
	Trigger(ctx context.Context, eventType def.EventType, data any)

	// HasHandler 检查是否有某个事件类型的处理器
	HasHandler(eventType def.EventType) bool

	// Clear 清除所有注册的处理器
	Clear()

	// BindHandler 绑定本地事件处理器
	BindHandler(eventType def.EventType, name string, handler IEventHandler, callback EventHandlerAny)
	// BindGlobalHandler 绑定集群事件处理器
	BindGlobalHandler(eventType def.EventType, name string, handler IEventHandler, callback EventHandlerAny)
	// BindServerHandler 绑定服务器事件处理器
	BindServerHandler(eventType def.EventType, name string, handler IEventHandler, callback EventHandlerAny)
	// BindSpecificHandler 绑定特定服务事件处理器
	BindSpecificHandler(eventType def.EventType, serviceUid string, name string, handler IEventHandler, callback EventHandlerAny)
	// UnbindHandler 取消绑定本地事件处理器
	UnbindHandler(eventType def.EventType, name string, handler IEventHandler)
	// UnbindGlobalHandler 取消绑定全局事件处理器
	UnbindGlobalHandler(eventType def.EventType, name string, handler IEventHandler)
	// UnbindServerHandler 取消绑定服务器事件处理器
	UnbindServerHandler(eventType def.EventType, name string, handler IEventHandler)
	// UnbindSpecificHandler 取消绑定特定服务事件处理器
	UnbindSpecificHandler(eventType def.EventType, serviceUid string, name string, handler IEventHandler)

	// ========== 集群事件发布 ==========

	// PublishGlobal 发布全局事件
	PublishGlobal(ctx context.Context, eventType def.EventType, data proto.Message) error
	// PublishServer 发布服务器事件
	PublishServer(ctx context.Context, eventType def.EventType, data proto.Message) error
	// PublishSpecific 发布特定服务事件
	PublishSpecific(ctx context.Context, eventType def.EventType, serviceUid string, data proto.Message) error
}

// IEventHandler 事件处理器接口（module级别，类似Handler）
// 每个module持有一个，用于管理自己注册的事件
// 注册事件请使用泛型函数：Register[T]、RegisterGlobal[T]、RegisterServer[T]、RegisterSpecific[T]
type IEventHandler interface {
	// GetTrigger 获取关联的Trigger
	GetTrigger() IEventProcessor

	// UnregisterAll 取消注册该handler下所有事件
	UnregisterAll()

	// Trigger 触发事件（快捷方法，调用关联Trigger的Trigger）
	Trigger(ctx context.Context, eventType def.EventType, data any)

	// GetRegisteredEvents 获取已注册的事件列表（用于调试）
	GetRegisteredEvents() map[def.EventType][]string

	// Destroy 销毁，取消所有注册
	Destroy()
}

type IEventHandlerRegistrar interface {
	// RegisterEvent 注册本地事件处理器
	RegisterEvent(eventType def.EventType, name string, callback EventHandlerAny) error
	// RegisterGlobalEvent 注册集群事件处理器
	RegisterGlobalEvent(eventType def.EventType, name string, callback EventHandlerAny) error
	// RegisterServerEvent 注册服务器事件处理器
	RegisterServerEvent(eventType def.EventType, name string, callback EventHandlerAny) error
	// RegisterSpecificEvent 注册特定服务事件处理器
	RegisterSpecificEvent(eventType def.EventType, serviceUid string, name string, callback EventHandlerAny) error

	// UnregisterEvent 注销本地事件处理器
	UnregisterEvent(eventType def.EventType, name string)
	// UnregisterGlobalEvent 注销集群事件处理器
	UnregisterGlobalEvent(eventType def.EventType, name string)
	// UnregisterServerEvent 注销服务器事件处理器
	UnregisterServerEvent(eventType def.EventType, name string)
	// UnregisterSpecificEvent 注销特定服务事件处理器
	UnregisterSpecificEvent(eventType def.EventType, serviceUid string, name string)
}
