// Package event
// @Title  事件管理器
// @Description  这里管理着所有已经注册的事件,一般是一个service一个processor,事件触发时分发到不同的handler，并执行回调
// @Author  yr  2024/7/19 下午3:33
// @Update  yr  2024/7/19 下午3:33
package event

import (
	"context"
	"sync"

	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"google.golang.org/protobuf/proto"
)

var _ inf.IEventProcessor = (*Processor)(nil)

type Processor struct {
	inf.IListener

	locker              sync.RWMutex
	mapListenerEvent    map[def.EventType]map[inf.IEventProcessor]int             //监听者信息
	mapBindHandlerEvent map[def.EventType]map[inf.IEventHandler]inf.EventCallBack //收到事件处理
}

func NewProcessor() *Processor {
	p := &Processor{
		mapListenerEvent:    make(map[def.EventType]map[inf.IEventProcessor]int),
		mapBindHandlerEvent: make(map[def.EventType]map[inf.IEventHandler]inf.EventCallBack),
	}
	return p
}

func (p *Processor) Init(listener inf.IListener) {
	p.IListener = listener
}

func (p *Processor) safeExec(f func(ctx context.Context, e inf.IEvent), ctx context.Context, e inf.IEvent) {
	defer func() {
		if err := recover(); err != nil {
			//log.Error("event handler panic:", err)
			log.SysLogger.Errorf("event handler panic: %v", err)
		}
	}()
	f(ctx, e)
}

// EventHandler 事件处理
func (p *Processor) EventHandler(ctx context.Context, ev inf.IEvent) {
	eventType := ev.GetEventType()
	mapCallBack, ok := p.mapBindHandlerEvent[eventType]
	if !ok {
		return
	}
	for _, callback := range mapCallBack {
		p.safeExec(callback, ctx, ev)
	}
}

// RegEventReceiverFunc 注册事件处理函数
func (p *Processor) RegEventReceiver(eventType def.EventType, receiver inf.IEventHandler, callback inf.EventCallBack) {
	//记录receiver自己注册过的事件
	receiver.AddRegInfo(eventType, p)
	//记录当前所属IEventProcessor注册的回调
	receiver.GetEventProcessor().AddBindEvent(eventType, receiver, callback)
	//将注册加入到监听中
	p.AddListen(eventType, receiver)
}

// UnRegEventReceiverFun 取消注册
func (p *Processor) UnRegEventReceiver(eventType def.EventType, receiver inf.IEventHandler) {
	p.RemoveListen(eventType, receiver)
	receiver.GetEventProcessor().RemoveBindEvent(eventType, receiver)
	receiver.RemoveRegInfo(eventType, p)
}

// 全局事件
func (p *Processor) RegGlobalEventReceiver(eventType def.EventType, receiver inf.IEventHandler, callback inf.EventCallBack) {
	p.RegEventReceiver(eventType, receiver, callback)
	GetEventBus().SubscribeGlobal(eventType, p)
}

func (p *Processor) UnRegGlobalEventReceiver(eventType def.EventType, receiver inf.IEventHandler) {
	p.UnRegEventReceiver(eventType, receiver)
	GetEventBus().UnSubscribeGlobal(eventType, p)
}

// 服务器事件
func (p *Processor) RegServerEventReceiver(eventType def.EventType, receiver inf.IEventHandler, callback inf.EventCallBack) {
	p.RegEventReceiver(eventType, receiver, callback)
	GetEventBus().SubscribeServer(eventType, p)
}

func (p *Processor) UnRegServerEventReceiver(eventType def.EventType, receiver inf.IEventHandler) {
	p.UnRegEventReceiver(eventType, receiver)
	GetEventBus().UnSubscribeServer(eventType, p)
}

// 特定服务事件
func (p *Processor) RegSpecificEventReceiver(eventType def.EventType, serviceUid string, receiver inf.IEventHandler, callback inf.EventCallBack) {
	p.RegEventReceiver(eventType, receiver, callback)
	GetEventBus().SubscribeSpecific(eventType, serviceUid, p)
}

func (p *Processor) UnRegSpecificEventReceiver(eventType def.EventType, serviceUid string, receiver inf.IEventHandler) {
	p.UnRegEventReceiver(eventType, receiver)
	GetEventBus().UnSubscribeSpecific(eventType, serviceUid, p)
}

// 发布全局事件
func (p *Processor) PublishGlobal(ctx context.Context, eventType def.EventType, data proto.Message) error {
	return GetEventBus().PublishGlobal(ctx, eventType, data)
}

// 发布服务器事件
func (p *Processor) PublishServer(ctx context.Context, eventType def.EventType, data proto.Message) error {
	return GetEventBus().PublishServer(ctx, eventType, p.GetPartition(), data)
}

// 发布特定服务事件
func (p *Processor) PublishSpecific(ctx context.Context, eventType def.EventType, serviceUid string, data proto.Message) error {
	return GetEventBus().PublishSpecific(ctx, eventType, serviceUid, data)
}

// castEvent 广播事件
func (p *Processor) CastEvent(ctx context.Context, event inf.IEvent) {
	//if p.mapListenerEvent == nil {
	//	//log.Error("mapListenerEvent not init!")
	//	return
	//}
	//
	//eventProcessor, ok := p.mapListenerEvent[event.GetEventType()]
	//if ok == false || p == nil {
	//	return
	//}
	//
	//for proc := range eventProcessor {
	//	//proc.PushEvent(ctx, event)
	//}
}

// addListen 添加监听
func (p *Processor) AddListen(eventType def.EventType, receiver inf.IEventHandler) {
	p.locker.Lock()
	defer p.locker.Unlock()

	if _, ok := p.mapListenerEvent[eventType]; ok == false {
		p.mapListenerEvent[eventType] = map[inf.IEventProcessor]int{}
	}

	p.mapListenerEvent[eventType][receiver.GetEventProcessor()] += 1
}

// addBindEvent 添加绑定事件
func (p *Processor) AddBindEvent(eventType def.EventType, receiver inf.IEventHandler, callback inf.EventCallBack) {
	p.locker.Lock()
	defer p.locker.Unlock()

	if _, ok := p.mapBindHandlerEvent[eventType]; ok == false {
		p.mapBindHandlerEvent[eventType] = map[inf.IEventHandler]inf.EventCallBack{}
	}

	p.mapBindHandlerEvent[eventType][receiver] = callback
}

// removeBindEvent 移除绑定事件
func (p *Processor) RemoveBindEvent(eventType def.EventType, receiver inf.IEventHandler) {
	p.locker.Lock()
	defer p.locker.Unlock()
	if _, ok := p.mapBindHandlerEvent[eventType]; ok == true {
		delete(p.mapBindHandlerEvent[eventType], receiver)
	}
}

// removeListen 移除监听
func (p *Processor) RemoveListen(eventType def.EventType, receiver inf.IEventHandler) {
	p.locker.Lock()
	defer p.locker.Unlock()
	if _, ok := p.mapListenerEvent[eventType]; ok == true {
		p.mapListenerEvent[eventType][receiver.GetEventProcessor()] -= 1
		if p.mapListenerEvent[eventType][receiver.GetEventProcessor()] <= 0 {
			delete(p.mapListenerEvent[eventType], receiver.GetEventProcessor())
		}
	}
}
