// Package ws 提供 WebSocket 协议的适配实现。
//
// # OpenSpec
//
//   - 模块:     WebSocket 协议适配器
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/sysModule/gate/protocol_adapter/ws
//   - 层级:     extension
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// ws 包实现了网关的 WebSocket 协议适配。处理 WebSocket 升级握手、
// 消息帧的编解码和连接生命周期管理。
//
// # 核心类型
//
//   - WebSocket: WebSocket 协议处理器。
//   - Processor: WebSocket 消息处理器。
//   - Handler:   WebSocket 连接事件处理。
//
// # 依赖
//
// 外部:
//   - github.com/gorilla/websocket: WebSocket 实现
package ws
