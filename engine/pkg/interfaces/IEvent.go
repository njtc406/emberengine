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

// EventCallBack 事件接受器
type EventCallBack func(event IEvent)
type EventOption func(eventType int32, processor IEventProcessor) int

type IEvent interface {
	GetType() int32
	GetKey() string
	GetPriority() int32
	Release()
}

type IEventChannel interface {
	PushEvent(ev IEvent) error // 使用接口时,请注意数据引用问题!!
}

type IListener interface {
	IEventChannel
	IServer
}

type IEventProcessor interface {
	IEventChannel

	Init(eventChannel IListener)
	EventHandler(ev IEvent)
	// 普通事件
	RegEventReceiverFunc(eventType int32, receiver IEventHandler, callback EventCallBack)
	UnRegEventReceiverFun(eventType int32, receiver IEventHandler)
	// 全局事件
	RegGlobalEventReceiverFunc(eventType int32, receiver IEventHandler, callback EventCallBack)
	UnRegGlobalEventReceiverFun(eventType int32, receiver IEventHandler)
	// 发布全局事件
	PublishGlobal(ctx context.Context, eventType int32, data proto.Message) error

	// 服务器事件
	RegServerEventReceiverFunc(eventType int32, receiver IEventHandler, callback EventCallBack)
	UnRegServerEventReceiverFun(eventType int32, receiver IEventHandler)
	// 发布服务器事件
	PublishServer(ctx context.Context, eventType int32, data proto.Message) error

	// 主节点事件
	RegMasterEventReceiverFunc(receiver IEventHandler, callback EventCallBack)
	UnRegMasterEventReceiverFun(receiver IEventHandler)
	// 发布到从节点
	PublishToSlaves(ctx context.Context, data proto.Message) error

	// 从节点事件
	RegSlaverEventReceiverFunc(receiver IEventHandler, callback EventCallBack)
	UnRegSlaverEventReceiverFun(receiver IEventHandler)
	// 发布到主节点
	PublishToMaster(ctx context.Context, data proto.Message) error

	CastEvent(event IEvent) //广播事件
	AddBindEvent(eventType int32, receiver IEventHandler, callback EventCallBack)
	AddListen(eventType int32, receiver IEventHandler)
	RemoveBindEvent(eventType int32, receiver IEventHandler)
	RemoveListen(eventType int32, receiver IEventHandler)
}

type IEventHandler interface {
	Init(p IEventProcessor)
	GetEventProcessor() IEventProcessor
	NotifyEvent(IEvent)
	Destroy()
	//注册了事件
	AddRegInfo(eventType int32, eventProcessor IEventProcessor)
	RemoveRegInfo(eventType int32, eventProcessor IEventProcessor)
}
