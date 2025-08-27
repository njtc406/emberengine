// Package mailbox
// @Title  多级优先级队列使用示例
// @Description  展示如何配置和使用多级优先级队列功能
// @Author  EmberEngine Team
// @Update  2025/8/27
package mailbox

import (
	"github.com/njtc406/emberengine/engine/pkg/config"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

// ======== 使用示例 ========

// ExampleBasicMultiLevel 基础多级优先级队列示例
func ExampleBasicMultiLevel() {
	// 1. 创建自定义多级优先级配置
	priorities := []PriorityConfig{
		{Level: PriorityUrgent, BatchSize: 50, Weight: 10},   // 紧急任务：大批量，高权重
		{Level: PriorityHigh, BatchSize: 30, Weight: 7},      // 高优先级：中批量，中高权重
		{Level: PriorityNormal, BatchSize: 20, Weight: 5},    // 普通任务：标准批量
		{Level: PriorityLow, BatchSize: 10, Weight: 3},       // 低优先级：小批量
		{Level: PriorityBackground, BatchSize: 5, Weight: 1}, // 后台任务：最小批量
	}

	// 2. 创建使用加权轮询策略的配置
	workerConfig := CreateMultiLevelConfig(StrategyWeighted, priorities)

	// 3. 创建WorkerPool并应用配置
	conf := &config.WorkerConf{
		WorkerNum:    4,
		MaxWorkerNum: 8,
	}

	// 假设有一个消息处理器 invoker
	var invoker inf.IMessageInvoker // 需要实际的实现
	pool := NewWorkerPool(conf, invoker)
	pool.SetWorkerConfig(workerConfig) // 在Start之前设置配置

	// 4. 启动WorkerPool
	pool.Start()
	defer pool.Stop()

	// 5. 现在可以通过不同优先级提交事件
	// var event inf.IEvent // 需要实际的事件实现
	// worker := pool.workers[0] // 获取第一个worker进行演示
	// worker.SubmitEventWithPriority(event, PriorityUrgent)   // 紧急任务
	// worker.SubmitEventWithPriority(event, PriorityHigh)     // 高优先级任务
	// worker.SubmitEventWithPriority(event, PriorityNormal)   // 普通任务
}

// ExampleDefaultMultiLevel 使用默认多级优先级配置示例
func ExampleDefaultMultiLevel() {
	// 1. 直接使用默认配置（推荐用于大多数场景）
	workerConfig := CreateDefaultMultiLevelConfig()

	// 2. 创建WorkerPool
	conf := &config.WorkerConf{
		WorkerNum:    2,
		MaxWorkerNum: 4,
	}

	var invoker inf.IMessageInvoker
	pool := NewWorkerPool(conf, invoker)
	pool.SetWorkerConfig(workerConfig)

	pool.Start()
	defer pool.Stop()
}

// ExampleAbsolutePriority 绝对优先级策略示例
func ExampleAbsolutePriority() {
	// 1. 创建绝对优先级配置（高优先级完全阻塞低优先级）
	priorities := []PriorityConfig{
		{Level: PriorityUrgent, BatchSize: 100}, // 紧急任务：大批量处理
		{Level: PriorityHigh, BatchSize: 50},    // 高优先级：中批量处理
		{Level: PriorityNormal, BatchSize: 20},  // 普通任务：标准批量
		{Level: PriorityLow, BatchSize: 10},     // 低优先级：小批量处理
	}

	workerConfig := CreateAbsolutePriorityConfig(priorities)

	// 其余设置同上...
	_ = workerConfig // 示例中避免未使用变量警告
}

// ExampleFairnessPriority 防饥饿策略示例
func ExampleFairnessPriority() {
	// 1. 创建防饥饿配置（确保每个优先级都有处理机会）
	priorities := []PriorityConfig{
		{Level: PriorityHigh, BatchSize: 30},   // 高优先级
		{Level: PriorityNormal, BatchSize: 20}, // 普通任务
		{Level: PriorityLow, BatchSize: 15},    // 低优先级
		{Level: PriorityBatch, BatchSize: 10},  // 批量处理
	}

	workerConfig := CreateFairnessPriorityConfig(priorities)

	// 其余设置同上...
	_ = workerConfig // 示例中避免未使用变量警告
}

// ExampleMigrationFromLegacy 从传统高低优先级迁移示例
func ExampleMigrationFromLegacy() {
	// 原有代码：
	// worker.submitHighPriEvent(event) // 高优先级
	// worker.submitLowPriEvent(event)  // 低优先级

	// 迁移后的代码：
	// 方式1：直接使用预定义常量
	// worker.SubmitEventWithPriority(event, PriorityHigh) // 对应原高优先级
	// worker.SubmitEventWithPriority(event, PriorityLow)  // 对应原低优先级

	// 方式2：如果不启用多级队列，新API会自动回退到传统模式
	workerConfig := DefaultWorkerConfig() // 不启用多级队列

	conf := &config.WorkerConf{WorkerNum: 1}
	var invoker inf.IMessageInvoker
	pool := NewWorkerPool(conf, invoker)
	pool.SetWorkerConfig(workerConfig)

	// 这种情况下，SubmitEventWithPriority会自动回退到submitHighPriEvent/submitLowPriEvent
}

// ======== 配置建议 ========

// RecommendedConfigForGameServer 游戏服务器推荐配置
func RecommendedConfigForGameServer() *WorkerConfig {
	priorities := []PriorityConfig{
		{Level: PriorityUrgent, BatchSize: 100, Weight: 20},   // 系统关键消息：100/批次，高权重
		{Level: PriorityHigh, BatchSize: 50, Weight: 10},      // 战斗相关：50/批次，中高权重
		{Level: PriorityNormal, BatchSize: 30, Weight: 6},     // 玩家操作：30/批次，正常权重
		{Level: PriorityLow, BatchSize: 20, Weight: 3},        // 聊天消息：20/批次，低权重
		{Level: PriorityBackground, BatchSize: 10, Weight: 1}, // 数据统计：10/批次，最低权重
	}
	return CreateMultiLevelConfig(StrategyWeighted, priorities)
}

// RecommendedConfigForWebServer Web服务器推荐配置
func RecommendedConfigForWebServer() *WorkerConfig {
	priorities := []PriorityConfig{
		{Level: PriorityUrgent, BatchSize: 50, Weight: 15}, // API限流：50/批次
		{Level: PriorityHigh, BatchSize: 40, Weight: 10},   // 用户请求：40/批次
		{Level: PriorityNormal, BatchSize: 30, Weight: 6},  // 后台任务：30/批次
		{Level: PriorityLow, BatchSize: 20, Weight: 3},     // 日志处理：20/批次
		{Level: PriorityBatch, BatchSize: 100, Weight: 1},  // 批量数据：100/批次，但低权重
	}
	return CreateMultiLevelConfig(StrategyWeighted, priorities)
}

// RecommendedConfigForRealtime 实时系统推荐配置
func RecommendedConfigForRealtime() *WorkerConfig {
	priorities := []PriorityConfig{
		{Level: PriorityUrgent, BatchSize: 1},  // 实时消息：单个处理，绝对优先
		{Level: PriorityHigh, BatchSize: 5},    // 高优先级：小批量
		{Level: PriorityNormal, BatchSize: 10}, // 普通消息：标准批量
		{Level: PriorityLow, BatchSize: 20},    // 低优先级：大批量补偿
	}
	return CreateAbsolutePriorityConfig(priorities) // 使用绝对优先策略
}
