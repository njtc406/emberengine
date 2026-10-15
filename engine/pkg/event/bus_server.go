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

func (eb *Bus) PublishServer(ctx context.Context, eventType def.EventType, partition int32, data proto.Message) error {
	be, err := eb.marshalEvent(ctx, eventType, partition, "", data)
	if err != nil {
		return err
	}
	if eb.isNatsEnabled() {
		// 发到nats
		eventData, err := proto.Marshal(be.event)
		if err != nil {
			return err
		}

		return eb.nc.Publish(eb.genKey(eb.serverPrefix, eventType, partition), eventData)
	} else {
		// 没有使用nats,那么直接触发本地事件
		eb.publishServer(be.ctx, be.event)
		return nil
	}
}

// publishServer 发布服务器事件到本地订阅者
func (eb *Bus) publishServer(ctx context.Context, e *actor.Event) {
	key := eb.genKey(eb.serverPrefix, e.GetEventType(), e.GetPartition())
	eb.serverLock.RLock(key)
	defer eb.serverLock.RUnlock(key)
	if serverMap, ok := eb.serverSubscribers[e.GetEventType()]; ok {
		if subMap, ok := serverMap[e.GetPartition()]; ok {
			for _, ch := range subMap {
				j := job.NewEventBusJob()
				j.SetPayload(e)
				j.SetContext(ctx)
				j.SetDispatcherKey(e.GetDispatcherKey())
				j.SetPriority(def.Priority(e.GetPriority()))
				j.SetDeadline(e.GetDeadline())
				// PostJob 拥有 Job 所有权
				if err := ch.PostJob(j); err != nil {
					eb.Errorf("push server event error: %v", err)
				}
			}
		}
	}
}

func (eb *Bus) PublishServerLocal(ctx context.Context, eventType def.EventType, partition int32, data proto.Message) error {
	be, err := eb.marshalEvent(ctx, eventType, partition, "", data)
	if err != nil {
		return err
	}
	eb.publishServer(be.ctx, be.event)
	return nil
}

func (eb *Bus) SubscribeServer(eventType def.EventType, svc inf.IListener) {
	key := eb.genKey(eb.serverPrefix, eventType, svc.GetPartition())
	eb.serverLock.Lock(key)
	defer eb.serverLock.Unlock(key)
	var needListen bool
	if _, ok := eb.serverSubscribers[eventType]; !ok {
		eb.serverSubscribers[eventType] = make(map[int32]map[string]inf.IListener)
	}
	if _, ok := eb.serverSubscribers[eventType][svc.GetPartition()]; !ok {
		eb.serverSubscribers[eventType][svc.GetPartition()] = make(map[string]inf.IListener)
		needListen = true
	}
	eb.serverSubscribers[eventType][svc.GetPartition()][svc.GetPid().GetServiceUid()] = svc
	if needListen {
		// 之前没有监听过这个事件类型
		if eb.isNatsEnabled() {
			if subscription, err := eb.nc.Subscribe(key, func(msg *nats.Msg) {
				// 解析数据
				be, err := eb.unmarshalEvent(msg.Data)
				if err != nil {
					eb.Errorf("unmarshal partition[%d] event error: %v", svc.GetPartition(), err)
					return
				}
				eb.publishServer(be.ctx, be.event)
			}); err == nil {
				eb.applySubPendingLimits(subscription)
				eb.addSub(key, subscription)
			} else {
				eb.Errorf("subscribe partition[%d] event error: %v", svc.GetPartition(), err)
			}
		}
	}
}

func (eb *Bus) UnSubscribeServer(eventType def.EventType, svc inf.IListener) {
	key := eb.genKey(eb.serverPrefix, eventType, svc.GetPartition())
	eb.serverLock.Lock(key)
	defer eb.serverLock.Unlock(key)
	var needUnListen bool
	if subMap, ok := eb.serverSubscribers[eventType]; ok {
		if nameMap, ok := subMap[svc.GetPartition()]; ok {
			delete(nameMap, svc.GetPid().GetServiceUid())
			if len(nameMap) == 0 {
				delete(subMap, svc.GetPartition())
				needUnListen = true
			}
		}
		if len(subMap) == 0 {
			delete(eb.serverSubscribers, eventType)
		}
	}
	if needUnListen {
		eb.unSubscribe(key)
	}
}
