// Package timingwheel 提供高性能分层时间轮实现。
//
// # OpenSpec
//
//   - 模块:     时间轮
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/utils/timingwheel
//   - 层级:     utility
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// timingwheel 包实现了分层时间轮（Hierarchical Timing Wheel）算法，
// 是引擎中定时器管理的核心组件。相比标准 time.Timer，时间轮在
// 大量定时器场景下具有更优的 O(1) 插入/删除性能。
//
// 支持一次性定时器、周期性定时器和 Cron 表达式调度。
// TaskScheduler 提供任务级调度封装。
//
// # 核心类型
//
//   - TimingWheel:   分层时间轮主体，提供 AfterFunc/Every/Schedule 等 API。
//   - Timer:         定时器句柄，可 Stop/Reset。
//   - Bucket:        时间轮的一个时间槽。
//   - TaskScheduler: 任务调度器，基于时间轮提供高级调度功能。
//   - Spec:          Cron 表达式解析结果。
//   - Option:        时间轮配置选项。
//
// # 子包
//
//   - delayqueue: 延迟队列，时间轮的底层驱动。
//
// # 依赖
//
// 内部:
//   - utils/timingwheel/delayqueue: 延迟队列
//
// 外部:
//   - 无
package timingwheel
