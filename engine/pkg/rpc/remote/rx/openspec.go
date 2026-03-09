// Package rx 提供基于 RPCX 的 RPC 服务端实现。
//
// # OpenSpec
//
//   - 模块:     RPCX 服务端
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/rpc/remote/rx
//   - 层级:     infrastructure
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// rx 包实现了基于 RPCX 框架的 RPC 服务端监听器。RPCX 提供了
// 丰富的服务治理能力（服务发现、负载均衡、熔断等），
// 适合对服务治理有较高要求的部署场景。
//
// # 核心类型
//
//   - Server:   RPCX 服务端封装。
//   - Listener: RPCX 监听器，管理服务注册和请求接收。
//
// # 依赖
//
// 外部:
//   - github.com/smallnest/rpcx: RPCX 框架
package rx
