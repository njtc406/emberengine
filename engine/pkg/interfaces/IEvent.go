// Package interfaces
// @Title  title
// @Description  desc
// @Author  pc  2024/11/5
// @Update  pc  2024/11/5
package interfaces

import (
	"context"

	"google.golang.org/protobuf/proto"
)

type EventType int32

// EventCallBack 事件接受器
type EventCallBack func(ctx context.Context, event IEvent)
type EventOption func(eventType int32, processor IEventProcessor) int

type IEvent interface {
	IDataDef
	IMailboxJob
	GetData() any
	GetEventType() EventType
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
	RegEventReceiverFunc(eventType EventType, receiver IEventHandler, callback EventCallBack)
	UnRegEventReceiverFun(eventType EventType, receiver IEventHandler)
	// 全局事件
	RegGlobalEventReceiverFunc(eventType EventType, receiver IEventHandler, callback EventCallBack)
	UnRegGlobalEventReceiverFun(eventType EventType, receiver IEventHandler)
	// 发布全局事件
	PublishGlobal(ctx context.Context, eventType EventType, data proto.Message) error

	// 服务器事件
	RegServerEventReceiverFunc(eventType EventType, receiver IEventHandler, callback EventCallBack)
	UnRegServerEventReceiverFun(eventType EventType, receiver IEventHandler)
	// 发布服务器事件
	PublishServer(ctx context.Context, eventType EventType, data proto.Message) error

	// 特定服务事件
	RegSpecificEventReceiverFunc(eventType EventType, serviceUid string, receiver IEventHandler, callback EventCallBack)
	UnRegSpecificEventReceiverFun(eventType EventType, serviceUid string, receiver IEventHandler)
	// 发布特定服务事件
	PublishSpecific(ctx context.Context, eventType EventType, serviceUid string, data proto.Message) error

	CastEvent(ctx context.Context, event IEvent) //广播事件
	AddBindEvent(eventType EventType, receiver IEventHandler, callback EventCallBack)
	AddListen(eventType EventType, receiver IEventHandler)
	RemoveBindEvent(eventType EventType, receiver IEventHandler)
	RemoveListen(eventType EventType, receiver IEventHandler)
}

type IEventHandler interface {
	Init(p IEventProcessor)
	GetEventProcessor() IEventProcessor
	NotifyEvent(ctx context.Context, ev IEvent)
	Destroy()
	//注册了事件
	AddRegInfo(eventType EventType, eventProcessor IEventProcessor)
	RemoveRegInfo(eventType EventType, eventProcessor IEventProcessor)
}
