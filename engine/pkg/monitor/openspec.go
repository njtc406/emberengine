// Package monitor 提供 RPC 调用的超时监控能力。
//
// # OpenSpec
//
//   - 模块:     RPC 监控
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/monitor
//   - 层级:     infrastructure
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// monitor 包专门用于监控 RPC Call（同步调用）请求的状态。
// 通过时间轮（TimingWheel）管理超时计时器，在超时或响应到达时
// 触发对应的回调。确保每个 RPC 调用都不会无限挂起，提供了
// 系统可靠性的关键保障。
//
// # 核心类型
//
//   - RpcMonitor: RPC 监控器主体，管理 CallState 生命周期。
//     维护 waitBucket 分桶哈希，降低锁竞争。
//   - CallState:  单次 RPC 调用的状态追踪，包含请求ID、
//     超时定时器、回调函数等。
//
// # 核心函数
//
//   - NewRpcMonitor: 创建监控器实例。
//   - Add:           注册新的 RPC 调用监控。
//   - Get:           根据 ReqId 获取调用状态。
//   - Del:           移除已完成的调用监控。
//   - Clear:         清空所有监控状态。
//
// # 依赖
//
// 内部:
//   - interfaces:       IMonitor 接口
//   - utils/timingwheel: 时间轮
//   - log:              日志
package monitor
