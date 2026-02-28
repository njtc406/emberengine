// Package circuitbreaker 提供通用熔断器实现。
//
// # OpenSpec
//
//   - 模块:     熔断器
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/utils/circuitbreaker
//   - 层级:     utility
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// circuitbreaker 包实现了经典的三态熔断器（Closed/Open/HalfOpen）。
// 通过统计失败率自动切换状态，在检测到下游不健康时快速失败，
// 避免雪崩效应。
//
// # 核心类型
//
//   - Breaker: 熔断器，支持 Execute/Allow/MarkSuccess/MarkFailure 操作。
//
// # 依赖
//
// 无外部依赖。
package circuitbreaker
