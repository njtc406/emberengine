// Package interfaces
// @Title  title
// @Description  desc
// @Author  yr  2025/2/8
// @Update  yr  2025/2/8
package interfaces

import "github.com/njtc406/emberengine/engine/pkg/def"

// IMailboxMiddleware 中间件
type IMailboxMiddleware interface {
	MailboxStarted()             // 当Mailbox启动时调用
	MessageReceived(evt IEvent)  // 当有消息到达时调用
	MessageProcessed(evt IEvent) // 当消息处理完成时调用
}

// IMessageInvoker 处理消息
type IMessageInvoker interface {
	GetServiceName() string
	InvokeMessage(evt IEvent)
	EscalateFailure(reason interface{}, evt IEvent)
}

// IMailboxChannel 消息接口
type IMailboxChannel interface {
	PostMessage(evt IEvent) error
}

// IMailbox interface is used to enqueue messages to the mailbox
type IMailbox interface {
	IMailboxChannel
	Start()
	Stop()
	Suspend() bool // 挂起
	Resume() bool  // 恢复
}

type IMailboxWorker interface {
	Start()
	Stop()
	SubmitEvent(evt IEvent) error
	GetWorkerId() int
	// GetMsgLen 获取当前队列中的消息数量
	GetMsgLen() int
}

type IMailboxStatistics interface {
	GetPriorityQueueLen(priority def.Priority) int
	GetTotalQueueLen() int
	GetPriorityStatistics() map[def.Priority]int
	GetSchedulerStatistics() map[string]interface{}
}

//type IDispatcher interface {
//	Schedule(fn func()) error
//	Throughput() int // 每次处理的消息数量,达到该值后,释放cpu资源等待下次处理
//}

// MailboxProducer is a function which creates a new mailbox
//type MailboxProducer func(conf *config.Mailbox, invoker IMessageInvoker, middlewares ...IMailboxMiddleware) IMailbox
