// Package event
// @Title  title
// @Description  desc
// @Author  yr  2025/6/12
// @Update  yr  2025/6/12
package event

import (
	"context"
	"github.com/nats-io/nats.go"
	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"google.golang.org/protobuf/proto"
)

// 主节点事件,从节点监听,收到事件后交由inf.IListener来处理

// dispatchMaster 主节点处理从节点事件
func (eb *Bus) dispatchMaster(e *actor.Event) {
	key := eb.genKey(eb.slavePrefix, e.ServiceUid)
	eb.masterLock.RLock(key)
	defer eb.masterLock.RUnlock(key)
	// 主从节点的ServiceUid是一样的,所以直接取,不允许主从或者多从在同一个节点上,这样就失去了主从的意义
	if ch, ok := eb.masterSubscribers[e.ServiceUid]; ok {
		ev := NewEvent()
		ev.Type = ServiceSlaverEventTrigger
		ev.Data = e
		ev.Key = e.GetKey()
		ev.Priority = e.GetPriority()
		if err := ch.PushEvent(ev); err != nil {
			ev.Release()
			log.SysLogger.Errorf("push master event error: %v", err)
		}
	}
}

// dispatchSlaver 从节点处理主节点事件
func (eb *Bus) dispatchSlaver(e *actor.Event) {
	key := eb.genKey(eb.masterPrefix, e.ServiceUid)
	eb.masterLock.RLock(key)
	defer eb.masterLock.RUnlock(key)
	// 主从节点的ServiceUid是一样的,所以直接取,不允许主从或者多从在同一个节点上,这样就失去了主从的意义
	if ch, ok := eb.masterSubscribers[e.ServiceUid]; ok {
		ev := NewEvent()
		ev.Type = ServiceMasterEventTrigger
		ev.Data = e
		ev.Key = e.GetKey()
		ev.Priority = e.GetPriority()
		if err := ch.PushEvent(ev); err != nil {
			ev.Release()
			log.SysLogger.Errorf("push master event error: %v", err)
		}
	}
}

// SubscribeMaster 订阅主节点事件(从节点订阅)
func (eb *Bus) SubscribeMaster(svc inf.IListener) {
	if eb.isNatsEnabled() {
		serviceUid := svc.GetPid().GetServiceUid()
		key := eb.genKey(eb.masterPrefix, serviceUid) // 订阅主节点事件
		eb.slaveLock.Lock(key)
		defer eb.slaveLock.Unlock(key)
		eb.slaveSubscribers[serviceUid] = svc // 记录一下订阅者
		if subscription, err := eb.nc.Subscribe(key, func(msg *nats.Msg) {
			// 解析数据
			e, err := eb.unmarshalEvent(msg.Data)
			if err != nil {
				log.SysLogger.Errorf("unmarshal master event error: %v", err)
				//fmt.Println("unmarshal master event error:", err)
				return
			}
			eb.dispatchSlaver(e) // 通知从节点处理主节点事件
		}); err == nil {
			eb.addSub(key, subscription)
		} else {
			log.SysLogger.Errorf("subscribe master[%s] event error: %v", serviceUid, err)
		}
	}
}

// UnSubscribeMaster 取消订阅主节点事件(从节点取消订阅,这是当从节点升级为主节点时会用到)
func (eb *Bus) UnSubscribeMaster(svc inf.IListener) {
	serviceUid := svc.GetPid().GetServiceUid()
	key := eb.genKey(eb.masterPrefix, serviceUid)
	eb.slaveLock.Lock(key)
	defer eb.slaveLock.Unlock(key)
	delete(eb.slaveSubscribers, serviceUid)
	eb.unSubscribe(key)
}

