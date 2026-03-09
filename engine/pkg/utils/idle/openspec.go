// Package idle 提供空闲检测与退避策略。
//
// # OpenSpec
//
//   - 模块:     空闲检测
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/utils/idle
//   - 层级:     utility
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// idle 包提供空闲检测器和指数退避策略。用于在无消息时
// 优雅地降低 CPU 使用率，避免忙等浪费资源。
//
// # 核心类型
//
//   - Idle:    空闲检测器。
//   - Backoff: 指数退避策略实现。
//
// # 依赖
//
// 无外部依赖。
package idle
