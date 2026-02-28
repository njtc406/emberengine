// Package endpoints 管理集群中所有已知的服务端点。
//
// # OpenSpec
//
//   - 模块:     端点管理
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/cluster/endpoints
//   - 层级:     infrastructure
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// endpoints 包维护当前节点所感知的所有服务端点（PID）列表。
// 当服务发现通知有新节点加入或旧节点离开时，此模块负责
// 同步更新端点状态，并为路由模块提供查询支持。
//
// # 核心类型
//
//   - EndpointManager: 端点管理器，维护全局 PID 索引，提供增删查改操作。
//
// # 依赖
//
// 内部:
//   - actor:      PID 类型
//   - interfaces: INodeEndpointManager 接口
//   - log:        日志
package endpoints