// PublishMaster 发送主节点事件(从节点发送给主节点)
func (eb *Bus) PublishMaster(ctx context.Context, svc inf.IListener, data proto.Message) error {
	if eb.isNatsEnabled() {
		serviceUid := svc.GetPid().GetServiceUid()
		// TODO 记得增加节点相关标识
		e, err := eb.marshalEvent(ctx, 0, svc.GetServerId(), serviceUid, data)
		if err != nil {
			return err
		}
		// 发到nats
		eventData, err := e.Marshal()
		if err != nil {
			return err
		}

		return eb.nc.Publish(eb.genKey(eb.slavePrefix, serviceUid), eventData)
	} else {
		// 同节点内无法做主从,所以直接丢弃
		return nil
	}
}

// TODO 下面这部分还没改

// SubscribeSlaver 订阅从节点事件(主节点订阅)
func (eb *Bus) SubscribeSlaver(svc inf.IListener) {
	if eb.isNatsEnabled() {
		serviceUid := svc.GetPid().GetServiceUid()
		key := eb.genKey(eb.slavePrefix, serviceUid)
		eb.masterLock.Lock(key)
		defer eb.masterLock.Unlock(key)
		eb.masterSubscribers[serviceUid] = svc
		if subscription, err := eb.nc.Subscribe(key, func(msg *nats.Msg) {
			// 解析数据
			e, err := eb.unmarshalEvent(msg.Data)
			if err != nil {
				log.SysLogger.Errorf("unmarshal master event error: %v", err)
				//fmt.Println("unmarshal master event error:", err)
				return
			}
			eb.dispatchMaster(e)
		}); err == nil {
			eb.addSub(key, subscription)
		} else {
			log.SysLogger.Errorf("subscribe master[%s] event error: %v", serviceUid, err)
		}
	}
}

func (eb *Bus) UnSubscribeSlaver(svc inf.IListener) {
	serviceUid := svc.GetPid().GetServiceUid()
	key := eb.genKey(eb.slavePrefix, serviceUid)
	eb.slaveLock.Lock(key)
	defer eb.slaveLock.Unlock(key)
	delete(eb.slaveSubscribers, serviceUid)
	eb.unSubscribe(key)
}

func (eb *Bus) PublishSlaver(ctx context.Context, svc inf.IListener, data proto.Message) error {
	serviceUid := svc.GetPid().GetServiceUid()
	e, err := eb.marshalEvent(ctx, 0, 0, serviceUid, data)
	if err != nil {
		return err
	}
	if eb.isNatsEnabled() {
		// 发到nats
		eventData, err := proto.Marshal(e)
		if err != nil {
			return err
		}
		return eb.nc.Publish(eb.genKey(eb.slavePrefix, serviceUid), eventData)
	}
	return nil
}

// TODO 这个等改完了上面的部分再看怎么来做
// 看了下,这个好像没法使用request来做,因为server注册的是一样的，没法区分出来
// SyncPrimaryState 同步主节点状态(从节点同步主节点状态),阻塞调用
func (eb *Bus) SyncPrimaryState(ctx context.Context, svc inf.IListener, req, resp proto.Message) error {
	if eb.isNatsEnabled() {
		serviceUid := svc.GetPid().GetServiceUid()
		// TODO 记得增加节点相关标识
		e, err := eb.marshalEvent(ctx, ServiceSlaverEventRequest, svc.GetServerId(), serviceUid, req)
		if err != nil {
			return err
		}
		// 发到nats
		eventData, err := e.Marshal()
		if err != nil {
			return err
		}

		msg, err := eb.nc.Request(eb.genKey(eb.masterPrefix, serviceUid), eventData, def.DefaultRpcTimeout)
		if err != nil {
			return err
		}

		respEvent, err := eb.unmarshalEvent(msg.Data)
		if err != nil {
			log.SysLogger.Errorf("unmarshal master event error: %v", err)
			//fmt.Println("unmarshal master event error:", err)
			return err
		}

		// 解析数据
		if err = proto.Unmarshal(respEvent.Data.RawData, resp); err != nil {
			return err
		}

		return nil
	}

	return def.ErrPrimarySecondNotSupported
}
