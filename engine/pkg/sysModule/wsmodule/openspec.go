// Package wsmodule 提供 WebSocket 客户端管理模块。
//
// # OpenSpec
//
//   - 模块:     WebSocket 模块
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/sysModule/wsmodule
//   - 层级:     extension
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// wsmodule 包提供 WebSocket 客户端的连接管理和消息处理能力。
// ClientMgr 维护所有活跃的 WebSocket 连接，提供广播、
// 单播和连接生命周期管理功能。
//
// # 核心类型
//
//   - ClientMgr: WebSocket 客户端管理器，维护连接池和路由表。
//   - Client:    单个 WebSocket 客户端抽象，封装读写操作。
//
// # 依赖
//
// 内部:
//   - core:       Module 基类
//   - interfaces: IModule 接口
//   - log:        日志
//
// 外部:
//   - github.com/gorilla/websocket: WebSocket 实现
package wsmodule
