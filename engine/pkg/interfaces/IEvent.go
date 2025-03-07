// Package interfaces
// @Title  title
// @Description  desc
// @Author  pc  2024/11/5
// @Update  pc  2024/11/5
package interfaces

// EventCallBack 事件接受器
type EventCallBack func(event IEvent)

type IEvent interface {
	GetType() int32
	GetKey() string
	GetPriority() int32
	Release()
}

type IEventChannel interface {
	PushEvent(ev IEvent) error
}

type IEventProcessor interface {
	IEventChannel

	Init(eventChannel IEventChannel)
	EventHandler(ev IEvent)
	RegEventReceiverFunc(eventType int32, receiver IEventHandler, callback EventCallBack)
	UnRegEventReceiverFun(eventType int32, receiver IEventHandler)

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
