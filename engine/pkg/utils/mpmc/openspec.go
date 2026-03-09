// Package mpmc 提供多生产者多消费者无锁队列。
//
// # OpenSpec
//
//   - 模块:     MPMC 队列
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/utils/mpmc
//   - 层级:     utility
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// mpmc 包实现了多生产者多消费者（Multi-Producer Multi-Consumer）
// 的无锁并发队列。基于环形缓冲区和 CAS 原子操作，提供极高的
// 并发吞吐量，是引擎内部高性能消息传递的基础组件。
//
// # 核心类型
//
//   - Queue: MPMC 无锁环形队列，提供 Enqueue/Dequeue 操作。
//
// # 依赖
//
// 无外部依赖。
package mpmc
