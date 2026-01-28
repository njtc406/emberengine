// Package mailbox
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2026/1/29 00:18
// 最后更新:  yr  2026/1/29 00:18
package mailbox

import (
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
}

func (j *Job[T]) Reset() {
	j.Type = def.MailboxJobTypeNone
	j.Priority = def.PriorityNormal
	j.DispatcherKey = ""
	var zero T
	j.payload = zero
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

// MsgJob rpc消息任务
type MsgJob struct {
	Job[inf.IEnvelope]
}

func NewMsgJob() *MsgJob {
	return getMsgJobPool().Get()
}

func (j *MsgJob) Release() {
	getMsgJobPool().Put(j)
}

type EventBusJob struct {
	Job[*actor.Event]
}

func NewEventBusJob() *EventBusJob {
	return getEventBusJobPool().Get()
}

func (j *EventBusJob) Release() {
	getEventBusJobPool().Put(j)
}

type TimerJob struct {
	Job[*timingwheel.ITimer]
}

func NewTimerJob() *TimerJob {
	return getTimerJobPool().Get()
}

func (j *TimerJob) Release() {
	getTimerJobPool().Put(j)
}

type ConcurrentCallbackJob struct {
	Job[inf.IConcurrentCallback]
}

func NewConcurrentCallbackJob() *ConcurrentCallbackJob {
	return getConcurrentCallbackJobPool().Get()
}

func (j *ConcurrentCallbackJob) Release() {
	getConcurrentCallbackJobPool().Put(j)
}

type SysCtlJob struct {
	Job[dto.SysCmd]
}

func NewSysCtlJob() *SysCtlJob {
	return getSysCtlJobPool().Get()
}

func (j *SysCtlJob) Release() {
	getSysCtlJobPool().Put(j)
}

type InternalEventJob struct {
	Job[inf.IEvent]
}

func NewInternalEventJob() *InternalEventJob {
	return getInternalEventJobPool().Get()
}

func (j *InternalEventJob) Release() {
	getInternalEventJobPool().Put(j)
}
