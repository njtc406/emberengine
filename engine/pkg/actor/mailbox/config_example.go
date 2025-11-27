// Package mailbox
// @Title  配置示例
// @Description  演示如何配置双队列模式和多优先级队列模式
// @Author  yr  2025/11/27
// @Update  yr  2025/11/27
package mailbox

import (
	"time"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
)

// ExampleDualQueueConfig 双队列模式配置示例
// 适用场景：简单场景，只需要区分系统消息和用户消息
func ExampleDualQueueConfig() *config.MailboxConf {
	return &config.MailboxConf{
		// 队列模式：双队列（系统队列 + 用户队列）
		QueueMode: "dual",

		SchedulePolicy: &config.WorkerSchedulePolicy{
			// Worker数量配置
			InitialWorkerNum:  4,     // 启动时创建4个Worker
			VirtualWorkerRate: 16,    // 一致性哈希虚拟节点倍率
			EnableAutoScaling: false, // 不启用自动扩缩容

			// 空闲控制配置
			IdlerConf: &config.WorkerIdlerConf{
				EnableCond:           true, // 使用条件变量等待（节省CPU）
				BackoffBaseDelay:     1 * time.Microsecond,
				BackoffMaxDelay:      16 * time.Microsecond,
				BackoffMaxRetries:    3,
				MaxIdleBeforeBackoff: 1000,
			},
		},
	}
}

// ExamplePriorityQueueConfig_Absolute 多优先级队列配置示例 - 绝对优先策略
// 适用场景：对优先级要求严格，允许低优先级消息等待
func ExamplePriorityQueueConfig_Absolute() *config.MailboxConf {
	return &config.MailboxConf{
		// 队列模式：多优先级队列
		QueueMode: "priority",

		SchedulePolicy: &config.WorkerSchedulePolicy{
			// Worker数量配置
			InitialWorkerNum:  8,     // 启动时创建8个Worker
			VirtualWorkerRate: 16,    // 一致性哈希虚拟节点倍率
			EnableAutoScaling: false, // 不启用自动扩缩容

			// 空闲控制配置
			IdlerConf: &config.WorkerIdlerConf{
				EnableCond:           false, // 使用退避策略（响应快）
				BackoffBaseDelay:     1 * time.Microsecond,
				BackoffMaxDelay:      16 * time.Microsecond,
				BackoffMaxRetries:    3,
				MaxIdleBeforeBackoff: 1000,
			},

			// 多优先级队列配置
			MultiLevelQueueConf: &config.MultiLevelQueueConf{
				// 调度策略：绝对优先（始终优先处理高优先级消息）
				Strategy: def.StrategyAbsolute,

				// 单次循环最多处理的消息总数
				TotalBatchLimit: 64,

				// 各优先级的批量处理配置
				PriorityBatches: map[def.Priority]*config.PriorityConfig{
					def.PrioritySys: {
						BatchSize: 32, // 系统消息每次最多处理32条
						Weight:    20, // 权重（在绝对优先策略下不使用）
					},
					def.PriorityUrgent: {
						BatchSize: 16,
						Weight:    10,
					},
					def.PriorityHigh: {
						BatchSize: 12,
						Weight:    5,
					},
					def.PriorityNormal: {
						BatchSize: 8,
						Weight:    3,
					},
					def.PriorityLow: {
						BatchSize: 4,
						Weight:    2,
					},
					def.PriorityBatch: {
						BatchSize: 2,
						Weight:    1,
					},
				},
			},
		},
	}
}

