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
	"github.com/njtc406/emberengine/engine/pkg/actor/mailbox/job"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/emberctx"
	"github.com/njtc406/emberengine/engine/pkg/utils/shardedlock"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
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

type Bus struct {
	*log.Logger // 嵌入 Logger（替代 log.SysLogger）

	nc     *nats.Conn // TODO 目前只支持nats,后续再看要不要扩展吧
	enable atomic.Int32

	// NATS 订阅 pending 配置（用于高突发缓冲，未配置则使用默认值）
	subPendingMsgLimit   int
	subPendingBytesLimit int

	// 全体事件(所有订阅的服务都会收到)
	globalPrefix      string                                     // 全局事件前缀
	globalLock        *shardedlock.ShardedRWLock                 // 用分段锁提升并发能力
	globalSubscribers map[def.EventType]map[string]inf.IListener // map[事件类型]map[服务唯一id]事件通道

	// 服务器事件(只有相同服务器的订阅会收到)
	serverPrefix      string // 服务器事件前缀
	serverLock        *shardedlock.ShardedRWLock
	serverSubscribers map[def.EventType]map[int32]map[string]inf.IListener // map[事件类型]map[服务器id]map[服务唯一id]事件通道

	// 特定事件(只有订阅者会收到)
	specificPrefix      string // 特定事件前缀
	specificLock        *shardedlock.ShardedRWLock
	specificSubscribers map[def.EventType]map[string]map[string]inf.IListener // map[事件类型]map[目标服务唯一id]map[订阅者服务唯一id]事件通道

	subMap sync.Map // 记录所有订阅 map[string]*nats.Subscription

	// 新增的事件分类和限流系统
	eventRegistry   *EventRegistry                // 事件注册表
	throttleManager *ThrottleManager              // 限流管理器
	eventBuffer     map[def.EventType][]*busEvent // 事件缓冲区 (按类型批处理)
	bufferMutex     sync.RWMutex                  // 缓冲区锁
	batchTicker     *time.Ticker                  // 批处理定时器
	batchStop       chan struct{}                 // 停止批处理 goroutine
	batchStopOnce   sync.Once                     // 确保只关闭一次
	metrics         *EventMetrics                 // 事件指标
}

