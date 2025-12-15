// Package interfaces
// @Title  title
// @Description  desc
// @Author  yr  2025/2/8
// @Update  yr  2025/2/8
package interfaces

import (
	"context"

	"github.com/njtc406/emberengine/engine/pkg/def"
)

// IMailboxMiddleware 中间件
type IMailboxMiddleware interface {
	MailboxStarted()                                  // 当Mailbox启动时调用
	MessageReceived(ctx context.Context, evt IEvent)  // 当有消息到达时调用
	MessageProcessed(ctx context.Context, evt IEvent) // 当消息处理完成时调用
}

// IMessageInvoker 处理消息
type IMessageInvoker interface {
	GetServiceName() string
	InvokeMessage(ctx context.Context, evt IEvent)
	EscalateFailure(ctx context.Context, reason interface{}, evt IEvent)
}

// IMailboxChannel 消息接口
type IMailboxChannel interface {
	PostMessage(ctx context.Context, evt IEvent) error
}

// IMailbox interface is used to enqueue messages to the mailbox
type IMailbox interface {
	IMailboxChannel
	Start()
	Stop()
	Suspend() bool // 挂起邮箱, 邮箱挂起后, 不再接收紧急以下的任何消息
	Resume() bool  // 恢复邮箱, 邮箱恢复后, 可以接收紧急以下的消息
}

type IMailboxWorker interface {
	Start()
	Stop()
	SubmitEvent(ctx context.Context, evt IEvent) error
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
