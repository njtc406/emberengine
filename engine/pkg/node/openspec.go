// Package node 是 EmberEngine 的顶级运行时容器。
//
// # OpenSpec
//
//   - 模块:     节点容器
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/node
//   - 层级:     core
//   - 状态:     stable
//   - 线程安全: yes（启动后组件引用不变，各组件自身线程安全）
//
// # 概述
//
// node 包定义了 EmberEngine 的自包含运行时实例。一个 Node 代表
// 一个完整的进程上下文，持有该进程所需的全部私有组件实例。Node
// 完整实现了 interfaces.INodeContext 接口，是所有服务运行时依赖
// 的注入源头。
//
// 每个进程可运行一个或多个 Node 实例（资源隔离），每个 Node
// 拥有独立的配置、日志、连接池、时间轮、事件总线、集群管理器、
// 服务管理器和路由器。
//
// # 核心类型
//
//   - Node: 引擎运行时容器，组合了以下核心组件：
//   - Config:       配置系统
//   - Logger:       日志实例
//   - AntsPool:     协程池
//   - TimingWheel:  时间轮
//   - EventBus:     事件总线
//   - Cluster:      集群管理器
//   - ServiceMgr:   服务管理器
//   - Router:       路由器
//   - RpcMonitor:   RPC 监控器
//   - SenderMgr:    RPC 发送管理器
//
// # 核心函数
//
//   - Start:       启动 Node（接受可选的 StartOption）。
//   - Stop:        优雅停止 Node。
//   - GetNodeUid:  获取节点唯一标识。
//
// # 接口实现
//
// Node 完整实现 interfaces.INodeContext，包括：
//   - GetConfig, GetLogger, GetRouter, GetEventBus, GetCluster 等。
//
// # 依赖
//
// 内部:
//   - 几乎所有 engine/pkg 子包（作为顶层聚合器）
//
// 外部:
//   - github.com/panjf2000/ants/v2: 协程池
package node
