// Package mailbox
// @Title  邮箱
// @Description  负责调度消息
// @Author  yr  2025/2/8
// @Update  yr  2025/2/8
package mailbox

import (
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
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

func (m *defaultMailbox) PostMessage(e inf.IEvent) error {
	if e.GetPriority() != def.PrioritySys && m.isSuspended() {
		return def.ErrMailboxNotRunning
	}
	return m.workerPool.DispatchEvent(e)
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
