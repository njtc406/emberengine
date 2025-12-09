// Package eventBus
// @Title  全局事件服务
// @Description  desc
// @Author  yr  2025/4/10
// @Update  yr  2025/4/10
package event

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/emberctx"
	"github.com/njtc406/emberengine/engine/pkg/utils/shardedlock"
	"google.golang.org/protobuf/proto"
)

// EventMetrics 事件指标统计
type EventMetrics struct {
	TotalPublished   int64 `json:"total_published"`
	TotalDelivered   int64 `json:"total_delivered"`
	TotalThrottled   int64 `json:"total_throttled"`
	TotalBatched     int64 `json:"total_batched"`
	AvgDeliveryTime  int64 `json:"avg_delivery_time_ns"`
	PeakEventRate    int64 `json:"peak_event_rate"`
	CurrentEventRate int64 `json:"current_event_rate"`
	LastEventTime    int64 `json:"last_event_time"`
}

var bus *Bus

type Bus struct {
	nc     *nats.Conn // TODO 目前只支持nats,后续再看要不要扩展吧
	enable atomic.Int32

	// 全体事件(所有订阅的服务都会收到)
	globalPrefix      string                             // 全局事件前缀
	globalLock        *shardedlock.ShardedRWLock         // 用分段锁提升并发能力
	globalSubscribers map[int32]map[string]inf.IListener // map[事件类型]map[服务唯一id]事件通道

	// 服务器事件(只有相同服务器的订阅会收到)
	serverPrefix      string // 服务器事件前缀
	serverLock        *shardedlock.ShardedRWLock
	serverSubscribers map[int32]map[int32]map[string]inf.IListener // map[事件类型]map[服务器id]map[服务唯一id]事件通道

	// 特定事件(只有订阅者会收到)
	specificPrefix      string // 特定事件前缀
	specificLock        *shardedlock.ShardedRWLock
	specificSubscribers map[int32]map[string]map[string]inf.IListener // map[事件类型]map[目标服务唯一id]map[订阅者服务唯一id]事件通道

	subMap sync.Map // 记录所有订阅 map[string]*nats.Subscription

	// 新增的事件分类和限流系统
	eventRegistry   *EventRegistry           // 事件注册表
	throttleManager *ThrottleManager         // 限流管理器
	eventBuffer     map[int32][]*actor.Event // 事件缓冲区 (按类型批处理)
	bufferMutex     sync.RWMutex             // 缓冲区锁
	batchTicker     *time.Ticker             // 批处理定时器
	metrics         *EventMetrics            // 事件指标
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
	// 初始化事件分类和限流系统
	eb.eventRegistry = NewEventRegistry()
	eb.throttleManager = NewThrottleManager(eb.eventRegistry)
	eb.eventBuffer = make(map[int32][]*actor.Event)
	eb.metrics = &EventMetrics{}

	// 启动批处理定时器 (每100ms处理一次缓冲)
	eb.batchTicker = time.NewTicker(100 * time.Millisecond)
	go eb.processBatchedEvents()

	if conf != nil && conf.NatsConf != nil && len(conf.NatsConf.EndPoints) != 0 {
		opts := switchOpts(conf.NatsConf)

		nc, err := nats.Connect(strings.Join(conf.NatsConf.EndPoints, ","), opts...)
		if err != nil {
			log.SysLogger.Panic(err)
			//panic(err)
		}
		eb.nc = nc
		eb.enable.Store(1)
		eb.globalPrefix = conf.GlobalPrefix
		if eb.globalPrefix == "" {
			eb.globalPrefix = def.NatsDefaultGlobalPrefix
		}
		eb.serverPrefix = conf.ServerPrefix
		if eb.serverPrefix == "" {
			eb.serverPrefix = def.NatsDefaultServerPrefix
		}
		eb.specificPrefix = conf.SpecificPrefix
		if eb.specificPrefix == "" {
			eb.specificPrefix = def.DefaultSpecificPrefix
		}
		log.SysLogger.Debug("==========> nats init success")
	}

	var shardCount = def.NatsDefaultShardCount
	if conf != nil && conf.ShardCount > 0 {
		shardCount = conf.ShardCount
	}

	eb.globalLock = shardedlock.NewShardedRWLock(shardCount)
	eb.globalSubscribers = make(map[int32]map[string]inf.IListener)
	eb.serverLock = shardedlock.NewShardedRWLock(shardCount)
	eb.serverSubscribers = make(map[int32]map[int32]map[string]inf.IListener)
	eb.specificLock = shardedlock.NewShardedRWLock(shardCount)
	eb.specificSubscribers = make(map[int32]map[string]map[string]inf.IListener)
}

