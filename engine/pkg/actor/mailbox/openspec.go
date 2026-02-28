// Package mailbox 实现了 EmberEngine 的 Actor 消息邮箱调度系统。
//
// # OpenSpec
//
//   - 模块:     消息邮箱
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/actor/mailbox
//   - 层级:     foundation
//   - 状态:     stable
//   - 线程安全: yes（Mailbox 本身是并发安全的投递入口）
//
// # 概述
//
// mailbox 包是每个 Service 的消息调度核心。它负责接收外部投递的 Job，
// 通过中间件链处理后交由 Worker 执行。支持优先级队列、消息挂起/恢复、
// 熔断器、限流、分发键统计等高级特性。
//
// Mailbox 采用可扩展的 Worker Pool 架构，支持动态扩缩容（Scaler），
// 并通过 Strategy 接口实现不同的调度策略。
//
// # 核心类型
//
//   - Mailbox:                 消息邮箱主体，管理 Worker Pool 和中间件链。
//   - MailboxOption:           构造时的可选配置函数。
//   - Worker/WorkerPool:       实际执行 Job 的工作单元和池。
//   - Scheduler:               调度器，决定 Job 分配给哪个 Worker。
//   - Scaler:                  动态扩缩容控制器。
//   - QueueManager:            队列管理器，支持普通/双通道/优先级模式。
//   - MiddlewareChain:         中间件执行链。
//   - SentinelMiddleware:      哨兵中间件（默认末端执行器）。
//   - RateLimitMiddleware:     限流中间件。
//   - CircuitBreakerMiddleware: 熔断器中间件。
//   - DispatchKeyStatsMiddleware: 分发键统计中间件。
//   - SuspendPolicy:           挂起策略。
//   - StopPolicy:              停止策略。
//
// # 核心函数
//
//   - NewMailbox:      创建 Mailbox 实例，注入配置、日志、调用者和中间件。
//   - PostJob:         向 Mailbox 投递一个 Job（线程安全）。
//   - Suspend/Resume:  暂停/恢复接收普通消息。
//   - Start/Stop/Wait: 生命周期管理。
//
// # 依赖
//
// 内部:
//   - config:     MailboxConf 配置
//   - def:        常量和默认值
//   - interfaces: IMailbox, IMailboxJob, IMessageInvoker, IMailboxMiddleware 等接口
//   - log:        日志
//
// 外部:
//   - 无外部第三方依赖
package mailbox
