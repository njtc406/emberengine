// Package asynclib 提供高性能协程池封装。
//
// # OpenSpec
//
//   - 模块:     协程池
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/utils/asynclib
//   - 层级:     utility
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// asynclib 包封装了基于 ants 的协程池，提供受控的并发 goroutine
// 执行能力，避免无限制创建 goroutine 导致资源耗尽。
//
// # 核心类型
//
//   - Pool: 协程池封装，提供 Go/Release 等操作。
//
// # 依赖
//
// 外部:
//   - github.com/panjf2000/ants/v2: 协程池
package asynclib
