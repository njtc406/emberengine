// Package connx 提供网关的连接抽象。
//
// # OpenSpec
//
//   - 模块:     连接抽象
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/sysModule/gate/protocol_adapter/connx
//   - 层级:     extension
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// connx 包提供网关连接的统一抽象，屏蔽不同传输协议
// （WebSocket、TCP 等）的差异，向上层提供一致的读写接口。
//
// # 核心类型
//
//   - Conn: 连接抽象接口及基础实现。
//
// # 依赖
//
// 内部:
//   - 无
package connx