// ExamplePriorityQueueConfig_Weighted 多优先级队列配置示例 - 加权策略
// 适用场景：需要兼顾各优先级，按权重分配处理机会
func ExamplePriorityQueueConfig_Weighted() *config.MailboxConf {
	return &config.MailboxConf{
		// 队列模式：多优先级队列
		QueueMode: "priority",

		SchedulePolicy: &config.WorkerSchedulePolicy{
			InitialWorkerNum:  8,
			VirtualWorkerRate: 16,
			EnableAutoScaling: false,

			IdlerConf: &config.WorkerIdlerConf{
				EnableCond:           false,
				BackoffBaseDelay:     1 * time.Microsecond,
				BackoffMaxDelay:      16 * time.Microsecond,
				BackoffMaxRetries:    3,
				MaxIdleBeforeBackoff: 1000,
			},

			MultiLevelQueueConf: &config.MultiLevelQueueConf{
				// 调度策略：加权策略（按权重分配处理机会）
				Strategy: def.StrategyWeighted,

				TotalBatchLimit: 64,

				// 各优先级的批量处理配置（Weight字段生效）
				PriorityBatches: map[def.Priority]*config.PriorityConfig{
					def.PrioritySys: {
						BatchSize: 32,
						Weight:    20, // 权重最大，获得最多处理机会
					},
					def.PriorityUrgent: {
						BatchSize: 16,
						Weight:    10,
					},
					def.PriorityHigh: {
						BatchSize: 12,
						Weight:    5,
					},
					def.PriorityNormal: {
						BatchSize: 8,
						Weight:    3,
					},
					def.PriorityLow: {
						BatchSize: 4,
						Weight:    2, // 权重较小，但仍能获得处理机会
					},
					def.PriorityBatch: {
						BatchSize: 2,
						Weight:    1,
					},
				},
			},
		},
	}
}

// ExamplePriorityQueueConfig_Fairness 多优先级队列配置示例 - 公平策略
// 适用场景：防止低优先级消息饥饿，确保所有优先级都能被处理
func ExamplePriorityQueueConfig_Fairness() *config.MailboxConf {
	return &config.MailboxConf{
		// 队列模式：多优先级队列
		QueueMode: "priority",

		SchedulePolicy: &config.WorkerSchedulePolicy{
			InitialWorkerNum:  8,
			VirtualWorkerRate: 16,
			EnableAutoScaling: false,

			IdlerConf: &config.WorkerIdlerConf{
				EnableCond:           false,
				BackoffBaseDelay:     1 * time.Microsecond,
				BackoffMaxDelay:      16 * time.Microsecond,
				BackoffMaxRetries:    3,
				MaxIdleBeforeBackoff: 1000,
			},

			MultiLevelQueueConf: &config.MultiLevelQueueConf{
				// 调度策略：公平策略（确保每个优先级都有处理机会）
				Strategy: def.StrategyFairness,

				TotalBatchLimit: 64,

				// 各优先级的批量处理配置
				PriorityBatches: map[def.Priority]*config.PriorityConfig{
					def.PrioritySys: {
						BatchSize: 32,
						Weight:    20, // 在公平策略下权重不使用
					},
					def.PriorityUrgent: {
						BatchSize: 16,
						Weight:    10,
					},
					def.PriorityHigh: {
						BatchSize: 12,
						Weight:    5,
					},
					def.PriorityNormal: {
						BatchSize: 8,
						Weight:    3,
					},
					def.PriorityLow: {
						BatchSize: 4,
						Weight:    2,
					},
					def.PriorityBatch: {
						BatchSize: 2,
						Weight:    1,
					},
				},
			},
		},
	}
}

// ExampleAutoScalingConfig 自动扩缩容配置示例
// 适用场景：负载波动较大，需要动态调整Worker数量
func ExampleAutoScalingConfig() *config.MailboxConf {
	return &config.MailboxConf{
		QueueMode: "dual",

		SchedulePolicy: &config.WorkerSchedulePolicy{
			// Worker数量配置
			InitialWorkerNum:  4, // 初始4个Worker
			VirtualWorkerRate: 16,
			EnableAutoScaling: true, // 启用自动扩缩容

			// 扩缩容策略配置
			ScalingStrategy: &config.WorkerStrategyConfig{
				Name: "max_load", // 基于最大负载的策略
				Params: map[string]interface{}{
					"max_load_threshold": 100, // 队列长度超过100时扩容
					"idle_threshold":     50,  // 50%的Worker空闲时缩容
				},
				MinWorkerNum:   2,               // 最小2个Worker
				MaxWorkerNum:   16,              // 最大16个Worker
				GrowthFactor:   1.5,             // 扩容因子：当前数量 * 1.5
				ShrinkFactor:   0.75,            // 缩容因子：当前数量 * 0.75
				ResizeCoolDown: 5 * time.Second, // 调整间隔5秒
			},

			IdlerConf: &config.WorkerIdlerConf{
				EnableCond:           true,
				BackoffBaseDelay:     1 * time.Microsecond,
				BackoffMaxDelay:      16 * time.Microsecond,
				BackoffMaxRetries:    3,
				MaxIdleBeforeBackoff: 1000,
			},
		},
	}
}

