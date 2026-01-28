// Package event
// @Title  Fine-grained Event Categorization System
// @Description  Provides detailed event classification to prevent storms and improve routing efficiency
// @Author  AI Assistant  2025/8/28
// @Update  AI Assistant  2025/8/28
package event

import (
	"sync"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/utils/timelib"
)

// EventCategory 事件分类
type EventCategory int

const (
	// 系统级事件 (最高优先级)
	CategorySystemCritical EventCategory = iota // 系统关键事件
	CategorySystemHealth                        // 系统健康检查
	CategorySystemConfig                        // 系统配置变更

	// 集群级事件
	CategoryClusterTopology  // 集群拓扑变化
	CategoryClusterDiscovery // 服务发现事件
	CategoryClusterLeader    // 主从选举事件

	// 业务级事件
	CategoryBusinessCritical // 业务关键事件
	CategoryBusinessNormal   // 业务普通事件
	CategoryBusinessBatch    // 业务批处理事件

	// 统计监控事件 (最低优先级)
	CategoryMetrics     // 性能指标事件
	CategoryStatistics  // 统计信息事件
	CategoryDiagnostics // 诊断事件
)

// EventScope 事件范围
type EventScope int

const (
	ScopeGlobal  EventScope = iota // 全局广播
	ScopeRegion                    // 区域广播
	ScopeCluster                   // 集群广播
	ScopeNode                      // 节点内广播
	ScopeService                   // 服务间广播
	ScopeLocal                     // 本地事件
)

// EventDeliveryMode 事件投递模式
type EventDeliveryMode int

const (
	DeliveryBroadcast   EventDeliveryMode = iota // 广播模式
	DeliveryMulticast                            // 组播模式
	DeliveryUnicast                              // 单播模式
	DeliveryRoundRobin                           // 轮询模式
	DeliveryLoadBalance                          // 负载均衡模式
)

// EventClassification 事件分类配置
type EventClassification struct {
	EventType    def.EventType     `json:"event_type"`    // 事件类型
	Category     EventCategory     `json:"category"`      // 事件分类
	Priority     def.Priority      `json:"priority"`      // 事件优先级
	Scope        EventScope        `json:"scope"`         // 事件范围
	DeliveryMode EventDeliveryMode `json:"delivery_mode"` // 投递模式
	MaxFrequency int               `json:"max_frequency"` // 最大频率 (次/秒)
	BatchSize    int               `json:"batch_size"`    // 批处理大小
	TTL          int               `json:"ttl"`           // 生存时间(秒)
	Tags         []string          `json:"tags"`          // 事件标签
}

// EventRegistry 事件注册表
type EventRegistry struct {
	mu              sync.RWMutex
	classifications map[def.EventType]*EventClassification
	categoryStats   map[EventCategory]*CategoryStats
}

// CategoryStats 分类统计信息
type CategoryStats struct {
	TotalEvents     int64 `json:"total_events"`
	ProcessedEvents int64 `json:"processed_events"`
	DroppedEvents   int64 `json:"dropped_events"`
	AvgLatency      int64 `json:"avg_latency_ns"`
	LastEventTime   int64 `json:"last_event_time"`
}

// 预定义的事件分类规则
var defaultClassifications = map[def.EventType]*EventClassification{
	// 系统关键事件
	SysEventServiceClose: {
		EventType:    SysEventServiceClose,
		Category:     CategorySystemCritical,
		Priority:     def.PriorityUrgent,
		Scope:        ScopeCluster,
		DeliveryMode: DeliveryBroadcast,
		MaxFrequency: 1000,
		BatchSize:    1,
		TTL:          60,
		Tags:         []string{"system", "critical", "lifecycle"},
	},
	ServiceNew: {
		EventType:    ServiceNew,
		Category:     CategorySystemCritical,
		Priority:     def.PriorityUrgent,
		Scope:        ScopeCluster,
		DeliveryMode: DeliveryBroadcast,
		MaxFrequency: 500,
		BatchSize:    1,
		TTL:          60,
		Tags:         []string{"system", "critical", "lifecycle"},
	},

	// 服务发现事件
	SysEventServiceReg: {
		EventType:    SysEventServiceReg,
		Category:     CategoryClusterDiscovery,
		Priority:     def.PriorityHigh,
		Scope:        ScopeCluster,
		DeliveryMode: DeliveryMulticast,
		MaxFrequency: 100,
		BatchSize:    5,
		TTL:          30,
		Tags:         []string{"cluster", "discovery", "registration"},
	},
	SysEventServiceDis: {
		EventType:    SysEventServiceDis,
		Category:     CategoryClusterDiscovery,
		Priority:     def.PriorityHigh,
		Scope:        ScopeCluster,
		DeliveryMode: DeliveryMulticast,
		MaxFrequency: 100,
		BatchSize:    5,
		TTL:          30,
		Tags:         []string{"cluster", "discovery", "deregistration"},
	},

	// 主从选举事件
	ServiceBecomeMaster: {
		EventType:    ServiceBecomeMaster,
		Category:     CategoryClusterLeader,
		Priority:     def.PriorityHigh,
		Scope:        ScopeCluster,
		DeliveryMode: DeliveryBroadcast,
		MaxFrequency: 10,
		BatchSize:    1,
		TTL:          120,
		Tags:         []string{"cluster", "leadership", "election"},
	},
	ServiceLoseMaster: {
		EventType:    ServiceLoseMaster,
		Category:     CategoryClusterLeader,
		Priority:     def.PriorityHigh,
		Scope:        ScopeCluster,
		DeliveryMode: DeliveryBroadcast,
		MaxFrequency: 10,
		BatchSize:    1,
		TTL:          120,
		Tags:         []string{"cluster", "leadership", "election"},
	},

	// 心跳事件 (统计类)
	ServiceHeartbeat: {
		EventType:    ServiceHeartbeat,
		Category:     CategoryMetrics,
		Priority:     def.PriorityBackground,
		Scope:        ScopeLocal,
		DeliveryMode: DeliveryUnicast,
		MaxFrequency: 1,
		BatchSize:    10,
		TTL:          5,
		Tags:         []string{"metrics", "heartbeat", "health"},
	},

	// 连接事件
	SysEventNodeConn: {
		EventType:    SysEventNodeConn,
		Category:     CategorySystemHealth,
		Priority:     def.PriorityNormal,
		Scope:        ScopeNode,
		DeliveryMode: DeliveryMulticast,
		MaxFrequency: 50,
		BatchSize:    3,
		TTL:          15,
		Tags:         []string{"system", "connection", "health"},
	},
}

