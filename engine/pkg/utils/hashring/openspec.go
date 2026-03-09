// Package hashring 提供一致性哈希环实现。
//
// # OpenSpec
//
//   - 模块:     一致性哈希
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/utils/hashring
//   - 层级:     utility
//   - 状态:     stable
//   - 线程安全: partial（读并发安全，写需外部同步）
//
// # 概述
//
// hashring 包实现了一致性哈希环算法，用于在集群多节点间
// 均匀分布负载。支持虚拟节点、节点增删和 key 寻址。
//
// # 核心类型
//
//   - HashRing: 一致性哈希环，提供 Add/Remove/Get 操作。
//
// # 依赖
//
// 无外部依赖。
package hashring
