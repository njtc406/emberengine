// Package interfaces 定义了 EmberEngine 全部核心组件的解耦接口。
//
// # OpenSpec
//
//   - 模块:     接口契约
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/interfaces
//   - 层级:     foundation
//   - 状态:     stable
//   - 线程安全: N/A（纯接口定义，无实现状态）
//
// # 概述
//
// interfaces 包是整个引擎实现依赖倒置（DIP）的关键。它集中定义了
// 所有核心组件的行为契约，使得具体实现可以独立演进而不破坏依赖关系。
// 这是引擎架构中耦合度最低的包，被几乎所有其他包引用。
//
// # 核心接口
//
// 服务层:
//   - IService:          顶级服务接口，组合 ILifecycle、IIdentifiable、IServiceHandler 等。
//   - ILifecycle:        生命周期（Init/Start/Stop/OnInit/OnStart/OnStarted/OnRelease）。
//   - IServiceHandler:   服务处理器（获取配置、邮箱、RPC 处理器等）。
//   - IIdentifiable:     服务身份标识。
//
// 模块层:
//   - IModule:           模块接口，组合 IModuleLifecycle、IModuleIdentity、IModuleHierarchy。
//   - IModuleHierarchy:  模块层级管理（AddModule/ReleaseModule/GetModule）。
//   - IMethodMgr:        方法管理器（注册/查询/移除 RPC 方法）。
//
// 通信层:
//   - IBus:              RPC 调用语义接口（Call/AsyncCall/Send 及 WithOpt 变体）。
//   - IEnvelope:         RPC 消息信封，抽象元数据和数据部。
//   - IEnvelopeMeta:     信封元数据（发送方/接收方 PID、ReqId、Deadline）。
//   - IEnvelopeData:     信封数据部（方法、请求/响应、错误）。
//   - IRpcHandler:       组合 IRpcInvoker、IRpcProcessor。
//   - IRpcSelector:      RPC 路由选择（Select/SelectByPid/SelectByRule 等）。
//
// 事件层:
//   - IEvent:            事件接口（EventType、Data、Context）。
//   - IEventProcessor:   事件分发器（Trigger、BindHandler、PublishGlobal）。
//   - IEventHandler:     事件处理器管理。
//   - IEventChannel:     事件推送通道。
//
// 邮箱层:
//   - IMailbox:          邮箱接口（Start/Stop/Suspend/Resume）。
//   - IMailboxJob:       任务接口（上下文、优先级、分发键、类型）。
//   - IMailboxChannel:   邮箱投递通道（PostJob）。
//   - IMessageInvoker:   消息调用器（ExecuteJob/EscalateFailure）。
//   - IMailboxMiddleware: 邮箱中间件（OnStart/OnStop/OnReceive/OnComplete）。
//
// 运行时层:
//   - INodeContext:      节点上下文，提供获取所有核心组件的入口。
//   - INodeConfig:       节点配置查询。
//   - INodePool:         协程池接口。
//   - INodeTimingWheel:  时间轮接口。
//   - INodeRouter:       路由接口。
//   - ISelector:         底层路由选择器。
//
// 基础设施层:
//   - IDiscovery:        服务发现接口。
//   - IRemoteServer:     远程 RPC 服务端接口。
//   - ICodec:            编解码器接口。
//   - IDeduplicator:     去重器接口。
//   - IProfiler:         性能分析器接口。
//   - IMonitor:          RPC 监控接口。
//
// # 依赖
//
// 内部:
//   - actor: PID 类型引用
//   - def:   常量和枚举引用
//   - log:   ILoggerX 接口引用
//
// 外部:
//   - 无
package interfaces
