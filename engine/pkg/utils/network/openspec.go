// Package network 提供底层网络传输实现。
//
// # OpenSpec
//
//   - 模块:     网络传输
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/utils/network
//   - 层级:     utility
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// network 包提供多种网络传输协议的服务端和客户端实现，
// 包括 TCP、WebSocket、KCP 和 HTTP。提供统一的 Conn 抽象接口
// 和 Agent 模型，屏蔽传输层差异。
//
// # 核心类型
//
//   - Conn:       连接接口抽象。
//   - Agent:      连接代理，管理单个连接的读写循环。
//   - TCPServer:  TCP 服务端。
//   - TCPClient:  TCP 客户端。
//   - WSServer:   WebSocket 服务端。
//   - WSConn:     WebSocket 连接封装。
//   - WSClient:   WebSocket 客户端。
//   - KCPServer:  KCP 服务端。
//   - KCPClient:  KCP 客户端。
//   - HTTPServer: HTTP 服务端。
//   - TCPMsg:     TCP 消息帧处理。
//
// # 子包
//
//   - processor: 消息处理器（JSON/Protobuf/PbRaw）。
//
// # 依赖
//
// 外部:
//   - github.com/gorilla/websocket: WebSocket
//   - github.com/xtaci/kcp-go:     KCP 传输
package network
