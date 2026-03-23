// Package mailbox
// @Title  邮箱
// @Description  负责调度消息
// @Author  yr  2025/2/8
// @Update  yr  2025/2/8
package mailbox

import (
	"sync/atomic"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
)

// Mailbox 邮箱
//
// 运行时行为：
//  1. 外部通过 PostMessage 投递 IEvent；
//  2. 如果 mailbox 已挂起，通过 ISuspendPolicy 判断是否放行；
//  3. 依次调用所有中间件的 OnReceive，支持限流/熔断/统计等；
//  4. 调用 WorkerPool.DispatchEvent，将事件路由到具体 worker；
//  5. Worker 从队列中取出事件并通过 IMessageInvoker 执行业务逻辑；
//  6. 执行完成后，逆序调用所有中间件的 OnComplete。
//
// 挂起机制：
//   - 调用 Suspend() 后，mailbox 进入"挂起"状态；
//   - ISuspendPolicy 决定哪些消息可以在挂起时放行（默认：紧急及以上、RPC reply、回调）；
//   - 调用 Resume() 可恢复正常接收。
//
// 中间件机制：
//   - 采用洋葱模型，OnReceive 按顺序执行，OnComplete 按逆序执行；
//   - 中间件可通过返回 Reject 拒绝消息入队；
//   - 中间件可通过返回 Skip 跳过后续中间件直接入队。
type Mailbox struct {
	// 挂起标记
	suspended atomic.Bool
	// 挂起策略（可自定义放行规则）
	suspendPolicy inf.ISuspendPolicy
	// 停机时队列处理策略
	drainPolicy DrainPolicy
	// 工作线程池
	workerPool *WorkerPool
	logger     log.ILoggerX
}

// MailboxOption 用于配置 Mailbox 的选项函数
type MailboxOption func(*Mailbox)

// WithSuspendPolicy 设置自定义的挂起策略( TODO 这个可能需要修改为注册式的，方便扩展)
func WithSuspendPolicy(policy inf.ISuspendPolicy) MailboxOption {
	return func(m *Mailbox) {
		m.suspendPolicy = policy
	}
}

// WithDrainPolicy 设置停机时的队列处理策略。
func WithDrainPolicy(policy DrainPolicy) MailboxOption {
	return func(m *Mailbox) {
		m.drainPolicy = policy
	}
}

// NewMailbox 根据 MailboxConf 创建一个默认 mailbox 实例。
//
//   - conf: 控制队列模式、worker 数量、扩缩容策略等；
//   - logger: 用于记录 mailbox 运行日志；
//   - invoker: 实际处理事件的 IMessageInvoker（通常由 Service 容器提供）；
//   - middlewares: 可选的 mailbox 中间件；
//   - opts: 可选的配置选项，如自定义挂起策略。
func NewMailbox(conf *config.MailboxConf, logger log.ILoggerX, invoker inf.IMessageInvoker,
	middlewares []inf.IMailboxMiddleware, opts ...MailboxOption) (*Mailbox, error) {
	wp, err := NewWorkerPool(conf, logger, invoker, middlewares...)
	if err != nil {
		return nil, err
	}
	m := &Mailbox{
		workerPool:    wp,
		suspendPolicy: NewDefaultSuspendPolicy(), // 默认挂起策略
		drainPolicy:   DrainExecute,
		logger:        logger,
	}
	for _, opt := range opts {
		opt(m)
	}
	// 将策略下发给 workerPool（worker 在 Start 时读取）
	m.workerPool.SetDrainPolicy(m.drainPolicy)
	return m, nil
}

// PostJob 将任务投递到 mailbox。
//
// 行为：
//  1. 如果 mailbox 已挂起，通过 ISuspendPolicy 判断是否放行，不放行则返回 ErrMailboxSuspended；
//  2. 依次调用所有中间件的 OnReceive，任一返回 Reject 则拒绝入队；
//  3. 将事件和中间件上下文交给 WorkerPool.DispatchEvent，由后者选择合适的 worker 入队。
func (m *Mailbox) PostJob(job inf.IMailboxJob) (err error) {
	// 挂起检查
	if m.isSuspended() && !m.suspendPolicy.ShouldAllow(job) {
		return def.ErrMailboxSuspended
	}

	// 执行中间件链的 OnReceive
	result, mctx := m.workerPool.middlewareChain.ExecuteOnReceive(job, m.workerPool.invoker.GetServiceName())
	if result.Action == def.ActionReject {
		if result.Err != nil {
			return result.Err
		}
		return def.ErrMailboxMiddlewareRejected
	}

	job.SetMiddlewareContext(mctx)

	// 分发事件（携带中间件上下文，用于 OnComplete 回调）
	return m.workerPool.DispatchJob(job)
}

func (m *Mailbox) isSuspended() bool {
	return m.suspended.Load()
}

func (m *Mailbox) Suspend() bool {
	return m.suspended.CompareAndSwap(false, true)
}

func (m *Mailbox) Resume() bool {
	return m.suspended.CompareAndSwap(true, false)
}

func (m *Mailbox) Start() {
	if err := m.workerPool.Start(); err != nil {
		m.logger.Errorf("mailbox start failed: %v", err)
	}
}

func (m *Mailbox) BeginStop() {
	m.workerPool.BeginStop()
}

func (m *Mailbox) Wait() {
	m.workerPool.Wait()
}

func (m *Mailbox) Stop() {
	m.BeginStop()
	m.Wait()
}

// IsRWEnabled 返回当前 RW 模式是否启用
func (m *Mailbox) IsRWEnabled() bool {
	return m.workerPool.IsRWEnabled()
}

// GetEnableRWPtr 返回 WorkerPool 内 enableRW 的指针，供 MethodMgr 等外部组件引用。
// 仅在服务初始化阶段调用一次。
func (m *Mailbox) GetEnableRWPtr() *atomic.Bool {
	return m.workerPool.GetEnableRWPtr()
}
