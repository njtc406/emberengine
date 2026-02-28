// Package delayqueue 提供延迟队列实现。
//
// # OpenSpec
//
//   - 模块:     延迟队列
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/utils/timingwheel/delayqueue
//   - 层级:     utility
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// delayqueue 包实现了基于最小堆的延迟队列，是时间轮的底层驱动。
// 元素按到期时间排序，支持阻塞等待和批量弹出到期元素。
//
// # 核心类型
//
//   - DelayQueue: 延迟队列，提供 Offer/Poll/Take 操作。
//
// # 依赖
//
// 无外部依赖。
package delayqueue
