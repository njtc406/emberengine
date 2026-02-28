// Package interfaces
// INodeContext 定义了 Node 运行时上下文接口。
// 所有需要访问 Node 级组件的模块通过此接口获取依赖，而非直接引用全局变量。
//
// 接口会随改造进度逐步扩展：
//   - Phase 1: Config, Logger, AntsPool, TimingWheel, DeDuplicator, NodeId, NodeType
//   - Phase 2: 添加 RpcMonitor, EventBus, Cluster, ServiceMgr 等
//   - Phase 3+: 添加 RPC 层、路由、Profiler 等
package interfaces

import (
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/asynclib"
)

// INodeContext 是 Node 运行时上下文的抽象。
// 各组件通过构造函数注入 INodeContext 来获取依赖。
type INodeContext interface {
	// ── Phase 1: 基础设施层 ──

	// GetConfig 返回本 Node 的配置实例
	GetConfig() *config.Config

	// GetLogger 返回本 Node 的 Logger 实例
	GetLogger() *log.Logger

	// GetAntsPool 返回本 Node 的协程池
	GetAntsPool() *asynclib.Pool

	// GetTimingWheel 返回本 Node 的时间轮实例。
	// 实际类型为 *timingwheel.TimingWheel，因包循环依赖约束返回 any。
	// 调用方应 tw := ctx.GetTimingWheel().(*timingwheel.TimingWheel)。
	GetTimingWheel() any

	// GetDeDuplicator 返回本 Node 的去重器
	GetDeDuplicator() IDeDuplicator

	// ── 节点身份信息 ──

	// GetNodeId 返回本 Node 的 ID 字符串
	GetNodeId() string

	// GetNodeType 返回本 Node 的类型标识
	GetNodeType() string

	// ── Phase 2 组件通过 Node 直接字段访问 ──
	// 由于包循环依赖限制（event/cluster/services/monitor 均已导入 interfaces），
	// Phase 2 组件不在接口中定义。调用方可通过 Node 的导出字段直接访问：
	//   n.RpcMonitor, n.EventBus, n.Cluster, n.ServiceMgr
}
