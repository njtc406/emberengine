package event

import (
	"context"

	"github.com/nats-io/nats.go"
	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/actor/mailbox/job"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"google.golang.org/protobuf/proto"
)

// PublishSpecific 发布指定服务的事件(只有订阅了该服务事件的服务会收到)
func (eb *Bus) PublishSpecific(ctx context.Context, eventType def.EventType, serviceUid string, data proto.Message) error {
	be, err := eb.marshalEvent(ctx, eventType, 0, serviceUid, data)
	if err != nil {
		return err
	}
	if eb.isNatsEnabled() {
		// 发到nats
		eventData, err := proto.Marshal(be.event)
		if err != nil {
			return err
		}

		return eb.nc.Publish(eb.genKey(eb.specificPrefix, eventType, serviceUid), eventData)
	} else {
		// 没有使用nats,那么直接触发本地事件
		eb.publishSpecific(be.ctx, be.event)
		return nil
	}
}

// PublishSpecificLocal 发布本地指定服务事件
func (eb *Bus) PublishSpecificLocal(ctx context.Context, eventType def.EventType, serviceUid string, data proto.Message) error {
	be, err := eb.marshalEvent(ctx, eventType, 0, serviceUid, data)
	if err != nil {
		return err
	}
	eb.publishSpecific(be.ctx, be.event)
	return nil
}

// publishSpecific 发布特定服务事件
func (eb *Bus) publishSpecific(ctx context.Context, e *actor.Event) {
	key := eb.genKey(eb.specificPrefix, e.GetEventType(), e.GetServiceUid())
	eb.specificLock.RLock(key)
	defer eb.specificLock.RUnlock(key)
	if eventMap, ok := eb.specificSubscribers[e.GetEventType()]; ok {
		if subMap, ok := eventMap[e.GetServiceUid()]; ok {
			for _, ch := range subMap {
				j := job.NewEventBusJob()
				j.SetPayload(e)
				j.SetContext(ctx)
				j.SetDispatcherKey(e.GetDispatcherKey())
				j.SetPriority(def.Priority(e.GetPriority()))
				j.SetDeadline(e.GetDeadline())

				// PostJob 拥有 Job 所有权
				if err := ch.PostJob(j); err != nil {
					eb.Errorf("push specific event error: %v", err)
				}
			}
		}
	}
}

// SubscribeSpecific 订阅指定服务的事件
// eventType: 事件类型
// serviceUid: 目标服务的唯一ID(要订阅哪个服务的事件)
// svc: 订阅者服务
func (eb *Bus) SubscribeSpecific(eventType def.EventType, serviceUid string, svc inf.IListener) {
	key := eb.genKey(eb.specificPrefix, eventType, serviceUid)
	eb.specificLock.Lock(key)
	defer eb.specificLock.Unlock(key)
	var needListen bool
	if _, ok := eb.specificSubscribers[eventType]; !ok {
		eb.specificSubscribers[eventType] = make(map[string]map[string]inf.IListener)
	}
	if _, ok := eb.specificSubscribers[eventType][serviceUid]; !ok {
		eb.specificSubscribers[eventType][serviceUid] = make(map[string]inf.IListener)
		needListen = true
	}
	eb.specificSubscribers[eventType][serviceUid][svc.GetPid().GetServiceUid()] = svc
	if needListen {
		// 之前没有监听过这个事件类型和目标服务的组合
		if eb.isNatsEnabled() {
			if subscription, err := eb.nc.Subscribe(key, func(msg *nats.Msg) {
				// 解析数据
				be, err := eb.unmarshalEvent(msg.Data)
				if err != nil {
					eb.Errorf("unmarshal specific event error: %v", err)
					return
				}

				eb.publishSpecific(be.ctx, be.event)
			}); err == nil {
				eb.applySubPendingLimits(subscription)
				eb.addSub(key, subscription)
			} else {
				eb.Errorf("subscribe specific event from nats failed, error: %v", err)
			}
		}
	}
}

// UnSubscribeSpecific 取消订阅指定服务的事件
func (eb *Bus) UnSubscribeSpecific(eventType def.EventType, serviceUid string, svc inf.IListener) {
	key := eb.genKey(eb.specificPrefix, eventType, serviceUid)
	eb.specificLock.Lock(key)
	defer eb.specificLock.Unlock(key)
	var needUnListen bool
	if eventMap, ok := eb.specificSubscribers[eventType]; ok {
		if subMap, ok := eventMap[serviceUid]; ok {
			delete(subMap, svc.GetPid().GetServiceUid())
			if len(subMap) == 0 {
				delete(eventMap, serviceUid)
				needUnListen = true
			}
		}
		if len(eventMap) == 0 {
			delete(eb.specificSubscribers, eventType)
		}
	}
	if needUnListen {
		eb.unSubscribe(key)
	}
}
