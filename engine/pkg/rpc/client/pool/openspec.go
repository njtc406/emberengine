// Package pool 提供 RPC 客户端连接池管理。
//
// # OpenSpec
//
//   - 模块:     RPC 连接池
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/rpc/client/pool
//   - 层级:     infrastructure
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// pool 包管理远程 RPC 客户端连接的复用和生命周期。提供连接池工厂、
// 运行时监控和熔断器集成。通过连接池化降低频繁建连的开销，
// 同时通过熔断器防止对不健康节点的持续请求。
//
// # 核心类型
//
//   - Manager:        连接池管理器，维护按目标地址分组的连接池。
//   - Factory:        连接工厂，按协议类型创建客户端连接。
//   - CircuitBreaker: 熔断器，按失败率自动切换 Open/HalfOpen/Closed 状态。
//
// # 依赖
//
// 内部:
//   - def:  常量
//   - log:  日志
//
// 外部:
//   - 各 RPC 框架的客户端库
package pool
