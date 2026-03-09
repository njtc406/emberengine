// Package client 提供 EmberEngine 的 RPC 客户端发送管理。
//
// # OpenSpec
//
//   - 模块:     RPC 客户端
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/rpc/client
//   - 层级:     infrastructure
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// client 包负责管理所有外发 RPC 连接。SenderManager 维护按协议类型
// （gRPC、NATS、RPCX）分类的 Sender 实例池，根据目标 PID 的 RpcType
// 自动选择正确的传输协议。本地调用通过 LocalSender 短路，避免网络开销。
//
// # 核心类型
//
//   - SenderManager:    发送器管理器，维护所有 Sender 实例。
//   - Sender:           发送器抽象基类。
//   - LocalSender:      本地发送器，直接投递到目标 Mailbox。
//   - GrpcRemoteSender: 基于 gRPC 的远程发送器。
//   - NatsRemoteSender: 基于 NATS 的远程发送器。
//   - RpcxRemoteSender: 基于 RPCX 的远程发送器。
//
// # 依赖
//
// 内部:
//   - actor:              PID 类型
//   - def:                RPC 类型常量
//   - interfaces:         IRpcClient 接口
//   - rpc/client/pool:    连接池管理
//   - rpc/message:        消息封装
//   - log:                日志
//
// 外部:
//   - google.golang.org/grpc:    gRPC 客户端
//   - github.com/nats-io/nats.go: NATS 客户端
//   - github.com/smallnest/rpcx: RPCX 客户端
package client
