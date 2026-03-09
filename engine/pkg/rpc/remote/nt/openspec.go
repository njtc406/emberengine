// Package nt 提供基于 NATS 的 RPC 服务端实现。
//
// # OpenSpec
//
//   - 模块:     NATS 服务端
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/rpc/remote/nt
//   - 层级:     infrastructure
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// nt 包实现了基于 NATS 消息队列的 RPC 服务端监听器。通过订阅
// 特定主题接收远程调用请求，并将请求转交给 Handler 处理。
// NATS 方式适合高吞吐、低延迟的消息传递场景。
//
// # 核心类型
//
//   - Server:   NATS RPC 服务端封装。
//   - Listener: NATS 订阅监听器，管理主题订阅生命周期。
//
// # 依赖
//
// 外部:
//   - github.com/nats-io/nats.go: NATS 客户端
package nt
