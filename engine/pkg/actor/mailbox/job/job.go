// Package mailbox
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2026/1/29 00:18
// 最后更新:  yr  2026/1/29 00:18
package job

import (
	"context"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
)

type Job[T any] struct {
	dto.DataRef
	// 类型，用于分发到不同 handler
	Type def.MailboxJobType
	// 优先级
	Priority def.Priority
	// 分发key,用于将job分发给不同的worker
	DispatcherKey string
	// 负载数据
	payload T

	ctx      context.Context
	deadline time.Time
	mctx     inf.IMiddlewareContext
}

func (j *Job[T]) Reset() {
	j.Type = def.MailboxJobTypeNone
	j.Priority = def.PriorityNormal
	j.DispatcherKey = ""
	var zero T
	j.payload = zero
}

func (j *Job[T]) SetContext(ctx context.Context) {
	j.ctx = ctx
}

func (j *Job[T]) SetDeadline(t time.Time) {
	j.deadline = t
}

func (j *Job[T]) SetPayload(payload T) {
	j.payload = payload
}

func (j *Job[T]) SetPriority(priority def.Priority) {
	j.Priority = priority
}

func (j *Job[T]) SetDispatcherKey(key string) {
	j.DispatcherKey = key
}

func (j *Job[T]) SetType(jobType def.MailboxJobType) {
	j.Type = jobType
}

// SetMiddlewareContext 设置中间件上下文
func (j *Job[T]) SetMiddlewareContext(mctx inf.IMiddlewareContext) {
	j.mctx = mctx
}

func (j *Job[T]) GetPayload() T {
	return j.payload
}

func (j *Job[T]) GetType() def.MailboxJobType {
	return j.Type
}

func (j *Job[T]) GetPriority() def.Priority {
	return j.Priority
}

func (j *Job[T]) GetDispatcherKey() string {
	return j.DispatcherKey
}

func (j *Job[T]) GetContext() context.Context {
	return j.ctx
}

func (j *Job[T]) GetDeadline() time.Time {
	return j.deadline
}

func (j *Job[T]) GetMiddlewareContext() inf.IMiddlewareContext {
	return j.mctx
}

// RpcJob rpc消息任务
type RpcJob struct {
	Job[inf.IEnvelope]
}

func NewRpcJob() *RpcJob {
	j := getMsgJobPool().Get()
	j.SetType(def.MailboxJobTypeRpc)
	return j
}

func (j *RpcJob) Release() {
	getMsgJobPool().Put(j)
}

type EventBusJob struct {
	Job[*actor.Event]
}

func NewEventBusJob() *EventBusJob {
	j := getEventBusJobPool().Get()
	j.SetType(def.MailboxJobTypeEvent)
	return j
}

func (j *EventBusJob) Release() {
	getEventBusJobPool().Put(j)
}

type TimerJob struct {
	Job[timingwheel.ITimer]
}

func NewTimerJob() *TimerJob {
	j := getTimerJobPool().Get()
	j.SetType(def.MailboxJobTypeTimer)
	return j
}

func (j *TimerJob) Release() {
	getTimerJobPool().Put(j)
}

type ConcurrentCallbackJob struct {
	Job[inf.IConcurrentCallback]
}

func NewConcurrentCallbackJob() *ConcurrentCallbackJob {
	j := getConcurrentCallbackJobPool().Get()
	j.SetType(def.MailboxJobTypeConcurrentCallback)
	return j
}

func (j *ConcurrentCallbackJob) Release() {
	getConcurrentCallbackJobPool().Put(j)
}

type SysCtlJob struct {
	Job[dto.SysCmd]
}

func NewSysCtlJob() *SysCtlJob {
	j := getSysCtlJobPool().Get()
	j.SetType(def.MailboxJobTypeSysCtl)
	return j
}

func (j *SysCtlJob) Release() {
	getSysCtlJobPool().Put(j)
}

type InternalEventJob struct {
	Job[inf.IEvent]
}

func NewInternalEventJob() *InternalEventJob {
	j := getInternalEventJobPool().Get()
	j.SetType(def.MailboxJobTypeInternalEvent)
	return j
}

func (j *InternalEventJob) Release() {
	getInternalEventJobPool().Put(j)
}