func (eb *Bus) Stop() {
	if eb.nc != nil && eb.enable.CompareAndSwap(1, 0) {
		eb.nc.Close()
		eb.nc = nil
	}

	// 停止批处理定时器
	if eb.batchTicker != nil {
		eb.batchTicker.Stop()
	}

	// 处理剩余的缓冲事件
	eb.flushAllBuffers()
}

// processBatchedEvents 处理批量事件
func (eb *Bus) processBatchedEvents() {
	for range eb.batchTicker.C {
		eb.flushAllBuffers()
	}
}

// flushAllBuffers 刷新所有缓冲区
func (eb *Bus) flushAllBuffers() {
	eb.bufferMutex.Lock()
	defer eb.bufferMutex.Unlock()

	for eventType, events := range eb.eventBuffer {
		if len(events) > 0 {
			eb.flushEventBatch(eventType, events)
			delete(eb.eventBuffer, eventType)
		}
	}
}

// flushEventBatch 刷新指定类型的事件批量
func (eb *Bus) flushEventBatch(eventType int32, events []*actor.Event) {
	if len(events) == 0 {
		return
	}

	classification := eb.eventRegistry.GetClassification(eventType)

	// 按照事件范围选择批量处理策略
	switch classification.Scope {
	case ScopeGlobal:
		for _, event := range events {
			eb.publishGlobal(event)
		}
	case ScopeCluster, ScopeRegion, ScopeNode:
		for _, event := range events {
			eb.publishServer(event)
		}
	default:
		// 本地事件直接处理
		for _, event := range events {
			eb.publishGlobal(event) // 默认作为全局事件处理
		}
	}

	// 更新指标
	atomic.AddInt64(&eb.metrics.TotalBatched, int64(len(events)))
	atomic.AddInt64(&eb.metrics.TotalDelivered, int64(len(events)))
}

func (eb *Bus) addSub(key string, sub *nats.Subscription) {
	eb.subMap.Store(key, sub)
}

func (eb *Bus) loadAndDelSub(key string) (*nats.Subscription, bool) {
	sub, ok := eb.subMap.LoadAndDelete(key)
	if !ok {
		return nil, false
	}
	return sub.(*nats.Subscription), true
}

func (eb *Bus) genKey(format string, args ...interface{}) string {
	return fmt.Sprintf(format, args...)
}

func (eb *Bus) marshalEvent(ctx context.Context, eventType, serverId int32, serviceUid string, data proto.Message) (*actor.Event, error) {
	// 组装数据
	rawData, err := proto.Marshal(data)
	if err != nil {
		return nil, err
	}

	if emberctx.GetHeaderValue(ctx, def.DefaultTraceIdKey) == "" {
		emberctx.AddHeader(ctx, def.DefaultDispatcherKey, uuid.NewString())
	}

	// TODO 事件中可能还需要带上一个节点信息,好区分是发给哪个从服务的
	e := &actor.Event{
		EventType: eventType,
		Data: &actor.EventData{
			Header:  emberctx.ToHeaders(ctx),
			RawData: rawData,
		},
		ServerId:   serverId,
		ServiceUid: serviceUid,
	}

	return e, nil
}

func (eb *Bus) isNatsEnabled() bool {
	return eb.enable.Load() == 1
}

func (eb *Bus) unmarshalEvent(eventData []byte) (*actor.Event, error) {
	e := &actor.Event{}
	if err := proto.Unmarshal(eventData, e); err != nil {
		return nil, err
	}
	return e, nil
}

// TODO 全局事件这里可以考虑订阅指定服务的事件，比如当处于某个场景服时，可以只订阅该场景服的事件，就可以实现广播功能，可以通过广播减少rpc寻址调用
// PublishGlobal 发布全局事件(带限流和批处理)
func (eb *Bus) PublishGlobal(ctx context.Context, eventType int32, data proto.Message) error {
	// 1. 检查限流
	if !eb.throttleManager.Allow(eventType) {
		atomic.AddInt64(&eb.metrics.TotalThrottled, 1)
		log.SysLogger.Warnf("Event type %d throttled", eventType)
		return fmt.Errorf("event type %d throttled", eventType)
	}

	// 2. 封装事件
	e, err := eb.marshalEvent(ctx, eventType, 0, "", data)
	if err != nil {
		return err
	}

	// 3. 获取事件分类信息
	classification := eb.eventRegistry.GetClassification(eventType)

	// 4. 根据分类决定处理策略
	if classification.BatchSize > 1 &&
		(classification.Category == CategoryMetrics ||
			classification.Category == CategoryStatistics ||
			classification.Category == CategoryBusinessBatch) {
		// 需要批处理的事件
		return eb.addToBatch(eventType, e)
	} else {
		// 立即处理的事件
		return eb.publishImmediately(e)
	}
}

