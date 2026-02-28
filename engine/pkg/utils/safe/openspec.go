// Package safe 提供安全执行工具。
//
// # OpenSpec
//
//   - 模块:     安全执行
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/utils/safe
//   - 层级:     utility
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// safe 包提供 panic 安全的函数执行封装。自动 recover panic
// 并转换为错误返回，防止单个 goroutine 的 panic 导致整个进程崩溃。
//
// # 依赖
//
// 无外部依赖。
package safe
