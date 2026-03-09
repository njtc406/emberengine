// Package mpsc 提供多生产者单消费者无锁队列。
//
// # OpenSpec
//
//   - 模块:     MPSC 队列
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/utils/mpsc
//   - 层级:     utility
//   - 状态:     stable
//   - 线程安全: yes（多生产者侧并发安全，消费者侧单线程）
//
// # 概述
//
// mpsc 包实现了多生产者单消费者（Multi-Producer Single-Consumer）
// 的无锁队列。这是 Mailbox 内部使用的核心数据结构，
// 多个外部 goroutine 可以同时投递消息，而 Mailbox 的 Worker
// 单线程消费，实现无锁高效调度。
//
// # 核心类型
//
//   - Deque: MPSC 无锁双端队列。
//
// # 依赖
//
// 无外部依赖。
package mpsc
