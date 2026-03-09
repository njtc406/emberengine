// Package pool 提供通用对象池抽象。
//
// # OpenSpec
//
//   - 模块:     对象池
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/utils/pool
//   - 层级:     utility
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// pool 包提供通用的对象池抽象接口（IPool）和基础实现，
// 支持对象复用以减少 GC 压力。包含统计记录器用于监控池使用率。
//
// # 核心类型
//
//   - IPool:         对象池接口。
//   - Pool:          对象池实现。
//   - StatsRecorder: 池使用统计记录器。
//
// # 依赖
//
// 无外部依赖。
package pool
