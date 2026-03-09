// Package gate 提供网关模块，处理客户端连接的接入与协议适配。
//
// # OpenSpec
//
//   - 模块:     网关
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/sysModule/gate
//   - 层级:     extension
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// gate 包实现了面向外部客户端的网关模块。负责接受客户端连接
// （WebSocket、TCP 等）、协议解析、会话管理、限流和消息路由。
// 是用户请求进入引擎的第一道入口。
//
// # 核心类型
//
//   - Gate: 网关主模块，管理协议适配器和会话管理器。
//
// # 子包
//
//   - config:           网关配置定义。
//   - proto:            网关协议 protobuf 定义。
//   - limiter:          连接/请求限流器。
//   - protocol_adapter: 协议适配器。
//   - ws:      WebSocket 协议适配。
//   - session: 会话管理。
//   - connx:   连接抽象。
//
// # 依赖
//
// 内部:
//   - core:       Module 基类
//   - interfaces: IGateHandler 等
//   - log:        日志
package gate
