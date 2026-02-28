// Package queue 提供多种队列数据结构实现。
//
// # OpenSpec
//
//   - 模块:     队列集合
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/utils/queue
//   - 层级:     utility
//   - 状态:     stable
//   - 线程安全: partial（SyncQueue 线程安全，其他需外部同步）
//
// # 概述
//
// queue 包提供多种队列实现，满足不同场景需求：
//   - Queue:         基础 FIFO 队列。
//   - Deque:         双端队列。
//   - PriorityQueue: 优先级队列。
//   - SyncQueue:     线程安全同步队列。
//   - SQueue:        特化队列。
//
// # 依赖
//
// 无外部依赖。
package queue
