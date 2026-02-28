// Package bytespool 提供字节缓冲区对象池。
//
// # OpenSpec
//
//   - 模块:     字节池
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/utils/bytespool
//   - 层级:     utility
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// bytespool 包提供分级字节缓冲区对象池，按容量分桶复用 []byte，
// 减少高频场景下的内存分配和 GC 压力。
//
// # 依赖
//
// 无外部依赖。
package bytespool
