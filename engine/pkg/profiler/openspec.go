// Package profiler 提供 EmberEngine 的性能剖析与分析工具。
//
// # OpenSpec
//
//   - 模块:     性能分析
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/profiler
//   - 层级:     infrastructure
//   - 状态:     stable
//   - 线程安全: partial（Profiler 实例非并发安全，需在 Service 线程内使用）
//
// # 概述
//
// profiler 包提供函数级别的耗时追踪能力。通过 Push/Pop 栈式 API
// 记录每次函数调用的进入和退出时间，检测长耗时任务或死锁嫌疑，
// 并支持报表输出。Registry 统一管理所有服务的 Profiler 实例。
//
// # 核心类型
//
//   - Profiler:  性能分析器，实现 interfaces.IProfiler，提供
//     Push/Pop/Reset/IsEnabled 操作。
//   - Element:   调用栈中的单条记录。
//   - Record:    分析记录，包含方法名、耗时、记录类型。
//   - RecordType: 记录类型枚举（正常/超时/死锁嫌疑）。
//   - Registry:  Profiler 实例注册表，按服务名管理。
//   - Adapter:   适配器，桥接外部监控系统。
//
// # 核心函数
//
//   - NewProfiler: 创建 Profiler 实例。
//
// # 配置常量
//
//   - DefaultMaxOvertime:  默认最大超时阈值。
//   - DefaultOvertime:     默认超时阈值。
//   - DefaultMaxRecordNum: 默认最大记录数。
//
// # 依赖
//
// 内部:
//   - interfaces: IProfiler 接口
//   - log:        日志
package profiler
