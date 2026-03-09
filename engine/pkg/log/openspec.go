// Package log 提供 EmberEngine 的高性能结构化日志系统。
//
// # OpenSpec
//
//   - 模块:     日志系统
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/log
//   - 层级:     foundation
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// log 包封装了基于 uber-go/zap 的高性能日志系统。提供多级别日志
// （包含自定义 Trace 级别）和结构化日志输出。每个 Node 拥有独立的
// Logger 实例，支持按服务隔离日志输出。
//
// # 核心类型
//
//   - ILoggerX: 增强日志接口，在标准 Logger 基础上扩展了 Trace 级别
//     和 Fields 支持。
//   - Fields:   结构化日志字段 map 类型。
//
// # 依赖
//
// 外部:
//   - go.uber.org/zap: 高性能日志库
package log
