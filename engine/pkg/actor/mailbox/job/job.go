// Package job
//
// # Job 所有权契约（Ownership Contract）
//
// 本包定义 Mailbox 子系统所使用的 Job 对象与对应对象池。所有 Job 对象都遵循
// **单一所有权 + 一次释放**模型：
//
//	new (pool.Get)  -->  ownership @ producer
//	     │
//	     ▼
//	Mailbox.PostJob ─ 成功 ──> ownership @ Worker  ──> Worker.execute ──> Job.Release()
//	     │
//	     └─ 失败 (Suspended / 中间件 Reject / Dispatch 失败)
//	            └─ Mailbox 内部 discardJob() 负责 OnJobDiscarded + Release
//
// 关键不变量：
//
//  1. Job 对象**所有权**始终由 **唯一**一方持有，禁止拷贝、共享。
//  2. `Job.Release()` 是**唯一**的所有权终止入口，幂等：底层 DataRef CAS 保证
//     重复 Release 不会双重 Put 回 sync.Pool。
//  3. 调用方**绝不应**在 `Mailbox.PostJob` 之后再调用 Release —— 无论成功与
//     失败，所有权已经转移给 Mailbox 子系统（成功路径上由 Worker 释放，失败
//     路径上由 Mailbox.discardJob 释放）。详见 [interfaces.IMailbox] 文档。
//
// # Payload 所有权（与 Job 独立）
//
// `Job[T].payload`（envelope/event/callback 等）通常拥有自己的对象池与 ref-count
// （`dto.DataRef`）。**Job 与 payload 的生命周期是正交的**：
//   - 例如 `RpcJob.payload`（IEnvelope）可能被上游业务保留作"响应回调引用"，
//     此时 RpcJob 已经被 Worker 释放，但 envelope 仍处于 ref 状态；
//   - 反之 envelope 可能在业务逻辑中先被释放（`envelope.Release()`），随后
//     RpcJob 才被 Worker 释放。
//
// 因此本包**不**对 payload 做"释放时检查 IsRef"之类的告警，避免给业务带来
// 误导信号；payload 自身的池统计（`pool.GetPoolStats()`）会暴露真实泄漏。
//
// # 池统计
//
// 所有 Job 池在 debug 模式下（`SetDebug(true)`）会注册到全局 stats registry，
// 通过 `pool.GetPoolStats()` 可观察 hit/miss/in-use 计数；release 模式下使用
// no-op recorder，零开销。
package job

import (
	"context"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
)

// Job 是所有具体 Job 类型的公共字段载体，不直接对外暴露。具体类型通过组合
// `Job[T]` 并提供自己的 `Release()` 来选择所属对象池。
//
// `dto.DataRef` 内嵌字段提供双重释放保护：池在 Get 时通过 Ref() 标记，Put 时
// 通过 UnRef() CAS；任何重复 Release 在 CAS 失败后被丢弃。这是池的实现细节，
// 业务无须关心。
type Job[T any] struct {
	dto.DataRef
	// 类型，用于分发到不同 handler
	Type def.MailboxJobType
	// 优先级
	Priority def.Priority
	// 分发 key，用于将 job 分发给不同的 worker
	DispatcherKey string
	// 负载数据；其生命周期与 Job 独立，详见包注释。
	payload T
	// 读写模式标记（实现 IRWModeJob 接口）。
	// 零值 RWModeWrite 保证未标记时默认为写操作。
	rwMode def.RWMode

	ctx      context.Context
	deadline int64
	mctx     inf.IMiddlewareContext
}

// Reset 清零所有可变字段，由对象池在 Get/Put 时调用。
//
// 严禁业务直接调用 Reset —— 对象一旦从池取出即由调用方独占所有权直到 Release。
func (j *Job[T]) Reset() {
	j.Type = def.MailboxJobTypeNone
	j.Priority = def.PriorityNormal
	j.DispatcherKey = ""
	j.rwMode = def.RWModeWrite
	var zero T
	j.payload = zero
	j.ctx = nil
	j.deadline = 0
	j.mctx = nil
}

