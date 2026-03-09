// Package emberctx 提供引擎级上下文传递工具。
//
// # OpenSpec
//
//   - 模块:     引擎上下文
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/utils/emberctx
//   - 层级:     utility
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// emberctx 包提供在 context.Context 中存取引擎特定数据的工具函数，
// 如追踪ID、服务信息等。用于跨层传递引擎运行时上下文。
//
// # 依赖
//
// 外部:
//   - 标准库 context
package emberctx
