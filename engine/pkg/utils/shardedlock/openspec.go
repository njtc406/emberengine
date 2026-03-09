// Package shardedlock 提供分片锁实现。
//
// # OpenSpec
//
//   - 模块:     分片锁
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/utils/shardedlock
//   - 层级:     utility
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// shardedlock 包实现了分片锁（Sharded Lock），通过将锁空间
// 分片来降低锁竞争。适用于高并发场景下需要细粒度锁的场景。
//
// # 核心类型
//
//   - ShardedLock: 分片锁，按 key 哈希选择分片。
//
// # 依赖
//
// 无外部依赖。
package shardedlock
