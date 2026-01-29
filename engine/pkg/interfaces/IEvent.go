// Package interfaces
// @Title  title
// @Description  desc
// @Author  pc  2024/11/5
// @Update  pc  2024/11/5
package interfaces

import (
	"context"

	"github.com/njtc406/emberengine/engine/pkg/def"
	"google.golang.org/protobuf/proto"
)

// EventCallBack 事件接受器
type EventCallBack func(ctx context.Context, event IEvent)
type EventOption func(eventType int32, processor IEventProcessor) int

type IEvent interface {
	GetData() any
	GetEventType() def.EventType
}

type IEventChannel interface {
	PushEvent(ctx context.Context, ev IEvent) error // 使用接口时,请注意数据引用问题!!
}

type IListener interface {
	IMailboxChannel
	IServer
}

type IEventListener interface {
	IEventChannel
	IMailboxChannel
	IServer
}

type IEventProcessor interface {
	IEventChannel

	Init(eventChannel IEventListener)
	EventHandler(ctx context.Context, ev IEvent)
	// 普通事件
	RegEventReceiverFunc(eventType def.EventType, receiver IEventHandler, callback EventCallBack)
	UnRegEventReceiverFun(eventType def.EventType, receiver IEventHandler)
	// 全局事件
	RegGlobalEventReceiverFunc(eventType def.EventType, receiver IEventHandler, callback EventCallBack)
	UnRegGlobalEventReceiverFun(eventType def.EventType, receiver IEventHandler)
	// 发布全局事件
	PublishGlobal(ctx context.Context, eventType def.EventType, data proto.Message) error

	// 服务器事件
	RegServerEventReceiverFunc(eventType def.EventType, receiver IEventHandler, callback EventCallBack)
	UnRegServerEventReceiverFun(eventType def.EventType, receiver IEventHandler)
	// 发布服务器事件
	PublishServer(ctx context.Context, eventType def.EventType, data proto.Message) error

	// 特定服务事件
	RegSpecificEventReceiverFunc(eventType def.EventType, serviceUid string, receiver IEventHandler, callback EventCallBack)
	UnRegSpecificEventReceiverFun(eventType def.EventType, serviceUid string, receiver IEventHandler)
	// 发布特定服务事件
	PublishSpecific(ctx context.Context, eventType def.EventType, serviceUid string, data proto.Message) error

	CastEvent(ctx context.Context, event IEvent) //广播事件
	AddBindEvent(eventType def.EventType, receiver IEventHandler, callback EventCallBack)
	AddListen(eventType def.EventType, receiver IEventHandler)
	RemoveBindEvent(eventType def.EventType, receiver IEventHandler)
	RemoveListen(eventType def.EventType, receiver IEventHandler)
}

type IEventHandler interface {
	Init(p IEventProcessor)
	GetEventProcessor() IEventProcessor
	NotifyEvent(ctx context.Context, ev IEvent)
	Destroy()
	//注册了事件
	AddRegInfo(eventType def.EventType, eventProcessor IEventProcessor)
	RemoveRegInfo(eventType def.EventType, eventProcessor IEventProcessor)
}
