// Package inf
// @Title  title
// @Description  desc
// @Author  pc  2024/11/5
// @Update  pc  2024/11/5
package inf

// EventCallBack 事件接受器
type EventCallBack func(event IEvent)

type EventType int32

type IEvent interface {
	GetType() EventType
}

type IEventChannel interface {
	PushEvent(ev IEvent) error
}

type IEventProcessor interface {
	IEventChannel

	Init(eventChannel IEventChannel)
	EventHandler(ev IEvent)
	RegEventReceiverFunc(eventType EventType, receiver IEventHandler, callback EventCallBack)
	UnRegEventReceiverFun(eventType EventType, receiver IEventHandler)

	CastEvent(event IEvent) //广播事件
	AddBindEvent(eventType EventType, receiver IEventHandler, callback EventCallBack)
	AddListen(eventType EventType, receiver IEventHandler)
	RemoveBindEvent(eventType EventType, receiver IEventHandler)
	RemoveListen(eventType EventType, receiver IEventHandler)
}

type IEventHandler interface {
	Init(p IEventProcessor)
	GetEventProcessor() IEventProcessor
	NotifyEvent(IEvent)
	Destroy()
	//注册了事件
	AddRegInfo(eventType EventType, eventProcessor IEventProcessor)
	RemoveRegInfo(eventType EventType, eventProcessor IEventProcessor)
}
