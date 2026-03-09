// Package ring 提供环形缓冲区实现。
//
// # OpenSpec
//
//   - 模块:     环形缓冲区
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/utils/ring
//   - 层级:     utility
//   - 状态:     stable
//   - 线程安全: no（需外部同步）
//
// # 概述
//
// ring 包实现了高效的环形缓冲区（Ring Buffer），提供固定容量的
// FIFO 读写操作。是 MPMC/MPSC 队列等高性能组件的底层基础。
//
// # 核心类型
//
//   - Ring: 环形缓冲区，提供 Push/Pop/Len/Cap 操作。
//
// # 依赖
//
// 无外部依赖。
package ring
