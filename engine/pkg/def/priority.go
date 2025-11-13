// Package def
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/8/29 0029 0:28
// 最后更新:  yr  2025/8/29 0029 0:28
package def

// Priority 优先级级别类型
type Priority int

const (
	PrioritySys        Priority = -3 // 系统优先级（最高）
	PriorityUrgent     Priority = -2 // 紧急优先级
	PriorityHigh       Priority = -1 // 高优先级
	PriorityNormal     Priority = 0  // 普通优先级
	PriorityLow        Priority = 1  // 低优先级
	PriorityBatch      Priority = 2  // 批量处理优先级
	PriorityBackground Priority = 3  // 后台优先级
)

const (
	PrioritySysStr        = "-3"
	PriorityUrgentStr     = "-2"
	PriorityHighStr       = "-1"
	PriorityNormalStr     = "0"
	PriorityLowStr        = "1"
	PriorityBatchStr      = "2"
	PriorityBackgroundStr = "3"
)

// ScheduleStrategy 调度策略
type ScheduleStrategy string

const (
	StrategyAbsolute ScheduleStrategy = "absolute" // 绝对优先策略
	StrategyWeighted ScheduleStrategy = "weighted" // 加权轮询策略
	StrategyFairness ScheduleStrategy = "fairness" // 防饥饿策略
)
