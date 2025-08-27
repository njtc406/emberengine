// Package mailbox
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/8/28 0028 0:28
// 最后更新:  yr  2025/8/28 0028 0:28
package mailbox

import (
	"github.com/njtc406/emberengine/engine/pkg/def"
	"sync"
)

// PriorityConfig 单个优先级配置
type PriorityConfig struct {
	Level     def.Priority `json:"level"`      // 优先级级别（数字越小优先级越高）
	BatchSize int          `json:"batch_size"` // 该级别批量处理大小
	Weight    int          `json:"weight"`     // 调度权重（用于加权策略）
}

// MultiLevelConfig 多级队列配置
type MultiLevelConfig struct {
	Enabled    bool                 `json:"enabled"`    // 是否启用多级队列
	Priorities []PriorityConfig     `json:"priorities"` // 优先级配置列表
	Strategy   def.ScheduleStrategy `json:"strategy"`   // 调度策略
}

// WorkerConfig Worker配置参数
type WorkerConfig struct {
	// 简单模式配置（向后兼容）
	HighPriBatch int // 高优先级消息批量大小，默认16
	LowPriBatch  int // 低优先级消息批量大小，默认8

	// 多级队列配置（可选）
	MultiLevel *MultiLevelConfig `json:"multi_level,omitempty"`
}

// DefaultWorkerConfig 返回默认配置
func DefaultWorkerConfig() *WorkerConfig {
	return &WorkerConfig{
		HighPriBatch: 16,
		LowPriBatch:  8,
	}
}

// PriorityScheduler 多级优先级调度器
type PriorityScheduler struct {
	strategy   def.ScheduleStrategy
	priorities []PriorityConfig
	weights    map[def.Priority]int
	counters   map[def.Priority]int // 用于加权轮询和防饥饿
	mutex      sync.RWMutex
}

// NewPriorityScheduler 创建新的优先级调度器
func NewPriorityScheduler(config *MultiLevelConfig) *PriorityScheduler {
	if config == nil || !config.Enabled {
		return nil
	}

	scheduler := &PriorityScheduler{
		strategy:   config.Strategy,
		priorities: config.Priorities,
		weights:    make(map[def.Priority]int),
		counters:   make(map[def.Priority]int),
	}

	// 初始化权重映射
	for _, pc := range config.Priorities {
		scheduler.weights[pc.Level] = pc.Weight
		scheduler.counters[pc.Level] = 0
	}

	return scheduler
}

// NextPriority 根据调度策略返回下一个应该处理的优先级
func (ps *PriorityScheduler) NextPriority(availablePriorities []def.Priority) def.Priority {
	if ps == nil || len(availablePriorities) == 0 {
		return -1
	}

	ps.mutex.Lock()
	defer ps.mutex.Unlock()

	switch ps.strategy {
	case def.StrategyAbsolute:
		return ps.absolutePriority(availablePriorities)
	case def.StrategyWeighted:
		return ps.weightedPriority(availablePriorities)
	case def.StrategyFairness:
		return ps.fairnessPriority(availablePriorities)
	default:
		return ps.absolutePriority(availablePriorities)
	}
}

// absolutePriority 绝对优先策略：总是选择数值最小的优先级
func (ps *PriorityScheduler) absolutePriority(available []def.Priority) def.Priority {
	minPriority := available[0]
	for _, p := range available[1:] {
		if p < minPriority {
			minPriority = p
		}
	}
	return minPriority
}

// weightedPriority 加权轮询策略：根据权重分配处理机会
func (ps *PriorityScheduler) weightedPriority(available []def.Priority) def.Priority {
	// 检查并重置计数器（防止溢出）
	ps.resetCountersIfNeeded()

	// 找到计数器值最小且有权重的优先级
	var selectedPriority def.Priority = -1
	minRatio := float64(1<<63 - 1)

	for _, p := range available {
		weight, exists := ps.weights[p]
		if !exists || weight <= 0 {
			continue
		}

		counter := ps.counters[p]
		ratio := float64(counter) / float64(weight)

		if selectedPriority == -1 || ratio < minRatio {
			minRatio = ratio
			selectedPriority = p
		}
	}

	if selectedPriority != -1 {
		ps.counters[selectedPriority]++
	}

	return selectedPriority
}

// fairnessPriority 防饥饿策略：确保每个优先级都有处理机会
func (ps *PriorityScheduler) fairnessPriority(available []def.Priority) def.Priority {
	// 检查并重置计数器（防止溢出）
	ps.resetCountersIfNeeded()

	// 找到计数器值最小的优先级
	var selectedPriority def.Priority = -1
	minCounter := 1<<63 - 1

	for _, p := range available {
		counter := ps.counters[p]
		if selectedPriority == -1 || counter < minCounter {
			minCounter = counter
			selectedPriority = p
		}
	}

	if selectedPriority != -1 {
		ps.counters[selectedPriority]++
	}

	return selectedPriority
}

// resetCountersIfNeeded 检查并重置计数器（防止溢出）
func (ps *PriorityScheduler) resetCountersIfNeeded() {
	// 检查是否有计数器超过阈值
	const resetThreshold = 1e9
	maxCounter := 0

	for _, counter := range ps.counters {
		if counter > maxCounter {
			maxCounter = counter
		}
	}

	// 如果最大计数器超过阈值，就统一归一化
	if maxCounter > resetThreshold {
		for p := range ps.counters {
			ps.counters[p] = ps.counters[p] / 2
		}
	}
}
