// Package msgbus 实现了 IBus 接口的消息分发能力。
//
// # OpenSpec
//
//   - 模块:     消息总线
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/rpc/message/msgbus
//   - 层级:     infrastructure
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// msgbus 包实现了 interfaces.IBus 接口，提供 Call（同步调用）、
// AsyncCall（异步调用）和 Send（单向发送）三种 RPC 调用语义。
// 它是服务间通信的高层入口，底层通过 SenderManager 选择合适的
// 传输通道，通过 RpcMonitor 追踪调用状态。
//
// # 核心类型
//
//   - Bus: 实现 interfaces.IBus，提供完整的 RPC 调用语义：
//   - Call/CallWithOpt:           同步调用，阻塞等待响应或超时。
//   - AsyncCall/AsyncCallWithOpt: 异步调用，通过回调接收响应。
//   - Send/SendWithOpt:           单向发送，不等待响应。
//   - Release:                    释放资源。
//
// # 依赖
//
// 内部:
//   - actor:                PID
//   - rpc/client:           发送器管理
//   - rpc/message/msgenvelope: 信封构建
//   - monitor:              RPC 监控
//   - interfaces:           IBus 接口
package msgbus
