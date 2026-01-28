// Package interfaces
// @Title  title
// @Description  desc
// @Author  yr  2025/2/8
// @Update  yr  2025/2/8
package interfaces

import (
	"context"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/dto"
)

// IMailbox interface is used to enqueue messages to the mailbox
type IMailbox interface {
	IMailboxChannel
	Start()
	// BeginStop 发起停止（非阻塞）：停止接收新消息并唤醒 worker 退出。
	BeginStop()
	// Wait 等待 mailbox 完全停止（阻塞）。
	Wait()
	// Stop 便捷方法：BeginStop + Wait。
	// TODO 这里的改造可能是多余的，stop不应该由service自身发起,应该由外部的管理器来发起,比如node的daemon服务,服务本身收到停止消息时，如果是
	// TODO 基础服务，那么立即向daemon服务send一条kill自己的消息，这样可以精确控制只关闭某个服务，当然也可以直接向daemon发送一条定向关闭某服务消息
	Stop()
	Suspend() bool // 挂起邮箱, 邮箱挂起后, 不再接收紧急以下的任何消息
	Resume() bool  // 恢复邮箱, 邮箱恢复后, 可以接收紧急以下的消息
}

type IMailboxWorker interface {
	Start()
	// BeginStop 发起停止（非阻塞）：停止接收新消息并让 run 循环退出。
	BeginStop()
	// Wait 等待 worker 完全退出。
	Wait()
	// Stop 便捷方法：BeginStop + Wait。
	Stop()

	GetWorkerId() int
	// GetJobLen 获取当前队列中的任务数量
	GetJobLen() int

	// SubmitJob 提交任务
	SubmitJob(job IMailboxJob) error
}

type IMailboxJob interface {
	// SetContext 设置上下文
	SetContext(ctx context.Context)
	// SetDeadline 设置截止时间
	SetDeadline(t int64)
	// SetPriority 设置优先级
	SetPriority(priority def.Priority)
	// SetDispatcherKey 设置分发key,用于将job分发给不同的worker
	SetDispatcherKey(key string)
	// SetType 设置类型，用于分发到不同 handler
	SetType(jobType def.MailboxJobType)
	// SetMiddlewareContext 设置中间件上下文
	SetMiddlewareContext(mctx IMiddlewareContext)

	// GetType 类型，用于分发到不同 handler
	GetType() def.MailboxJobType
	// GetPriority 获取优先级
	GetPriority() def.Priority
	// GetDispatcherKey 获取分发key,用于将job分发给不同的worker
	GetDispatcherKey() string
	// GetContext 获取上下文
	GetContext() context.Context
	// GetDeadline 获取截止时间
	GetDeadline() int64
	// GetMiddlewareContext 获取中间件上下文
	GetMiddlewareContext() IMiddlewareContext

	// Release 释放job
	Release()
}

// IMailboxChannel 消息接口
type IMailboxChannel interface {
	PostJob(job IMailboxJob) error
}

// IMessageInvoker 处理消息
type IMessageInvoker interface {
	GetServiceName() string
	ExecuteJob(ctx context.Context, job IMailboxJob) error
	EscalateFailure(ctx context.Context, reason interface{}, job IMailboxJob)
}

type IListener interface {
	IMailboxChannel
	IServer
}

// ================TODO 下面这些还未验证===================

type IMailboxStatistics interface {
	GetPriorityQueueLen(priority def.Priority) int
	GetTotalQueueLen() int
	GetPriorityStatistics() map[def.Priority]int
	GetSchedulerStatistics() map[string]interface{}
}