// ExampleSingleWorkerConfig 单 worker（接近 Actor 模型）模式配置示例
// 适用场景：希望该 Service 在单个协程中按顺序处理消息，行为接近传统 Actor
func ExampleSingleWorkerConfig() *config.MailboxConf {
	return &config.MailboxConf{
		QueueMode: "dual", // 单 worker 场景下一般使用双队列即可满足需求

		SchedulePolicy: &config.WorkerSchedulePolicy{
			// 单个 Worker，所有消息在同一协程内顺序处理
			InitialWorkerNum:  1,
			VirtualWorkerRate: 1,     // 单 worker 时虚拟节点倍率影响较小
			EnableAutoScaling: false, // 单 worker 模式下通常不启用自动扩缩容

			IdlerConf: &config.WorkerIdlerConf{
				EnableCond:           true,
				BackoffBaseDelay:     1 * time.Microsecond,
				BackoffMaxDelay:      16 * time.Microsecond,
				BackoffMaxRetries:    3,
				MaxIdleBeforeBackoff: 1000,
			},
		},
	}
}

/* 配置说明总结：

## 队列模式选择 (QueueMode)

### 1. dual（双队列模式）
- 职责：将消息分为系统消息和用户消息两个队列
- 调度：始终优先处理系统消息
- 适用场景：
  - 简单场景，只需要区分高优先级和普通优先级
  - 不需要细粒度的优先级控制
  - 追求简单和高性能

### 2. priority（多优先级队列模式）
- 职责：支持多个优先级队列（sys/urgent/high/normal/low/batch）
- 调度：根据策略决定（absolute/weighted/fairness）
- 适用场景：
  - 需要细粒度的优先级控制
  - 不同类型的消息有不同的处理要求
  - 需要防止低优先级消息饥饿

## 调度策略选择 (Strategy)

### 1. absolute（绝对优先）
- 行为：始终优先处理高优先级消息
- 优点：高优先级消息响应最快
- 缺点：可能导致低优先级消息饥饿
- 适用：对高优先级消息延迟要求极高的场景

### 2. weighted（加权策略）
- 行为：按权重分配处理机会
- 优点：兼顾各优先级，按权重比例处理
- 缺点：配置较复杂
- 适用：需要平衡各优先级的场景

### 3. fairness（公平策略）
- 行为：确保每个优先级都有处理机会
- 优点：不会出现消息饥饿
- 缺点：高优先级消息可能等待
- 适用：所有消息都必须被处理的场景

## 参数调优建议

### BatchSize（批量大小）
- 高优先级：设置较大值（如32），提高吞吐量
- 低优先级：设置较小值（如2-4），避免阻塞高优先级

### Weight（权重）
- 仅在 weighted 策略下生效
- 高优先级：设置较大权重（如20）
- 低优先级：设置较小权重（如1-2）

### TotalBatchLimit（总批次限制）
- 控制每次循环最多处理的消息数
- 建议：32-128之间
- 过大：可能导致长时间阻塞
- 过小：降低吞吐量

### InitialWorkerNum（Worker数量）
- 单核场景：1
- 多核场景：CPU核心数的1-2倍
- 高并发场景：8-16

### VirtualWorkerRate（虚拟节点倍率）
- 推荐：10-24之间
- 过大：哈希计算开销增加
- 过小：消息分布不均匀

*/
