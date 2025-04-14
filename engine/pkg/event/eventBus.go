// Package eventBus
// @Title  全局事件服务
// @Description  desc
// @Author  yr  2025/4/10
// @Update  yr  2025/4/10
package event

import (
	"crypto/tls"
	"fmt"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/shardedlock"
	"google.golang.org/protobuf/proto"
	"strings"
)

var bus *Bus

type Bus struct {
	nc *nats.Conn

	// 全体事件(所有订阅的服务都会收到)
	globalPrefix      string // 全局事件前缀
	globalLock        *shardedlock.ShardedRWLock
	globalSubscribers map[int32]map[string]inf.IListener // map[事件类型]map[服务唯一id]事件通道

	// 服务器事件(只有相同服务器的订阅会收到)
	serverPrefix      string // 服务器事件前缀
	serverLock        *shardedlock.ShardedRWLock
	serverSubscribers map[int32]map[int32]map[string]inf.IListener // map[事件类型]map[服务器id]map[服务唯一id]事件通道

	subMap map[string]*nats.Subscription
}

func GetEventBus() *Bus {
	if bus == nil {
		bus = &Bus{}
	}
	return bus
}

func switchOpts(conf *config.NatsConf) []nats.Option {
	var opts []nats.Option
	if conf != nil {
		if conf.MaxReconnects == 0 {
			conf.MaxReconnects = def.NatsDefaultMaxReconnects
		}
		opts = append(opts, nats.MaxReconnects(conf.MaxReconnects))

		if conf.ReconnectWait == 0 {
			conf.ReconnectWait = def.NatsDefaultReconnectWait
		}
		opts = append(opts, nats.ReconnectWait(conf.ReconnectWait))

		if conf.PingInterval == 0 {
			conf.PingInterval = def.NatsDefaultPingInterval
		}
		opts = append(opts, nats.PingInterval(conf.PingInterval))

		if conf.PingMaxOutstanding == 0 {
			conf.PingMaxOutstanding = def.NatsDefaultPingMaxOutstanding
		}
		opts = append(opts, nats.MaxPingsOutstanding(conf.PingMaxOutstanding))

		if conf.ReconnectBufSize == 0 {
			conf.ReconnectBufSize = def.NatsDefaultReconnectBufSize
		}
		opts = append(opts, nats.ReconnectBufSize(conf.ReconnectBufSize))

		if conf.Token != "" {
			opts = append(opts, nats.Token(conf.Token))
		} else {
			if conf.UserName != "" {
				opts = append(opts, nats.UserInfo(conf.UserName, conf.Password))
			}
		}

		if conf.Secure != "" {
			opts = append(opts, nats.Secure(&tls.Config{InsecureSkipVerify: true}))
		}

		if conf.CAs != "" {
			opts = append(opts, nats.RootCAs(conf.CAs))
		}

		if conf.Cert != "" && conf.CertKey != "" {
			opts = append(opts, nats.ClientCert(conf.Cert, conf.CertKey))
		}
	}
	return opts
}

func (eb *Bus) Init(conf *config.EventBusConf) {
	if conf != nil && conf.NatsConf != nil && len(conf.NatsConf.EndPoints) != 0 {
		opts := switchOpts(conf.NatsConf)

		nc, err := nats.Connect(strings.Join(conf.NatsConf.EndPoints, ","), opts...)
		if err != nil {
			log.SysLogger.Panic(err)
			//panic(err)
		}
		eb.nc = nc
		eb.globalPrefix = conf.GlobalPrefix
		if eb.globalPrefix == "" {
			eb.globalPrefix = def.NatsDefaultGlobalPrefix
		}
		eb.serverPrefix = conf.ServerPrefix
		if eb.serverPrefix == "" {
			eb.serverPrefix = def.NatsDefaultServerPrefix
		}
		log.SysLogger.Info("==========> nats init success")
	}

	var shardCount = def.NatsDefaultShardCount
	if conf != nil && conf.ShardCount > 0 {
		shardCount = conf.ShardCount
	}

	eb.globalLock = shardedlock.NewShardedRWLock(shardCount)
	eb.globalSubscribers = make(map[int32]map[string]inf.IListener)
	eb.serverLock = shardedlock.NewShardedRWLock(shardCount)
	eb.serverSubscribers = make(map[int32]map[int32]map[string]inf.IListener)

	eb.subMap = make(map[string]*nats.Subscription)
}

func (eb *Bus) Stop() {
	if eb.nc != nil {
		eb.nc.Close()
	}
}

