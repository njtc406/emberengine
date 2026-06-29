// Package interfaces
// INodeContext 定义了 Node 运行时上下文接口。
// 所有需要访问 Node 级组件的模块通过此接口获取依赖，而非直接引用全局变量。
//
// 循环依赖约束：interfaces 包不能 import 以下包（它们已 import interfaces）：
//
//	monitor, event, cluster, services, router, rpc/client, rpc/client/pool, core/rpc
//
// 因此这些组件通过窄接口暴露，由具体类型隐式满足。
//
// 以下包不 import interfaces，可以安全返回具体类型：
//
//	config, log, asynclib, timingwheel, profiler, plugins, actor(root), def
package interfaces

import (
	"time"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/log"
)

// ── 窄接口定义（由具体类型隐式满足，避免 interfaces → 具体包的反向导入） ──

type INodeConfig interface {
	IsDebug() bool
	GetStatus() string
	GetDefaultRpcTimeout() time.Duration
	GetCheckTimeoutInterval() time.Duration
}

type INodePool interface {
	Go(f func()) error
	Release()
	Running() int
	Cap() int
}

type INodeTimingWheel interface {
	Start()
	Stop()
	IsClosed() bool
	SetTimeOffset(offset time.Duration)
}

// INodeEndpointManager 端点管理器窄接口，由 *endpoints.EndpointManager 隐式满足
type INodeEndpointManager interface {
	CreatePid(partition int32, serviceId, serviceType, serviceName string, version int64, rpcType string) *actor.PID
	AddService(svc IService)
	ServiceReady(svc IService)
	RemoveService(svc IService)
	ToNodeService(svc IService)
}

// INodeEventBus 事件总线窄接口，由 *event.Bus 隐式满足
type INodeEventBus interface {
	SubscribeGlobal(eventType def.EventType, svc IListener)
	UnSubscribeGlobal(eventType def.EventType, svc IListener)
}

// INodeRouter 路由器窄接口，由 *router.Router 隐式满足
type INodeRouter interface {
	Select(sender *actor.PID, options ...SelectParamBuilder) IBus
	SelectByPid(sender, receiver *actor.PID) IBus
	RouteByPid(sender, receiver *actor.PID) IBus
	SelectByRule(sender *actor.PID, rule func(pid *actor.PID) bool) IBus
	SelectByServiceUid(sender *actor.PID, receiverServiceUid string) IBus
}

// INodeProfilerRegistry Profiler 注册中心窄接口，由 *profiler.Registry 隐式满足
type INodeProfilerRegistry interface {
	RegProfiler(name string, logger log.ILoggerX) IProfiler
	UnRegProfiler(name string)
}

type IProfiler interface {
	Push(tag string)
	Pop()
	Reset()
	IsEnabled() bool
}

// INodeMethodIndex 方法前缀索引窄接口，由 *rpc.MethodIndex 隐式满足
type INodeMethodIndex interface {
	HasApiPrefix(s string) bool
	HasRpcPrefix(s string) bool
	HasApiReadOnlyPrefix(s string) bool
	HasRpcReadOnlyPrefix(s string) bool
}

// INodeContext 是 Node 运行时上下文的抽象。
// 各组件通过构造函数注入 INodeContext 来获取依赖。
type INodeContext interface {
	// ── 基础设施层（无循环依赖，安全返回具体类型） ──

	// GetConfig 返回本 Node 的配置实例
	GetConfig() INodeConfig

	// GetLogger 返回本 Node 的 Logger 实例
	GetLogger() log.ILoggerX

	// GetAntsPool 返回本 Node 的协程池
	GetAntsPool() INodePool

	// GetTimingWheel 返回本 Node 的时间轮窄接口
	GetTimingWheel() INodeTimingWheel

	// GetDeDuplicator 返回本 Node 的去重器
	GetDeDuplicator() IDeDuplicator

	// ── 节点身份信息 ──

	// GetNodeId 返回本 Node 的 ID 字符串
	GetNodeId() string

	// GetNodeType 返回本 Node 的类型标识
	GetNodeType() string

	// GetNodeUid 返回本 Node 的运行时唯一标识
	GetNodeUid() string

	// ── 核心组件（返回窄接口，解决循环依赖 + 防止接口混用） ──

	// IsClusterMode 返回是否开启集群模式
	IsClusterMode() bool

	// GetEndpointManager 返回端点管理器（窄接口）
	GetEndpointManager() INodeEndpointManager

	// GetEventBus 返回事件总线（窄接口）
	GetEventBus() INodeEventBus

	// GetRouter 返回路由器（窄接口）
	GetRouter() INodeRouter

	// GetProfilerRegistry 返回 Profiler 注册中心（窄接口）
	GetProfilerRegistry() INodeProfilerRegistry

	// GetMethodIndex 返回方法前缀索引（窄接口）
	GetMethodIndex() INodeMethodIndex

	// ── 可观测性 ──

	// IsReady 返回节点是否已就绪，可接收流量
	IsReady() bool

	// GetRuntimeMetricsText 返回 Prometheus exposition text 格式的运行时指标
	GetRuntimeMetricsText() string
}
