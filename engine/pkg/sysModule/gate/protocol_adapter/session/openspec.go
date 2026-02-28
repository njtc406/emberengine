// Package session 提供网关的会话管理能力。
//
// # OpenSpec
//
//   - 模块:     会话管理
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/sysModule/gate/protocol_adapter/session
//   - 层级:     extension
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// session 包管理客户端连接对应的会话状态。每个客户端连接
// 关联一个 Session，用于追踪认证状态、用户数据和连接元信息。
// 支持 WebSocket 会话的特化实现。
//
// # 核心类型
//
//   - SessionBase:   会话基类。
//   - SessionWs:     WebSocket 会话实现。
//   - SessionMgrWs:  WebSocket 会话管理器。
//
// # 依赖
//
// 内部:
//   - log: 日志
package session
