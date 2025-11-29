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
	"github.com/njtc406/emberengine/engine/pkg/utils/safe"
)

// defaultMailbox 是 IMailbox 的默认实现，用于承载一个 service 的消息入口。
//
// 运行时行为：
//  1. 外部通过 PostMessage 投递 IEvent；
//  2. 在入队前依次调用所有 IMailboxMiddleware 的 MessageReceived，用于限流/统计等；
//  3. 调用 WorkerPool.DispatchEvent，将事件路由到具体 worker；
//  4. Worker 从队列中取出事件并通过 IMessageInvoker 执行业务逻辑。
//
// 挂起机制：
//   - 调用 Suspend() 后，mailbox 进入“挂起”状态，此时仅接受紧急级别及以上的消息；
//   - 普通优先级（低于 PriorityUrgent）的消息会被拒绝并返回 ErrMailboxNotRunning；
//   - 调用 Resume() 可恢复正常接收。
//
// 注意：
//   - 中间件 MessageReceived 当前会在 PostMessage 入口和 worker 执行后各被调用一次；
//     如果中间件依赖调用时机，请在实现中自行区分上下文，或仅在一个阶段使用。
type defaultMailbox struct {
	// 挂起标记
	// mailbox 挂起后, 不再接收紧急以下的任何消息
	// 如果想要在服务挂起后操作服务，需要使用紧急级别以上的消息来触发
	suspended atomic.Bool
	// 工作线程池
	workerPool *WorkerPool
	logger     log.ILogger
}

// NewDefaultMailbox 根据 MailboxConf 创建一个默认 mailbox 实例。
//
//   - conf: 控制队列模式、worker 数量、扩缩容策略等；
//   - logger: 用于记录 mailbox 运行日志；
//   - invoker: 实际处理事件的 IMessageInvoker（通常由 Service 容器提供）；
//   - middlewares: 可选的 mailbox 中间件，在消息入队和处理后被调用。
func NewDefaultMailbox(conf *config.MailboxConf, logger log.ILogger, invoker inf.IMessageInvoker, middlewares ...inf.IMailboxMiddleware) inf.IMailbox {
	return &defaultMailbox{
		workerPool: NewWorkerPool(conf, logger, invoker, middlewares...),
		logger:     logger,
	}
}

// PostMessage 将事件投递到 mailbox。
//
// 行为：
//  1. 如果 mailbox 已挂起（suspended=true），并且事件优先级低于紧急级别，则返回 ErrMailboxNotRunning；
//  2. 依次调用所有中间件的 MessageReceived（入队前 hook，带 panic 防护）；
//  3. 将事件交给 WorkerPool.DispatchEvent，由后者选择合适的 worker 入队。
func (m *defaultMailbox) PostMessage(e inf.IEvent) error {
	// TODO 这个是不是也可以做成一个中间件？还是直接写成是机制
	if e.GetPriority() > def.PriorityUrgent && m.isSuspended() {
		// 挂起后,不再接收紧急以下的任何消息
		return def.ErrMailboxNotRunning
	}

	// 调用所有中间件的 MessageReceived 方法(比如限流、熔断等)
	for _, middleware := range m.workerPool.middlewares {
		if err := safe.Do(func() error {
			middleware.MessageReceived(e) // TODO 这里可能需要一些返回信息,不然无法中断
			return nil
		}); err != nil {
			return err
		}
	}

	return m.workerPool.DispatchEvent(e)
}

func (m *defaultMailbox) isSuspended() bool {
	return m.suspended.Load()
}

func (m *defaultMailbox) Suspend() bool {
	return m.suspended.CompareAndSwap(false, true)
}

func (m *defaultMailbox) Resume() bool {
	return m.suspended.CompareAndSwap(true, false)
}

func (m *defaultMailbox) Start() {
	m.workerPool.Start()
}

func (m *defaultMailbox) Stop() {
	m.workerPool.Stop()
}
