// Package mailbox
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/8/28 0028 0:28
// 最后更新:  yr  2025/8/28 0028 0:28
package mailbox

import (
	"sync"

	"github.com/njtc406/emberengine/engine/pkg/config"

	"github.com/njtc406/emberengine/engine/pkg/def"
)

// MultiLevelConfig 多级队列配置
type MultiLevelConfig struct {
	Enabled    bool                                    `json:"enabled"`    // 是否启用多级队列
	Priorities map[def.Priority]*config.PriorityConfig `json:"priorities"` // 优先级配置列表
	Strategy   def.ScheduleStrategy                    `json:"strategy"`   // 调度策略
}

func newDefaultPriorityMap() map[def.Priority]*config.PriorityConfig {
	return map[def.Priority]*config.PriorityConfig{
		def.PrioritySys:    {BatchSize: 64, Weight: 20},
		def.PriorityUrgent: {BatchSize: 32, Weight: 10},
		def.PriorityHigh:   {BatchSize: 16, Weight: 5},
		def.PriorityNormal: {BatchSize: 8, Weight: 3},
		def.PriorityLow:    {BatchSize: 4, Weight: 2},
		def.PriorityBatch:  {BatchSize: 2, Weight: 1},
	}
}

func newDefaultMultiLevelConfig() *MultiLevelConfig {
	return &MultiLevelConfig{
		Enabled:    true,
		Strategy:   def.StrategyAbsolute,
		Priorities: newDefaultPriorityMap(),
	}
}

// DefaultWorkerConfig 返回默认配置
func DefaultWorkerConfig() *config.MultiLevelWorkerConf {
	return &config.MultiLevelWorkerConf{
		WaitMode:        "busy",
		Strategy:        def.StrategyAbsolute,
		TotalBatchLimit: 32,
		PriorityBatches: newDefaultPriorityMap(),
	}
}

// PriorityScheduler 多级优先级调度器
type PriorityScheduler struct {
	strategy   def.ScheduleStrategy
	priorities map[def.Priority]*config.PriorityConfig
	weights    map[def.Priority]int
	counters   map[def.Priority]int // 用于加权轮询和防饥饿
	mutex      sync.RWMutex
}

// NewPriorityScheduler 创建新的优先级调度器
func NewPriorityScheduler(conf *config.MultiLevelWorkerConf) *PriorityScheduler {
	scheduler := &PriorityScheduler{
		strategy:   conf.Strategy,
		priorities: conf.PriorityBatches,
		weights:    make(map[def.Priority]int),
		counters:   make(map[def.Priority]int),
	}

	// 初始化权重映射
	for lv, pc := range conf.PriorityBatches {
		scheduler.weights[lv] = pc.Weight
		scheduler.counters[lv] = 0
	}

	return scheduler
}

// NextPriorityWithOrdering 支持优先级排序的调度方法
// availablePriorities 已经按优先级从高到低排序（数值越小优先级越高）
// 返回值：选中的优先级，-1表示无法选择
func (ps *PriorityScheduler) NextPriorityWithOrdering(availablePriorities []def.Priority) def.Priority {
	if ps == nil || len(availablePriorities) == 0 {
		return -1
	}
	// 单线程 worker 内调用，无需加锁
	switch ps.strategy {
	case def.StrategyAbsolute:
		// 绝对优先策略：直接选择最高优先级（数组第一个元素）
		return availablePriorities[0]
	case def.StrategyWeighted:
		// 加权策略：在相同最高优先级的所有队列中进行加权选择
		return ps.weightedPriorityWithOrdering(availablePriorities)
	case def.StrategyFairness:
		// 公平策略：在相同最高优先级的所有队列中进行公平选择
		return ps.fairnessPriorityWithOrdering(availablePriorities)
	default:
		return availablePriorities[0]
	}
}

// weightedPriorityWithOrdering 支持优先级排序的加权策略
func (ps *PriorityScheduler) weightedPriorityWithOrdering(available []def.Priority) def.Priority {
	// 检查并重置计数器（防止溢出）
	ps.resetCountersIfNeeded()

	// 找到最高优先级（数组第一个元素）
	highestPriority := available[0]

	// 收集所有相同最高优先级的队列
	var samePriorityQueues []def.Priority
	for _, p := range available {
		if p == highestPriority {
			samePriorityQueues = append(samePriorityQueues, p)
		} else {
			// 由于数组已排序，遇到不同优先级就退出
			break
		}
	}

	// 如果只有一个最高优先级队列，直接返回
	if len(samePriorityQueues) == 1 {
		return samePriorityQueues[0]
	}

	// 在相同优先级的队列中进行加权选择
	var selectedPriority def.Priority = -1
	minRatio := float64(1<<63 - 1)

	for _, p := range samePriorityQueues {
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

// fairnessPriorityWithOrdering 支持优先级排序的公平策略
func (ps *PriorityScheduler) fairnessPriorityWithOrdering(available []def.Priority) def.Priority {
	// 检查并重置计数器（防止溢出）
	ps.resetCountersIfNeeded()

	// 找到最高优先级（数组第一个元素）
	highestPriority := available[0]

	// 收集所有相同最高优先级的队列
	var samePriorityQueues []def.Priority
	for _, p := range available {
		if p == highestPriority {
			samePriorityQueues = append(samePriorityQueues, p)
		} else {
			// 由于数组已排序，遇到不同优先级就退出
			break
		}
	}

	// 如果只有一个最高优先级队列，直接返回
	if len(samePriorityQueues) == 1 {
		return samePriorityQueues[0]
	}

	// 在相同优先级的队列中找到计数器最小的
	var selectedPriority def.Priority = -1
	minCounter := 1<<63 - 1

	for _, p := range samePriorityQueues {
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

// NextPriority 原有方法，保持向后兼容
func (ps *PriorityScheduler) NextPriority(availablePriorities []def.Priority) def.Priority {
	if ps == nil || len(availablePriorities) == 0 {
		return -1
	}
	// 单线程 worker 内调用，无需加锁
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
// 优化：提高阈值和使用更安全的重置策略
func (ps *PriorityScheduler) resetCountersIfNeeded() {
	// 检查是否有计数器超过阈值（提高阈值以减少重置频率）
	const resetThreshold = 1e10 // 从1e9提高到1e10
	maxCounter := 0

	for _, counter := range ps.counters {
		if counter > maxCounter {
			maxCounter = counter
		}
	}

	// 如果最大计数器超过阈值，就统一归一化
	if maxCounter > resetThreshold {
		// 使用更安全的重置策略：找到最小值，然后所有计数器减去最小值
		minCounter := maxCounter
		for _, counter := range ps.counters {
			if counter < minCounter {
				minCounter = counter
			}
		}

		// 所有计数器减去最小值，保持相对比例
		for p := range ps.counters {
			ps.counters[p] = ps.counters[p] - minCounter
		}
	}
}
