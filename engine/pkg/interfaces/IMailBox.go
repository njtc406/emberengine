// Package interfaces
// @Title  title
// @Description  desc
// @Author  yr  2025/2/8
// @Update  yr  2025/2/8
package interfaces

// IMailboxMiddleware 中间件
type IMailboxMiddleware interface {
	MailboxStarted()
	MessageReceived(evt IEvent)
}

// IMessageInvoker 处理消息
type IMessageInvoker interface {
	InvokeSystemMessage(evt IEvent)
	InvokeUserMessage(evt IEvent)
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
	Suspend() bool
	Resume() bool
}

//type IDispatcher interface {
//	Schedule(fn func()) error
//	Throughput() int // 每次处理的消息数量,达到该值后,释放cpu资源等待下次处理
//}

// MailboxProducer is a function which creates a new mailbox
//type MailboxProducer func(conf *config.WorkerConf, invoker IMessageInvoker, middlewares ...IMailboxMiddleware) IMailbox
