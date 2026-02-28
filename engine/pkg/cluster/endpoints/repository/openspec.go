// Package repository 提供端点存储的选择器与仓库实现。
//
// # OpenSpec
//
//   - 模块:     端点仓库
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/cluster/endpoints/repository
//   - 层级:     infrastructure
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// repository 包为 endpoints 模块提供底层数据存储和选择能力。
// Repository 按 ServiceType、Partition 等维度索引 PID，
// Selector 实现了 ISelector 接口，支持多种路由选择策略。
//
// # 核心类型
//
//   - Repository: PID 数据仓库，提供按多维度索引的增删查操作。
//   - Selector:   实现 interfaces.ISelector，基于 Repository 执行路由选择。
//
// # 依赖
//
// 内部:
//   - actor:      PID 类型
//   - interfaces: ISelector 接口
package repository
