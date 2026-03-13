// Package eventBus
// @Title  全局事件服务
// @Description  desc
// @Author  yr  2025/4/10
// @Update  yr  2025/4/10
package event

import (
	"context"
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
	log.ILoggerX // 持有 ILoggerX

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

func (eb *Bus) Init(conf *config.EventBusConf, logger log.ILoggerX) error {
	eb.ILoggerX = logger
	// 初始化事件分类和限流系统
	eb.eventRegistry = NewEventRegistry()
	eb.throttleManager = NewThrottleManager(eb.eventRegistry)
	eb.eventBuffer = make(map[def.EventType][]*busEvent)
	eb.metrics = &EventMetrics{}

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

	// 始终启动批处理定时器，确保非 NATS 模式下缓冲事件也能被 flush
	eb.batchStop = make(chan struct{})
	eb.batchTicker = time.NewTicker(100 * time.Millisecond)
	go eb.processBatchedEvents()

	return nil
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