// addToBatch 添加到批处理缓冲区
func (eb *Bus) addToBatch(eventType int32, event *actor.Event) error {
	eb.bufferMutex.Lock()
	defer eb.bufferMutex.Unlock()

	if eb.eventBuffer[eventType] == nil {
		eb.eventBuffer[eventType] = make([]*actor.Event, 0)
	}

	eb.eventBuffer[eventType] = append(eb.eventBuffer[eventType], event)
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
func (eb *Bus) publishImmediately(event *actor.Event) error {
	atomic.AddInt64(&eb.metrics.TotalPublished, 1)
	atomic.StoreInt64(&eb.metrics.LastEventTime, time.Now().UnixNano())

	if eb.isNatsEnabled() {
		// 发到nats
		eventData, err := event.Marshal()
		if err != nil {
			return err
		}

		return eb.nc.Publish(eb.genKey(eb.globalPrefix, event.EventType), eventData)
	} else {
		// 没有使用nats,那么直接触发本地事件
		eb.publishGlobal(event)
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
		ev.SetHeader(def.DefaultDispatcherKey, e.GetDispatcherKey())
		ev.SetHeader(def.DefaultPriorityKey, e.GetPriority())

		for _, ch := range subMap {
			if err := ch.PushEvent(ev); err != nil {
				log.SysLogger.Errorf("push global event error: %v", err)
				//fmt.Println("push global event error:", err)
			}
		}
	}
}

// PublishGlobalLocal 发布本地全局事件
func (eb *Bus) PublishGlobalLocal(ctx context.Context, eventType int32, data proto.Message) error {
	e, err := eb.marshalEvent(ctx, eventType, 0, "", data)
	if err != nil {
		return err
	}

	eb.publishGlobal(e)
	return nil
}

func (eb *Bus) PublishServer(ctx context.Context, eventType, serverId int32, data proto.Message) error {
	e, err := eb.marshalEvent(ctx, eventType, serverId, "", data)
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
		ev.SetHeader(def.DefaultDispatcherKey, e.GetDispatcherKey())
		ev.SetHeader(def.DefaultPriorityKey, e.GetPriority())

		if subMap, ok := serverMap[e.ServerId]; ok {
			for _, ch := range subMap {
				if err := ch.PushEvent(ev); err != nil {
					log.SysLogger.Errorf("push server event error: %v", err)
				}
			}
		}
	}
}

func (eb *Bus) PublishServerLocal(ctx context.Context, eventType, serverId int32, data proto.Message) error {
	e, err := eb.marshalEvent(ctx, eventType, serverId, "", data)
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
	eb.globalSubscribers[eventType][svc.GetPid().GetServiceUid()] = svc
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
				eb.addSub(key, subscription)
			} else {
				log.SysLogger.Errorf("subscribe global event from nats failed, error: %v", err)
				//fmt.Println("subscribe global event from nats failed, error:", err)
			}
		}
	}
}

// PublishSpecific 发布指定服务的事件(只有订阅了该服务事件的服务会收到)
func (eb *Bus) PublishSpecific(ctx context.Context, eventType int32, serviceUid string, data proto.Message) error {
	e, err := eb.marshalEvent(ctx, eventType, 0, serviceUid, data)
	if err != nil {
		return err
	}
	if eb.isNatsEnabled() {
		// 发到nats
		eventData, err := proto.Marshal(e)
		if err != nil {
			return err
		}

		return eb.nc.Publish(eb.genKey(eb.specificPrefix, eventType, serviceUid), eventData)
	} else {
		// 没有使用nats,那么直接触发本地事件
		eb.publishSpecific(e)
		return nil
	}
}

// PublishSpecificLocal 发布本地指定服务事件
func (eb *Bus) PublishSpecificLocal(ctx context.Context, eventType int32, serviceUid string, data proto.Message) error {
	e, err := eb.marshalEvent(ctx, eventType, 0, serviceUid, data)
	if err != nil {
		return err
	}
	eb.publishSpecific(e)
	return nil
}

