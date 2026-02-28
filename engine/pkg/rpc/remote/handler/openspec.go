// Package handler 提供远程 RPC 消息的接收与派发处理。
//
// # OpenSpec
//
//   - 模块:     远程消息处理器
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/rpc/remote/handler
//   - 层级:     infrastructure
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// handler 包负责接收远程节点发来的 RPC 消息，将其反序列化后
// 投递到目标 Service 的 Mailbox 中。它是远程消息进入本地
// 处理流水线的入口点。
//
// # 核心类型
//
//   - Handler: 远程消息处理器，实现消息接收 → 解码 → 路由 → 投递。
//
// # 依赖
//
// 内部:
//   - actor:              PID 查找
//   - interfaces:         IEnvelope, IMailboxChannel
//   - rpc/message/msgenvelope: 消息解码
//   - services:           服务查找
//   - log:                日志
package handler