func (j *Job[T]) SetContext(ctx context.Context)     { j.ctx = ctx }
func (j *Job[T]) SetDeadline(t int64)                { j.deadline = t }
func (j *Job[T]) SetPayload(payload T)               { j.payload = payload }
func (j *Job[T]) SetPriority(priority def.Priority)  { j.Priority = priority }
func (j *Job[T]) SetDispatcherKey(key string)        { j.DispatcherKey = key }
func (j *Job[T]) SetType(jobType def.MailboxJobType) { j.Type = jobType }
func (j *Job[T]) SetMiddlewareContext(mctx inf.IMiddlewareContext) {
	j.mctx = mctx
}
func (j *Job[T]) SetRWMode(mode def.RWMode) { j.rwMode = mode }

func (j *Job[T]) GetPayload() T                                { return j.payload }
func (j *Job[T]) GetType() def.MailboxJobType                  { return j.Type }
func (j *Job[T]) GetPriority() def.Priority                    { return j.Priority }
func (j *Job[T]) GetDispatcherKey() string                     { return j.DispatcherKey }
func (j *Job[T]) GetContext() context.Context                  { return j.ctx }
func (j *Job[T]) GetDeadline() int64                           { return j.deadline }
func (j *Job[T]) GetMiddlewareContext() inf.IMiddlewareContext { return j.mctx }
func (j *Job[T]) GetRWMode() def.RWMode                        { return j.rwMode }

// ===== 具体 Job 类型 =====
//
// 每种类型对应业务侧一类消息载体。所有类型共享 Job[T] 的字段与方法；唯一
// 差异是 payload 类型与对应的 sync.Pool（在 job_factory.go 中定义）。

// RpcJob 承载跨 service 的 RPC 消息。
type RpcJob struct{ Job[inf.IEnvelope] }

func NewRpcJob() *RpcJob {
	j := getMsgJobPool().Get()
	j.SetType(def.MailboxJobTypeRpc)
	return j
}

func (j *RpcJob) IsCancelOnContextDone() bool { return true }

func (j *RpcJob) Release() { getMsgJobPool().Put(j) }

// EventBusJob 承载 EventBus 投递的事件。
type EventBusJob struct{ Job[*actor.Event] }

func NewEventBusJob() *EventBusJob {
	j := getEventBusJobPool().Get()
	j.SetType(def.MailboxJobTypeEvent)
	return j
}

func (j *EventBusJob) Release() { getEventBusJobPool().Put(j) }

// TimerJob 承载时间轮触发的定时任务。
type TimerJob struct{ Job[timingwheel.ITimer] }

func NewTimerJob() *TimerJob {
	j := getTimerJobPool().Get()
	j.SetType(def.MailboxJobTypeTimer)
	return j
}

func (j *TimerJob) Release() { getTimerJobPool().Put(j) }

// ConcurrentCallbackJob 承载异步并发回调（与 RWMode 读路径协作）。
type ConcurrentCallbackJob struct{ Job[inf.IConcurrentCallback] }

func NewConcurrentCallbackJob() *ConcurrentCallbackJob {
	j := getConcurrentCallbackJobPool().Get()
	j.SetType(def.MailboxJobTypeConcurrentCallback)
	return j
}

func (j *ConcurrentCallbackJob) Release() { getConcurrentCallbackJobPool().Put(j) }

// SysCtlJob 承载 mailbox 内部控制命令（暂停/恢复/健康检查等）。
type SysCtlJob struct{ Job[dto.SysCmd] }

func NewSysCtlJob() *SysCtlJob {
	j := getSysCtlJobPool().Get()
	j.SetType(def.MailboxJobTypeSysCtl)
	return j
}

func (j *SysCtlJob) Release() { getSysCtlJobPool().Put(j) }