// IMiddlewareContext 中间件上下文接口
//
// 提供中间件执行期间的上下文信息和数据传递能力。
// 每个消息处理周期创建一个新的上下文实例。
type IMiddlewareContext interface {
	// Context 获取原始 context.Context
	Context() context.Context

	// Job 获取当前处理的作业
	Job() IMailboxJob

	// ServiceName 获取所属服务名
	ServiceName() string

	// Set 存储键值对，用于中间件间传递数据
	Set(key string, value interface{})

	// Get 获取存储的值
	Get(key string) (interface{}, bool)

	// GetString 获取字符串值
	GetString(key string) string

	// GetInt 获取整数值
	GetInt(key string) int

	// GetBool 获取布尔值
	GetBool(key string) bool

	// StartTime 获取消息开始处理的时间
	StartTime() time.Time

	// Elapsed 获取已经过的时间
	Elapsed() time.Duration
}

// IMailboxMiddleware 中间件接口
//
// 中间件采用洋葱模型，支持前置拦截（OnReceive）和后置处理（OnComplete）。
// 执行顺序：OnReceive 按注册顺序执行，OnComplete 按逆序执行。
//
// 典型使用场景：
//   - 限流：在 OnReceive 中检查速率，超限则 Reject
//   - 熔断：在 OnReceive 中检查熔断状态，开启则 Reject
//   - 统计：在 OnReceive 记录开始，在 OnComplete 记录结束和耗时
//   - 日志：在 OnReceive/OnComplete 中记录消息流转
//   - 追踪：在 OnReceive 中注入 trace，在 OnComplete 中结束 span
type IMailboxMiddleware interface {
	// Name 返回中间件名称，用于日志和调试
	Name() string

	// OnStart 当 Mailbox 启动时调用
	OnStart()

	// OnStop 当 Mailbox 停止时调用
	OnStop()

	// OnReceive 消息入队前调用
	//
	// 返回值决定后续行为：
	//   - Continue(): 继续执行后续中间件
	//   - Reject(err): 拒绝消息，返回错误给调用方
	//   - Skip(): 跳过后续中间件，直接入队
	//
	// 注意：此方法应该快速返回，避免阻塞投递线程
	OnReceive(mctx IMiddlewareContext) dto.MiddlewareResult

	// OnComplete 消息处理完成后调用
	//
	// 参数：
	//   - mctx: 中间件上下文（与 OnReceive 同一实例）
	//   - err: 消息处理过程中的错误（nil 表示成功）
	//   - panic: 如果处理过程发生 panic，这里会传入 recover 的值
	//
	// 注意：即使 OnReceive 返回 Reject，OnComplete 也不会被调用
	OnComplete(mctx IMiddlewareContext, err error, panicVal interface{})
}

// IMiddlewareChain 中间件链接口
type IMiddlewareChain interface {
	// Add 添加中间件
	Add(middleware IMailboxMiddleware)

	// Remove 移除中间件
	Remove(name string) bool

	// ExecuteOnReceive 执行所有中间件的 OnReceive
	// 返回最终结果和创建的上下文（用于后续 OnComplete）
	ExecuteOnReceive(ctx context.Context, job IMailboxJob, serviceName string) (dto.MiddlewareResult, IMiddlewareContext)

	// ExecuteOnComplete 执行所有中间件的 OnComplete（逆序）
	ExecuteOnComplete(mctx IMiddlewareContext, err error, panicVal interface{})

	// Start 启动所有中间件
	Start()

	// Stop 停止所有中间件
	Stop()
}

// ISuspendPolicy 挂起策略接口
//
// 定义 Mailbox 挂起时的准入控制规则。Mailbox 挂起后，默认仅接受紧急级别及以上的消息，
// 但某些特殊消息（如 RPC reply、回调等）需要放行以避免死锁。
//
// 该策略是 Mailbox 的内建机制，固定在所有中间件之前执行，确保安全边界始终生效。
// 用户可通过实现此接口来扩展或覆盖默认放行规则。
type ISuspendPolicy interface {
	// ShouldAllow 判断挂起状态下是否允许该事件通过。
	// 返回 true 表示放行，false 表示拒绝。
	ShouldAllow(job IMailboxJob) bool
}