func (eb *Bus) genKey(format string, args ...interface{}) string {
	return fmt.Sprintf(format, args...)
}

func (eb *Bus) marshalEvent(eventType, serverId int32, data proto.Message, header dto.Header) (*actor.Event, error) {
	// 组装数据
	rawData, err := proto.Marshal(data)
	if err != nil {
		return nil, err
	}

	var traceId string
	if v, ok := header["TraceId"]; ok && v != "" {
		traceId = v
	} else {
		traceId = uuid.NewString()
	}

	e := &actor.Event{
		EventType: eventType,
		Data: &actor.EventData{
			Header:  header,
			RawData: rawData,
		},
		ServerId: serverId,
		TraceId:  traceId,
	}

	return e, nil
}

func (eb *Bus) isNatsEnabled() bool {
	return eb.nc != nil
}

func (eb *Bus) unmarshalEvent(eventData []byte) (*actor.Event, error) {
	e := &actor.Event{}
	if err := proto.Unmarshal(eventData, e); err != nil {
		return nil, err
	}
	return e, nil
}

// PublishGlobal 发布全局事件
func (eb *Bus) PublishGlobal(eventType int32, data proto.Message, header dto.Header) error {
	e, err := eb.marshalEvent(eventType, 0, data, header)
	if err != nil {
		return err
	}
	if eb.isNatsEnabled() {

		// 发到nats
		eventData, err := proto.Marshal(e)
		if err != nil {
			return err
		}

		return eb.nc.Publish(eb.genKey(eb.globalPrefix, eventType), eventData)
	} else {
		// 没有使用nats,那么直接触发本地事件
		eb.publishGlobal(e)
		return nil
	}
}

func (eb *Bus) publishGlobal(e *actor.Event) {
	key := eb.genKey(eb.globalPrefix, e.EventType)
	eb.globalLock.RLock(key)
	defer eb.globalLock.RUnlock(key)
	if subMap, ok := eb.globalSubscribers[e.EventType]; ok {
		ev := NewEvent()
		ev.Type = ServiceGlobalEventTrigger
		ev.Data = e
		ev.Key = e.GetKey()
		ev.Priority = e.GetPriority()

		for _, ch := range subMap {
			if err := ch.PushEvent(ev); err != nil {
				log.SysLogger.Errorf("push global event error: %v", err)
				//fmt.Println("push global event error:", err)
			}
		}
	}
}

// PublishGlobalLocal 发布本地全局事件
func (eb *Bus) PublishGlobalLocal(eventType int32, data proto.Message, header dto.Header) error {
	e, err := eb.marshalEvent(eventType, 0, data, header)
	if err != nil {
		return err
	}

	eb.publishGlobal(e)
	return nil
}

func (eb *Bus) PublishServer(eventType, serverId int32, data proto.Message, header dto.Header) error {
	e, err := eb.marshalEvent(eventType, serverId, data, header)
	if err != nil {
		return err
	}
	if eb.isNatsEnabled() {
		// 发到nats
		eventData, err := proto.Marshal(e)
		if err != nil {
			return err
		}

		return eb.nc.Publish(eb.genKey(eb.serverPrefix, eventType, serverId), eventData)
	} else {
		// 没有使用nats,那么直接触发本地事件
		eb.publishServer(e)
		return nil
	}
}

func (eb *Bus) publishServer(e *actor.Event) {
	key := eb.genKey(eb.serverPrefix, e.EventType, e.ServerId)
	eb.serverLock.RLock(key)
	defer eb.serverLock.RUnlock(key)
	if serverMap, ok := eb.serverSubscribers[e.EventType]; ok {
		ev := NewEvent()
		ev.Type = ServiceGlobalEventTrigger
		ev.Data = e
		ev.Key = e.GetKey()
		ev.Priority = e.GetPriority()
		if subMap, ok := serverMap[e.ServerId]; ok {
			for _, ch := range subMap {
				if err := ch.PushEvent(ev); err != nil {
				}
			}
		}
	}
}

func (eb *Bus) PublishServerLocal(eventType, serverId int32, data proto.Message, header dto.Header) error {
	e, err := eb.marshalEvent(eventType, serverId, data, header)
	if err != nil {
		return err
	}
	eb.publishServer(e)
	return nil
}

