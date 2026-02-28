package event

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/actor/mailbox/job"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"google.golang.org/protobuf/proto"
)

// PublishGlobal 发布全局事件(带限流和批处理)
func (eb *Bus) PublishGlobal(ctx context.Context, eventType def.EventType, data proto.Message) error {
	// 1. 检查限流
	if !eb.throttleManager.Allow(eventType) {
		atomic.AddInt64(&eb.metrics.TotalThrottled, 1)
		eb.Warnf("Event type %d throttled", eventType)
		return fmt.Errorf("event type %d throttled", eventType)
	}

	// 2. 封装事件
	be, err := eb.marshalEvent(ctx, eventType, 0, "", data)
	if err != nil {
		return err
	}

	// 3. 获取事件分类信息
	classification := eb.eventRegistry.GetClassification(eventType)

	// 4. 根据分类决定处理策略
	if eb.isNatsEnabled() && classification.BatchSize > 1 &&
		(classification.Category == CategoryMetrics ||
			classification.Category == CategoryStatistics ||
			classification.Category == CategoryBusinessBatch) {
		// 需要批处理的事件
		return eb.addToBatch(eventType, be)
	} else {
		// 立即处理的事件
		return eb.publishImmediately(be)
	}
}

// addToBatch 添加到批处理缓冲区
func (eb *Bus) addToBatch(eventType def.EventType, be *busEvent) error {
	eb.bufferMutex.Lock()
	defer eb.bufferMutex.Unlock()

	if eb.eventBuffer[eventType] == nil {
		eb.eventBuffer[eventType] = make([]*busEvent, 0)
	}
	eb.eventBuffer[eventType] = append(eb.eventBuffer[eventType], be)
	atomic.AddInt64(&eb.metrics.TotalPublished, 1)

	// 检查是否达到批量大小限制
	classification := eb.eventRegistry.GetClassification(eventType)
	if len(eb.eventBuffer[eventType]) >= classification.BatchSize {
		// 立即刷新该类型的批量
		events := eb.eventBuffer[eventType]
		delete(eb.eventBuffer, eventType)

		// 在新的goroutine中处理，避免阻塞
		go eb.flushEventBatch(eventType, events)
	}

	return nil
}

// publishImmediately 立即发布事件
func (eb *Bus) publishImmediately(be *busEvent) error {
	atomic.AddInt64(&eb.metrics.TotalPublished, 1)
	atomic.StoreInt64(&eb.metrics.LastEventTime, time.Now().UnixNano())

	if eb.isNatsEnabled() {
		// 发到nats
		eventData, err := proto.Marshal(be.event)
		if err != nil {
			return err
		}

		return eb.nc.Publish(eb.genKey(eb.globalPrefix, be.event.Type), eventData)
	} else {
		// 没有使用nats,那么直接触发本地事件
		eb.publishGlobal(be.ctx, be.event)
		return nil
	}
}

// publishGlobal 发布全局事件到本地订阅者
// ctx: 上下文，用于追踪和传递元数据
// e: actor.Event 纯数据载体
func (eb *Bus) publishGlobal(ctx context.Context, e *actor.Event) {
	key := eb.genKey(eb.globalPrefix, e.GetEventType())
	eb.globalLock.RLock(key)
	defer eb.globalLock.RUnlock(key)
	if subMap, ok := eb.globalSubscribers[e.GetEventType()]; ok {
		for _, ch := range subMap {
			j := job.NewEventBusJob()
			j.SetPayload(e)
			j.SetContext(ctx)
			j.SetDispatcherKey(e.GetDispatcherKey())
			j.SetPriority(def.Priority(e.GetPriority()))
			j.SetDeadline(e.GetDeadline())
			if err := ch.PostJob(j); err != nil {
				eb.WithContext(ctx).Errorf("push global event error: %v", err)
				j.Release()
			}
		}
	}
}

// PublishGlobalLocal 发布本地全局事件
func (eb *Bus) PublishGlobalLocal(ctx context.Context, eventType def.EventType, data proto.Message) error {
	be, err := eb.marshalEvent(ctx, eventType, 0, "", data)
	if err != nil {
		return err
	}

	eb.publishGlobal(be.ctx, be.event)
	return nil
}

func (eb *Bus) SubscribeGlobal(eventType def.EventType, svc inf.IListener) {
	key := eb.genKey(eb.globalPrefix, eventType)
	eb.globalLock.Lock(key)
	defer eb.globalLock.Unlock(key)
	var needListen bool
	if _, ok := eb.globalSubscribers[eventType]; !ok {
		eb.globalSubscribers[eventType] = make(map[string]inf.IListener)
		needListen = true
	}
	eb.globalSubscribers[eventType][svc.GetPid().GetServiceUid()] = svc
	if needListen {
		// 之前没有监听过这个事件类型
		if eb.isNatsEnabled() {
			if subscription, err := eb.nc.Subscribe(key, func(msg *nats.Msg) {
				// 解析数据
				be, err := eb.unmarshalEvent(msg.Data)
				if err != nil {
					eb.Errorf("unmarshal global event error: %v", err)
					return
				}

				eb.publishGlobal(be.ctx, be.event)
			}); err == nil {
				eb.applySubPendingLimits(subscription)
				eb.addSub(key, subscription)
			} else {
				eb.Errorf("subscribe global event from nats failed, error: %v", err)
			}
		}
	}
}

func (eb *Bus) UnSubscribeGlobal(eventType def.EventType, svc inf.IListener) {
	key := eb.genKey(eb.globalPrefix, eventType)
	eb.globalLock.Lock(key)
	defer eb.globalLock.Unlock(key)
	var needUnListen bool
	if _, ok := eb.globalSubscribers[eventType]; ok {
		delete(eb.globalSubscribers[eventType], svc.GetPid().GetServiceUid())
		if len(eb.globalSubscribers[eventType]) == 0 {
			delete(eb.globalSubscribers, eventType)
			needUnListen = true
		}
	}
	if needUnListen {
		eb.unSubscribe(key)
	}
}
