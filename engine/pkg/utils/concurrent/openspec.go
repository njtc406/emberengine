// Package concurrent 提供并发执行上下文管理。
//
// # OpenSpec
//
//   - 模块:     并发上下文
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/utils/concurrent
//   - 层级:     utility
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// concurrent 包提供并发操作的上下文管理工具。用于在服务的
// Mailbox 线程之外安全地执行并发任务，并在完成后将结果
// 回调到 Mailbox 线程中，保证数据一致性。
//
// # 依赖
//
// 内部:
//   - interfaces: IConcurrentCallback
package concurrent
