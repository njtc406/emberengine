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

type defaultMailbox struct {
	// 挂起标记
	// 邮箱挂起后,不再接收紧急以下的任何消息
	// 如果想要在服务挂起后操作服务，需要使用紧急级别以上的消息来触发
	suspended atomic.Bool
	// 工作线程池
	workerPool *WorkerPool
	logger     log.ILogger
}

func NewDefaultMailbox(conf *config.MailboxConf, logger log.ILogger, invoker inf.IMessageInvoker, middlewares ...inf.IMailboxMiddleware) inf.IMailbox {
	return &defaultMailbox{
		workerPool: NewWorkerPool(conf, logger, invoker, middlewares...),
		logger:     logger,
	}
}

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