// NewEventRegistry 创建事件注册表
func NewEventRegistry() *EventRegistry {
	registry := &EventRegistry{
		classifications: make(map[def.EventType]*EventClassification),
		categoryStats:   make(map[EventCategory]*CategoryStats),
	}

	// 初始化预定义分类
	for eventType, classification := range defaultClassifications {
		registry.classifications[eventType] = classification
	}

	// 初始化分类统计
	for category := CategorySystemCritical; category <= CategoryDiagnostics; category++ {
		registry.categoryStats[category] = &CategoryStats{}
	}

	return registry
}

// GetClassification 获取事件分类信息
func (r *EventRegistry) GetClassification(eventType def.EventType) *EventClassification {
	r.mu.RLock()
	if classification, exists := r.classifications[eventType]; exists {
		r.mu.RUnlock()
		return classification
	}
	r.mu.RUnlock()

	// 返回默认分类
	return &EventClassification{
		EventType:    eventType,
		Category:     CategoryBusinessNormal,
		Priority:     def.PriorityNormal,
		Scope:        ScopeService,
		DeliveryMode: DeliveryBroadcast,
		MaxFrequency: 100,
		BatchSize:    5,
		TTL:          30,
		Tags:         []string{"business", "default"},
	}
}

// RegisterClassification 注册自定义事件分类
func (r *EventRegistry) RegisterClassification(classification *EventClassification) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.classifications[classification.EventType] = classification
}

// UpdateCategoryStats 更新分类统计信息
func (r *EventRegistry) UpdateCategoryStats(category EventCategory, processed bool, latency int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if stats, exists := r.categoryStats[category]; exists {
		stats.TotalEvents++
		if processed {
			stats.ProcessedEvents++
		} else {
			stats.DroppedEvents++
		}

		// 计算平均延迟 (简单移动平均)
		if stats.ProcessedEvents > 0 {
			stats.AvgLatency = (stats.AvgLatency + latency) / 2
		}

		stats.LastEventTime = timelib.Now().Unix()
	}
}

// GetCategoryStats 获取分类统计信息
func (r *EventRegistry) GetCategoryStats(category EventCategory) *CategoryStats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if stats, exists := r.categoryStats[category]; exists {
		return &CategoryStats{
			TotalEvents:     stats.TotalEvents,
			ProcessedEvents: stats.ProcessedEvents,
			DroppedEvents:   stats.DroppedEvents,
			AvgLatency:      stats.AvgLatency,
			LastEventTime:   stats.LastEventTime,
		}
	}
	return nil
}

// ShouldThrottle 检查是否应该限流
func (r *EventRegistry) ShouldThrottle(eventType def.EventType, currentFrequency int) bool {
	classification := r.GetClassification(eventType)
	return currentFrequency > classification.MaxFrequency
}

// GetBatchSize 获取批处理大小
func (r *EventRegistry) GetBatchSize(eventType def.EventType) int {
	classification := r.GetClassification(eventType)
	return classification.BatchSize
}

// IsExpired 检查事件是否过期
func (r *EventRegistry) IsExpired(eventType def.EventType, timestamp int64) bool {
	classification := r.GetClassification(eventType)
	return time.Now().Unix()-timestamp > int64(classification.TTL)
}

// GetEventsByCategory 按分类获取事件类型列表
func (r *EventRegistry) GetEventsByCategory(category EventCategory) []def.EventType {
	var events []def.EventType
	for eventType, classification := range r.classifications {
		if classification.Category == category {
			events = append(events, eventType)
		}
	}
	return events
}

// GetEventsByScope 按范围获取事件类型列表
func (r *EventRegistry) GetEventsByScope(scope EventScope) []def.EventType {
	var events []def.EventType
	for eventType, classification := range r.classifications {
		if classification.Scope == scope {
			events = append(events, eventType)
		}
	}
	return events
}

// GetEventsByTags 按标签获取事件类型列表
func (r *EventRegistry) GetEventsByTags(tags []string) []def.EventType {
	var events []def.EventType
	for eventType, classification := range r.classifications {
		if r.hasAllTags(classification.Tags, tags) {
			events = append(events, eventType)
		}
	}
	return events
}

// hasAllTags 检查是否包含所有指定标签
func (r *EventRegistry) hasAllTags(eventTags, requiredTags []string) bool {
	for _, required := range requiredTags {
		found := false
		for _, tag := range eventTags {
			if tag == required {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