func (eb *Bus) SubscribeGlobal(eventType int32, svc inf.IListener) {
	key := eb.genKey(eb.globalPrefix, eventType)
	eb.globalLock.Lock(key)
	defer eb.globalLock.Unlock(key)
	var needListen bool
	if _, ok := eb.globalSubscribers[eventType]; !ok {
		eb.globalSubscribers[eventType] = make(map[string]inf.IListener)
		needListen = true
	}
	eb.globalSubscribers[eventType][svc.GetName()] = svc
	if needListen {
		// 之前没有监听过这个事件类型
		if eb.isNatsEnabled() {
			if subscription, err := eb.nc.Subscribe(key, func(msg *nats.Msg) {
				// 解析数据
				e, err := eb.unmarshalEvent(msg.Data)
				if err != nil {
					log.SysLogger.Errorf("unmarshal global event error: %v", err)
					//fmt.Println("unmarshal global event error:", err)
					return
				}

				eb.publishGlobal(e)
			}); err == nil {
				//fmt.Println("subscribe global event from nats success")
				eb.subMap[key] = subscription
			} else {
				log.SysLogger.Errorf("subscribe global event from nats failed, error: %v", err)
				//fmt.Println("subscribe global event from nats failed, error:", err)
			}
		}
	}
}

func (eb *Bus) SubscribeServer(eventType int32, svc inf.IListener) {
	key := eb.genKey(eb.serverPrefix, eventType, svc.GetServerId())
	eb.serverLock.Lock(key)
	defer eb.serverLock.Unlock(key)
	var needListen bool
	if _, ok := eb.serverSubscribers[eventType]; !ok {
		eb.serverSubscribers[eventType] = make(map[int32]map[string]inf.IListener)
	}
	if _, ok := eb.serverSubscribers[eventType][svc.GetServerId()]; !ok {
		eb.serverSubscribers[eventType][svc.GetServerId()] = make(map[string]inf.IListener)
		needListen = true
	}
	eb.serverSubscribers[eventType][svc.GetServerId()][svc.GetName()] = svc
	if needListen {
		// 之前没有监听过这个事件类型
		if eb.isNatsEnabled() {
			if subscription, err := eb.nc.Subscribe(key, func(msg *nats.Msg) {
				// 解析数据
				e, err := eb.unmarshalEvent(msg.Data)
				if err != nil {
					log.SysLogger.Errorf("unmarshal server[%d] event error: %v", svc.GetServerId(), err)
					//fmt.Println("unmarshal server[", svc.GetServerId(), "] event error:", err)
					return
				}
				eb.publishServer(e)
			}); err == nil {
				//fmt.Println("subscribe server[", svc.GetServerId(), "] event success")
				eb.subMap[key] = subscription
			} else {
				log.SysLogger.Errorf("subscribe server[%d] event error: %v", svc.GetServerId(), err)
				//fmt.Println("subscribe server[", svc.GetServerId(), "] event error:", err)
			}
		}
	}
}

func (eb *Bus) unSubscribe(key string) {
	// 没有订阅者了,那么取消监听
	if eb.isNatsEnabled() {
		if subscription, ok := eb.subMap[key]; ok {
			if err := subscription.Unsubscribe(); err != nil {
				log.SysLogger.Errorf("unsubscribe global event error: %v", err)
				//fmt.Println("unsubscribe global event error:", err)
			}
			delete(eb.subMap, key)
		}
	}
}

func (eb *Bus) UnSubscribeGlobal(eventType int32, svc inf.IListener) {
	key := eb.genKey(eb.globalPrefix, eventType)
	eb.globalLock.Lock(key)
	defer eb.globalLock.Unlock(key)
	var needUnListen bool
	if _, ok := eb.globalSubscribers[eventType]; ok {
		delete(eb.globalSubscribers[eventType], svc.GetName())
		if len(eb.globalSubscribers[eventType]) == 0 {
			delete(eb.globalSubscribers, eventType)
			needUnListen = true
		}
	}
	if needUnListen {
		eb.unSubscribe(key)
	}
}

func (eb *Bus) UnSubscribeServer(eventType int32, svc inf.IListener) {
	key := eb.genKey(eb.serverPrefix, eventType, svc.GetServerId())
	eb.serverLock.Lock(key)
	defer eb.serverLock.Unlock(key)
	var needUnListen bool
	if subMap, ok := eb.serverSubscribers[eventType]; ok {
		if nameMap, ok := subMap[svc.GetServerId()]; ok {
			delete(nameMap, svc.GetName())
			if len(nameMap) == 0 {
				delete(subMap, svc.GetServerId())
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
