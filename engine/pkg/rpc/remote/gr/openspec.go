// Package gr 提供基于 gRPC 的 RPC 服务端实现。
//
// # OpenSpec
//
//   - 模块:     gRPC 服务端
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/rpc/remote/gr
//   - 层级:     infrastructure
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// gr 包实现了基于 gRPC 的 RPC 服务端监听器。注册 gRPC 服务方法，
// 接收远程调用请求并转交给 Handler 处理。
//
// # 核心类型
//
//   - Server:   gRPC 服务端封装。
//   - Listener: gRPC 监听器，管理 TCP 端口绑定和 gRPC Server 生命周期。
//
// # 依赖
//
// 外部:
//   - google.golang.org/grpc: gRPC 框架
package gr