// NewEventBus 创建新的事件总线实例（Phase 2 per-Node 模式推荐使用）。
func NewEventBus() *Bus {
	return &Bus{}
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
			// TODO 这里有风险，后续考虑完善TLS配置选项
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

func (eb *Bus) Init(conf *config.EventBusConf, logger *log.Logger) error {
	eb.Logger = logger
	// 初始化事件分类和限流系统
	eb.eventRegistry = NewEventRegistry()
	eb.throttleManager = NewThrottleManager(eb.eventRegistry)
	eb.eventBuffer = make(map[def.EventType][]*busEvent)
	eb.metrics = &EventMetrics{}

	// 启动批处理定时器 (每100ms处理一次缓冲)
	eb.batchStop = make(chan struct{})
	eb.batchTicker = time.NewTicker(100 * time.Millisecond)
	go eb.processBatchedEvents()

	if conf != nil && conf.NatsConf != nil && len(conf.NatsConf.EndPoints) != 0 {
		opts := switchOpts(conf.NatsConf)

		nc, err := nats.Connect(strings.Join(conf.NatsConf.EndPoints, ","), opts...)
		if err != nil {
			return fmt.Errorf("nats connect failed: %w", err)
		}
		eb.nc = nc
		eb.enable.Store(1)

		// 订阅 pending 限制：配置优先，缺省使用默认值
		eb.subPendingMsgLimit = conf.NatsConf.SubPendingMsgLimit
		eb.subPendingBytesLimit = conf.NatsConf.SubPendingBytesLimit
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
		eb.Debug("==========> nats init success")
	}

	var shardCount = def.NatsDefaultShardCount
	if conf != nil && conf.ShardCount > 0 {
		shardCount = conf.ShardCount
	}

	eb.globalLock = shardedlock.NewShardedRWLock(shardCount)
	eb.globalSubscribers = make(map[def.EventType]map[string]inf.IListener)
	eb.serverLock = shardedlock.NewShardedRWLock(shardCount)
	eb.serverSubscribers = make(map[def.EventType]map[int32]map[string]inf.IListener)
	eb.specificLock = shardedlock.NewShardedRWLock(shardCount)
	eb.specificSubscribers = make(map[def.EventType]map[string]map[string]inf.IListener)
	return nil
}

func (eb *Bus) applySubPendingLimits(subscription *nats.Subscription) {
	if subscription == nil {
		return
	}
	msgLimit := eb.subPendingMsgLimit
	bytesLimit := eb.subPendingBytesLimit
	if msgLimit <= 0 {
		msgLimit = def.NatsDefaultSubPendingMsgLimit
	}
	if bytesLimit <= 0 {
		bytesLimit = def.NatsDefaultSubPendingBytesLimit
	}
	_ = subscription.SetPendingLimits(msgLimit, bytesLimit)
}

func (eb *Bus) Stop() {
	if eb.nc != nil && eb.enable.CompareAndSwap(1, 0) {
		eb.nc.Close()
		eb.nc = nil
	}

	// 停止批处理 goroutine + 定时器
	eb.batchStopOnce.Do(func() {
		if eb.batchStop != nil {
			close(eb.batchStop)
		}
	})
	if eb.batchTicker != nil {
		eb.batchTicker.Stop()
	}

	// 处理剩余的缓冲事件
	eb.flushAllBuffers()
}

// processBatchedEvents 处理批量事件
func (eb *Bus) processBatchedEvents() {
	for {
		select {
		case <-eb.batchTicker.C:
			eb.flushAllBuffers()
		case <-eb.batchStop:
			return
		}
	}
}

// flushAllBuffers 刷新所有缓冲区
func (eb *Bus) flushAllBuffers() {
	// 注意：不要在持有 bufferMutex 时执行 publish/post job，避免阻塞批处理和生产者。
	eb.bufferMutex.Lock()
	if len(eb.eventBuffer) == 0 {
		eb.bufferMutex.Unlock()
		return
	}
	batches := make(map[def.EventType][]*busEvent, len(eb.eventBuffer))
	for eventType, events := range eb.eventBuffer {
		if len(events) > 0 {
			batches[eventType] = events
		}
	}
	// 直接替换 map，避免遍历时 delete
	eb.eventBuffer = make(map[def.EventType][]*busEvent)
	eb.bufferMutex.Unlock()

	for eventType, events := range batches {
		eb.flushEventBatch(eventType, events)
	}
}

// flushEventBatch 刷新指定类型的事件批量
func (eb *Bus) flushEventBatch(eventType def.EventType, events []*busEvent) {
	if len(events) == 0 {
		return
	}

	classification := eb.eventRegistry.GetClassification(eventType)

	// 按照事件范围选择批量处理策略
	switch classification.Scope {
	case ScopeGlobal:
		for _, be := range events {
			eb.publishGlobal(be.ctx, be.event)
		}
	case ScopeCluster, ScopeRegion, ScopeNode:
		for _, be := range events {
			eb.publishServer(be.ctx, be.event)
		}
	default:
		// 本地事件直接处理
		for _, be := range events {
			eb.publishGlobal(be.ctx, be.event) // 默认作为全局事件处理
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

// marshalEvent 将数据封装为 actor.Event，返回 ctx 和 event 分离的形式
func (eb *Bus) marshalEvent(ctx context.Context, eventType def.EventType, partition int32, serviceUid string, data proto.Message) (*busEvent, error) {
	// 组装数据
	rawData, err := anypb.New(data)
	if err != nil {
		return nil, err
	}

	// 确保有 DispatcherKey
	if dispatcherKey, _ := emberctx.GetHeaderValue(ctx, def.DefaultDispatcherKey).(string); dispatcherKey == "" {
		ctx = emberctx.AddHeader(ctx, def.DefaultDispatcherKey, uuid.NewString())
	}

	// actor.Event 作为纯数据载体
	// Priority 和 DispatcherKey 是显式字段
	// ContextHeaders 仅用于 tracing/metadata
	dispatcherKey, _ := emberctx.GetHeaderValue(ctx, def.DefaultDispatcherKey).(string)
	e := &actor.Event{
		Type:           int32(eventType),
		Priority:       int32(def.PriorityNormal), // 默认优先级，可由调用方覆盖
		DispatcherKey:  dispatcherKey,
		Partition:      partition,
		ServiceUid:     serviceUid,
		Payload:        rawData,
		ContextHeaders: emberctx.ToHeaders(ctx),
	}

	return newBusEvent(ctx, e), nil
}

func (eb *Bus) isNatsEnabled() bool {
	return eb.enable.Load() == 1
}

// unmarshalEvent 反序列化事件，从 ContextHeaders 重建 context
func (eb *Bus) unmarshalEvent(eventData []byte) (*busEvent, error) {
	e := &actor.Event{}
	if err := proto.Unmarshal(eventData, e); err != nil {
		return nil, err
	}
	// 从 ContextHeaders 重建 context
	ctx := buildContextFromHeaders(e.ContextHeaders)
	// 从显式字段恢复调度信息到 context
	if e.DispatcherKey != "" {
		ctx = emberctx.AddHeader(ctx, def.DefaultDispatcherKey, e.DispatcherKey)
	}
	if e.Priority != 0 {
		ctx = emberctx.AddHeader(ctx, def.DefaultPriorityKey, def.Priority(e.Priority))
	}
	return newBusEvent(ctx, e), nil
}

// TODO 全局事件这里可以考虑订阅指定服务的事件，比如当处于某个场景服时，可以只订阅该场景服的事件，就可以实现广播功能，可以通过广播减少rpc寻址调用
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
	if classification.BatchSize > 1 &&
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
				if err := ch.PostJob(j); err != nil {
					eb.Errorf("push server event error: %v", err)
					j.Release()
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

				if err := ch.PostJob(j); err != nil {
					eb.Errorf("push specific event error: %v", err)
					j.Release()
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

func (eb *Bus) unSubscribe(key string) {
	// 没有订阅者了,那么取消监听
	if eb.isNatsEnabled() {
		if subscription, ok := eb.loadAndDelSub(key); ok {
			if err := subscription.Unsubscribe(); err != nil {
				eb.Errorf("unsubscribe global event error: %v", err)
				//fmt.Println("unsubscribe global event error:", err)
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
func (eb *Bus) GetThrottleStats() map[def.EventType]*LimiterStats {
	return eb.throttleManager.GetAllStats()
}

// RegisterCustomEventType 注册自定义事件类型
func (eb *Bus) RegisterCustomEventType(classification *EventClassification) {
	eb.eventRegistry.RegisterClassification(classification)
}

// ResetThrottle 重置指定事件类型的限流器
func (eb *Bus) ResetThrottle(eventType def.EventType) {
	eb.throttleManager.Reset(eventType)
}

// GetBufferedEventCount 获取缓冲区中的事件数量
func (eb *Bus) GetBufferedEventCount() map[def.EventType]int {
	eb.bufferMutex.RLock()
	defer eb.bufferMutex.RUnlock()

	counts := make(map[def.EventType]int)
	for eventType, events := range eb.eventBuffer {
		counts[eventType] = len(events)
	}
	return counts
}
