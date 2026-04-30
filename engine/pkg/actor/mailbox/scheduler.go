// Package mailbox
// 多优先级队列调度器配置与调度策略实现。
// 作者:  yr  2025/8/28 0028 0:28
// 最后更新:  yr  2025/8/28 0028 0:28
package mailbox

import (
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

// DefaultMultiLevelQueueConf 返回默认的多优先级队列配置
func DefaultMultiLevelQueueConf() *config.MultiLevelQueueConf {
	return &config.MultiLevelQueueConf{
		Strategy:        def.StrategyAbsolute,
		TotalBatchLimit: 32,
		PriorityBatches: newDefaultPriorityMap(),
	}
}

func newDefaultMultiLevelConfig() *MultiLevelConfig {
	return &MultiLevelConfig{
		Enabled:    true,
		Strategy:   def.StrategyAbsolute,
		Priorities: newDefaultPriorityMap(),
	}
}

// DefaultWorkerConfig 返回多优先级队列的默认 worker 配置。
func DefaultWorkerConfig() *config.MultiLevelWorkerConf {
	return &config.MultiLevelWorkerConf{
		WaitMode:        "busy",
		Strategy:        def.StrategyAbsolute,
		TotalBatchLimit: 32,
		PriorityBatches: newDefaultPriorityMap(),
	}
}

// PriorityScheduler 多级优先级调度器
//
// NOTE: PriorityScheduler 不是并发安全的。
// counters map 由单个 Worker goroutine 独占使用（每个 Worker 拥有独立的 PriorityQueueManager/Scheduler），
// 不需要额外的锁或 atomic 保护；该调度器的并发边界是单 Worker 独占。
type PriorityScheduler struct {
	strategy   def.ScheduleStrategy
	priorities map[def.Priority]*config.PriorityConfig
	weights    map[def.Priority]int
	counters   map[def.Priority]int // 用于加权轮询和防饥饿

	// weighted/fairness 策略下"相同最高优先级"集合的复用 buffer：
	// 避免每次 NextJob 调用重复 allocate samePriorityQueues。
	// 单 worker 独占 scheduler，无需锁保护。
	samePriorityBuf []def.Priority
}

// NewPriorityScheduler 创建新的优先级调度器
func NewPriorityScheduler(conf *config.MultiLevelWorkerConf) *PriorityScheduler {
	scheduler := &PriorityScheduler{
		strategy:        conf.Strategy,
		priorities:      conf.PriorityBatches,
		weights:         make(map[def.Priority]int),
		counters:        make(map[def.Priority]int),
		samePriorityBuf: make([]def.Priority, 0, len(conf.PriorityBatches)),
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

	// 复用预分配 buffer，避免 per-NextJob 分配
	samePriorityQueues := ps.samePriorityBuf[:0]
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

	// 复用预分配 buffer，避免 per-NextJob 分配
	samePriorityQueues := ps.samePriorityBuf[:0]
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
