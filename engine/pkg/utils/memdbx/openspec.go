// Package memdbx 提供内存数据库封装。
//
// # OpenSpec
//
//   - 模块:     内存数据库
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/utils/memdbx
//   - 层级:     utility
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// memdbx 包封装了内存数据库能力，提供基于索引的快速查询，
// 适用于需要复杂查询但数据量有限的场景。
//
// # 依赖
//
// 外部:
//   - github.com/hashicorp/go-memdb: 内存数据库（如使用）
package memdbx
