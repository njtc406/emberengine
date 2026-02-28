// Package dedup 提供消息去重能力。
//
// # OpenSpec
//
//   - 模块:     消息去重器
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/utils/dedup
//   - 层级:     utility
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// dedup 包实现了基于时间窗口的消息去重器。通过维护已处理消息的
// ID 集合，在窗口期内自动过滤重复消息，防止网络重传导致的
// 重复处理。
//
// # 核心类型
//
//   - Deduplicator: 实现 interfaces.IDeduplicator 的去重器。
//
// # 依赖
//
// 内部:
//   - interfaces: IDeduplicator
package dedup
