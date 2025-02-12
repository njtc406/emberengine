// Package mailbox
// @Title  邮箱
// @Description  负责调度消息
// @Author  yr  2025/2/8
// @Update  yr  2025/2/8
package mailbox

import (
	"github.com/njtc406/emberengine/engine/config"
	"github.com/njtc406/emberengine/engine/errdef"
	"github.com/njtc406/emberengine/engine/inf"
	"sync/atomic"
)

type defaultMailbox struct {
	suspended  int32 // 挂起标记
	workerPool *WorkerPool
}

func NewDefaultMailbox(conf *config.WorkerConf, invoker inf.IMessageInvoker, middlewares ...inf.IMailboxMiddleware) inf.IMailbox {
	return &defaultMailbox{
		workerPool: NewWorkerPool(conf, invoker, middlewares...),
	}
}

func (m *defaultMailbox) PostUserMessage(e inf.IEvent) error {
	// 检查mailbox状态

	// TODO 后面检查一下这个标记是否会并发设置,如果没有并发,那么这里修改一下,不然每个msg都要load
	if m.isSuspended() {
		return errdef.MailboxNotRunning
	}

	return m.workerPool.DispatchUserEvent(e)
}

func (m *defaultMailbox) PostSystemMessage(e inf.IEvent) error {
	return m.workerPool.DispatchSysEvent(e)
}

func (m *defaultMailbox) isSuspended() bool {
	return atomic.LoadInt32(&m.suspended) == 1
}

func (m *defaultMailbox) Suspend() bool {
	return atomic.CompareAndSwapInt32(&m.suspended, 0, 1)
}

func (m *defaultMailbox) Resume() bool {
	return atomic.CompareAndSwapInt32(&m.suspended, 1, 0)
}

func (m *defaultMailbox) Start() {
	m.workerPool.Start()
}

func (m *defaultMailbox) Stop() {
	m.workerPool.Stop()
}