// publishSpecific 发布特定服务事件
func (eb *Bus) publishSpecific(e *actor.Event) {
	key := eb.genKey(eb.specificPrefix, e.EventType, e.ServiceUid)
	eb.specificLock.RLock(key)
	defer eb.specificLock.RUnlock(key)
	if eventMap, ok := eb.specificSubscribers[e.EventType]; ok {
		if subMap, ok := eventMap[e.ServiceUid]; ok {
			ev := NewEvent()
			ev.Type = ServiceGlobalEventTrigger
			ev.Data = e
			ev.SetHeader(def.DefaultDispatcherKey, e.GetDispatcherKey())
			ev.SetHeader(def.DefaultPriorityKey, e.GetPriority())

			for _, ch := range subMap {
				if err := ch.PushEvent(ev); err != nil {
					log.SysLogger.Errorf("push specific event error: %v", err)
				}
			}
		}
	}
}

// SubscribeSpecific 订阅指定服务的事件
// eventType: 事件类型
// serviceUid: 目标服务的唯一ID(要订阅哪个服务的事件)
// svc: 订阅者服务
func (eb *Bus) SubscribeSpecific(eventType int32, serviceUid string, svc inf.IListener) {
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
				e, err := eb.unmarshalEvent(msg.Data)
				if err != nil {
					log.SysLogger.Errorf("unmarshal specific event error: %v", err)
					return
				}

				eb.publishSpecific(e)
			}); err == nil {
				eb.addSub(key, subscription)
			} else {
				log.SysLogger.Errorf("subscribe specific event from nats failed, error: %v", err)
			}
		}
	}
}

// UnSubscribeSpecific 取消订阅指定服务的事件
func (eb *Bus) UnSubscribeSpecific(eventType int32, serviceUid string, svc inf.IListener) {
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
	eb.serverSubscribers[eventType][svc.GetServerId()][svc.GetPid().GetServiceUid()] = svc
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
				eb.addSub(key, subscription)
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
		if subscription, ok := eb.loadAndDelSub(key); ok {
			if err := subscription.Unsubscribe(); err != nil {
				log.SysLogger.Errorf("unsubscribe global event error: %v", err)
				//fmt.Println("unsubscribe global event error:", err)
			}
		}
	}
}

func (eb *Bus) UnSubscribeGlobal(eventType int32, svc inf.IListener) {
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

func (eb *Bus) UnSubscribeServer(eventType int32, svc inf.IListener) {
	key := eb.genKey(eb.serverPrefix, eventType, svc.GetServerId())
	eb.serverLock.Lock(key)
	defer eb.serverLock.Unlock(key)
	var needUnListen bool
	if subMap, ok := eb.serverSubscribers[eventType]; ok {
		if nameMap, ok := subMap[svc.GetServerId()]; ok {
			delete(nameMap, svc.GetPid().GetServiceUid())
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

// === 新增的管理和监控方法 ===

// GetEventMetrics 获取事件指标
func (eb *Bus) GetEventMetrics() *EventMetrics {
	return &EventMetrics{
		TotalPublished:   atomic.LoadInt64(&eb.metrics.TotalPublished),
		TotalDelivered:   atomic.LoadInt64(&eb.metrics.TotalDelivered),
		TotalThrottled:   atomic.LoadInt64(&eb.metrics.TotalThrottled),
		TotalBatched:     atomic.LoadInt64(&eb.metrics.TotalBatched),
		AvgDeliveryTime:  atomic.LoadInt64(&eb.metrics.AvgDeliveryTime),
		PeakEventRate:    atomic.LoadInt64(&eb.metrics.PeakEventRate),
		CurrentEventRate: atomic.LoadInt64(&eb.metrics.CurrentEventRate),
		LastEventTime:    atomic.LoadInt64(&eb.metrics.LastEventTime),
	}
}

// GetThrottleStats 获取限流统计
func (eb *Bus) GetThrottleStats() map[int32]*LimiterStats {
	return eb.throttleManager.GetAllStats()
}

// RegisterCustomEventType 注册自定义事件类型
func (eb *Bus) RegisterCustomEventType(classification *EventClassification) {
	eb.eventRegistry.RegisterClassification(classification)
}

// ResetThrottle 重置指定事件类型的限流器
func (eb *Bus) ResetThrottle(eventType int32) {
	eb.throttleManager.Reset(eventType)
}

// GetBufferedEventCount 获取缓冲区中的事件数量
func (eb *Bus) GetBufferedEventCount() map[int32]int {
	eb.bufferMutex.RLock()
	defer eb.bufferMutex.RUnlock()

	counts := make(map[int32]int)
	for eventType, events := range eb.eventBuffer {
		counts[eventType] = len(events)
	}
	return counts
}
