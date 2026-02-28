// Package limiter 提供网关的连接和请求限流能力。
//
// # OpenSpec
//
//   - 模块:     网关限流器
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/sysModule/gate/limiter
//   - 层级:     extension
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// limiter 包为网关提供连接级和请求级的限流能力，
// 防止单个客户端或整体流量超过系统承载能力。
//
// # 核心类型
//
//   - Limiter: 限流器接口及实现。
//
// # 依赖
//
// 内部:
//   - log: 日志
package limiter
