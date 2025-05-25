// Package event
// @Title  事件管理器
// @Description  这里管理着所有已经注册的事件,一般是一个service一个processor,事件触发时分发到不同的handler，并执行回调
// @Author  yr  2024/7/19 下午3:33
// @Update  yr  2024/7/19 下午3:33
package event

import (
	"context"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"google.golang.org/protobuf/proto"
	"sync"
)

type Processor struct {
	inf.IListener

	locker              sync.RWMutex
	mapListenerEvent    map[int32]map[inf.IEventProcessor]int             //监听者信息
	mapBindHandlerEvent map[int32]map[inf.IEventHandler]inf.EventCallBack //收到事件处理
}

func NewProcessor() inf.IEventProcessor {
	p := &Processor{
		mapListenerEvent:    make(map[int32]map[inf.IEventProcessor]int),
		mapBindHandlerEvent: make(map[int32]map[inf.IEventHandler]inf.EventCallBack),
	}
	return p
}

func (p *Processor) Init(listener inf.IListener) {
	p.IListener = listener
}

// EventHandler 事件处理
func (p *Processor) EventHandler(ev inf.IEvent) {
	eventType := ev.GetType()
	mapCallBack, ok := p.mapBindHandlerEvent[eventType]
	if !ok {
		return
	}
	for _, callback := range mapCallBack {
		callback(ev)
	}
}

// RegEventReceiverFunc 注册事件处理函数
func (p *Processor) RegEventReceiverFunc(eventType int32, receiver inf.IEventHandler, callback inf.EventCallBack) {
	//记录receiver自己注册过的事件
	receiver.AddRegInfo(eventType, p)
	//记录当前所属IEventProcessor注册的回调
	receiver.GetEventProcessor().AddBindEvent(eventType, receiver, callback)
	//将注册加入到监听中
	p.AddListen(eventType, receiver)
}

// UnRegEventReceiverFun 取消注册
func (p *Processor) UnRegEventReceiverFun(eventType int32, receiver inf.IEventHandler) {
	p.RemoveListen(eventType, receiver)
	receiver.GetEventProcessor().RemoveBindEvent(eventType, receiver)
	receiver.RemoveRegInfo(eventType, p)
}

// 全局事件
func (p *Processor) RegGlobalEventReceiverFunc(eventType int32, receiver inf.IEventHandler, callback inf.EventCallBack) {
	p.RegEventReceiverFunc(eventType, receiver, callback)
	GetEventBus().SubscribeGlobal(eventType, p)
}

func (p *Processor) UnRegGlobalEventReceiverFun(eventType int32, receiver inf.IEventHandler) {
	p.UnRegEventReceiverFun(eventType, receiver)
	GetEventBus().UnSubscribeGlobal(eventType, p)
}

// 服务器事件
func (p *Processor) RegServerEventReceiverFunc(eventType int32, receiver inf.IEventHandler, callback inf.EventCallBack) {
	p.RegEventReceiverFunc(eventType, receiver, callback)
	GetEventBus().SubscribeServer(eventType, p)
}

func (p *Processor) UnRegServerEventReceiverFun(eventType int32, receiver inf.IEventHandler) {
	p.UnRegEventReceiverFun(eventType, receiver)
	GetEventBus().UnSubscribeServer(eventType, p)
}

// 发布全局事件
func (p *Processor) PublishGlobal(ctx context.Context, eventType int32, data proto.Message) error {
	return GetEventBus().PublishGlobal(ctx, eventType, data)
}

// 发布服务器事件
func (p *Processor) PublishServer(ctx context.Context, eventType int32, data proto.Message) error {
	return GetEventBus().PublishServer(ctx, eventType, p.GetServerId(), data)
}

func (p *Processor) RegMasterEventReceiverFunc(receiver inf.IEventHandler, callback inf.EventCallBack) {
	p.RegEventReceiverFunc(ServiceMasterEventTrigger, receiver, callback)
	GetEventBus().SubscribeMaster(p)
}

func (p *Processor) UnRegMasterEventReceiverFun(receiver inf.IEventHandler) {
	p.UnRegEventReceiverFun(ServiceMasterEventTrigger, receiver)
	GetEventBus().UnSubscribeMaster(p)
}

func (p *Processor) PublishToSlaves(ctx context.Context, data proto.Message) error {
	return GetEventBus().PublishSlaver(ctx, p, data)
}

func (p *Processor) RegSlaverEventReceiverFunc(receiver inf.IEventHandler, callback inf.EventCallBack) {
	p.RegEventReceiverFunc(ServiceSlaverEventTrigger, receiver, callback)
	GetEventBus().SubscribeSlaver(p)
}
func (p *Processor) UnRegSlaverEventReceiverFun(receiver inf.IEventHandler) {
	p.UnRegEventReceiverFun(ServiceSlaverEventTrigger, receiver)
	GetEventBus().UnSubscribeSlaver(p)
}

func (p *Processor) PublishToMaster(ctx context.Context, data proto.Message) error {
	return GetEventBus().PublishMaster(ctx, p, data)
}

// castEvent 广播事件
func (p *Processor) CastEvent(event inf.IEvent) {
	if p.mapListenerEvent == nil {
		//log.Error("mapListenerEvent not init!")
		return
	}

	eventProcessor, ok := p.mapListenerEvent[event.GetType()]
	if ok == false || p == nil {
		return
	}

	for proc := range eventProcessor {
		proc.PushEvent(event)
	}
}

// addListen 添加监听
func (p *Processor) AddListen(eventType int32, receiver inf.IEventHandler) {
	p.locker.Lock()
	defer p.locker.Unlock()

	if _, ok := p.mapListenerEvent[eventType]; ok == false {
		p.mapListenerEvent[eventType] = map[inf.IEventProcessor]int{}
	}

	p.mapListenerEvent[eventType][receiver.GetEventProcessor()] += 1
}

// addBindEvent 添加绑定事件
func (p *Processor) AddBindEvent(eventType int32, receiver inf.IEventHandler, callback inf.EventCallBack) {
	p.locker.Lock()
	defer p.locker.Unlock()

	if _, ok := p.mapBindHandlerEvent[eventType]; ok == false {
		p.mapBindHandlerEvent[eventType] = map[inf.IEventHandler]inf.EventCallBack{}
	}

	p.mapBindHandlerEvent[eventType][receiver] = callback
}

// removeBindEvent 移除绑定事件
func (p *Processor) RemoveBindEvent(eventType int32, receiver inf.IEventHandler) {
	p.locker.Lock()
	defer p.locker.Unlock()
	if _, ok := p.mapBindHandlerEvent[eventType]; ok == true {
		delete(p.mapBindHandlerEvent[eventType], receiver)
	}
}

// removeListen 移除监听
func (p *Processor) RemoveListen(eventType int32, receiver inf.IEventHandler) {
	p.locker.Lock()
	defer p.locker.Unlock()
	if _, ok := p.mapListenerEvent[eventType]; ok == true {
		p.mapListenerEvent[eventType][receiver.GetEventProcessor()] -= 1
		if p.mapListenerEvent[eventType][receiver.GetEventProcessor()] <= 0 {
			delete(p.mapListenerEvent[eventType], receiver.GetEventProcessor())
		}
	}
}
