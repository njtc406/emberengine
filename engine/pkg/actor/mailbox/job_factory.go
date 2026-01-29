// Package mailbox
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2026/1/29 00:52
// 最后更新:  yr  2026/1/29 00:52
package mailbox

import (
	"sync"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
)

type jobEntry struct {
	creator func() inf.IMailboxJob
	getter  func(inf.IMailboxJob) any
}

// jobFactory 静态注册表，请勿在运行时修改
var jobFactory = map[def.MailboxJobType]jobEntry{
	def.MailboxJobTypeRpc: {
		creator: func() inf.IMailboxJob { return NewMsgJob() },
		getter:  func(j inf.IMailboxJob) any { return j.(*MsgJob).GetPayload() },
	},
	def.MailboxJobTypeEvent: {
		creator: func() inf.IMailboxJob { return NewEventBusJob() },
		getter:  func(j inf.IMailboxJob) any { return j.(*EventBusJob).GetPayload() },
	},
	def.MailboxJobTypeInternalEvent: {
		creator: func() inf.IMailboxJob { return NewInternalEventJob() },
		getter:  func(j inf.IMailboxJob) any { return j.(*InternalEventJob).GetPayload() },
	},
	def.MailboxJobTypeTimer: {
		creator: func() inf.IMailboxJob { return NewTimerJob() },
		getter:  func(j inf.IMailboxJob) any { return j.(*TimerJob).GetPayload() },
	},
	def.MailboxJobTypeConcurrentCallback: {
		creator: func() inf.IMailboxJob { return NewConcurrentCallbackJob() },
		getter:  func(j inf.IMailboxJob) any { return j.(*ConcurrentCallbackJob).GetPayload() },
	},
	def.MailboxJobSysCtl: {
		creator: func() inf.IMailboxJob { return NewSysCtlJob() },
		getter:  func(j inf.IMailboxJob) any { return j.(*SysCtlJob).GetPayload() },
	},
}

// CreateJob 按类型创建一个 job。
// 未注册则返回 (nil, false)。
func CreateJob(jobType def.MailboxJobType) (inf.IMailboxJob, bool) {
	entry, ok := jobFactory[jobType]
	if !ok {
		return nil, false
	}
	return entry.creator(), true
}

// GetJobPayload 根据 job 的类型获取其负载。
// 返回值需要调用方根据 jobType 断言为具体类型。
// 未注册的类型返回 nil。
func GetJobPayload(job inf.IMailboxJob) any {
	entry, ok := jobFactory[job.GetType()]
	if !ok {
		return nil
	}
	return entry.getter(job)
}

// GetJobPayloadAs 泛型版本，直接返回指定类型。
func GetJobPayloadAs[T any](job inf.IMailboxJob) T {
	payload := GetJobPayload(job)
	if payload == nil {
		var zero T
		return zero
	}
	return payload.(T)
}

// ===============================================================

var msgJobPool pool.IPool[*MsgJob]
var msgJobPoolOnce sync.Once

func getMsgJobPool() pool.IPool[*MsgJob] {
	msgJobPoolOnce.Do(func() {
		msgJobPool = pool.NewSyncPoolWrapper[*MsgJob](
			func() *MsgJob {
				return &MsgJob{}
			},
			func() pool.IStatsRecorder {
				if config.IsDebug() {
					return pool.NewStatsRecorder("MsgJobPool")
				} else {
					return pool.NewNoStatsRecorder()
				}
			}(),
			pool.WithReset[*MsgJob](func(j *MsgJob) {
				j.Reset()
			}),
			pool.WithRef[*MsgJob](func(j *MsgJob) {
				j.Ref()
			}),
			pool.WithUnRef[*MsgJob](func(j *MsgJob) bool {
				return j.UnRef()
			}),
		)
	})
	return msgJobPool
}

var eventBusJobPool pool.IPool[*EventBusJob]
var eventBusJobPoolOnce sync.Once

func getEventBusJobPool() pool.IPool[*EventBusJob] {
	eventBusJobPoolOnce.Do(func() {
		eventBusJobPool = pool.NewSyncPoolWrapper[*EventBusJob](
			func() *EventBusJob {
				return &EventBusJob{}
			},
			func() pool.IStatsRecorder {
				if config.IsDebug() {
					return pool.NewStatsRecorder("EventBusJobPool")
				} else {
					return pool.NewNoStatsRecorder()
				}
			}(),
			pool.WithReset[*EventBusJob](func(j *EventBusJob) {
				j.Reset()
			}),
			pool.WithRef[*EventBusJob](func(j *EventBusJob) {
				j.Ref()
			}),
			pool.WithUnRef[*EventBusJob](func(j *EventBusJob) bool {
				return j.UnRef()
			}),
		)
	})
	return eventBusJobPool
}

var timerJobPool pool.IPool[*TimerJob]
var timerJobPoolOnce sync.Once

func getTimerJobPool() pool.IPool[*TimerJob] {
	timerJobPoolOnce.Do(func() {
		timerJobPool = pool.NewSyncPoolWrapper[*TimerJob](
			func() *TimerJob {
				return &TimerJob{}
			},
			func() pool.IStatsRecorder {
				if config.IsDebug() {
					return pool.NewStatsRecorder("TimerJobPool")
				} else {
					return pool.NewNoStatsRecorder()
				}
			}(),
			pool.WithReset[*TimerJob](func(j *TimerJob) {
				j.Reset()
			}),
			pool.WithRef[*TimerJob](func(j *TimerJob) {
				j.Ref()
			}),
			pool.WithUnRef[*TimerJob](func(j *TimerJob) bool {
				return j.UnRef()
			}),
		)
	})
	return timerJobPool
}

var concurrentCallbackJobPool pool.IPool[*ConcurrentCallbackJob]
var concurrentCallbackJobPoolOnce sync.Once

func getConcurrentCallbackJobPool() pool.IPool[*ConcurrentCallbackJob] {
	concurrentCallbackJobPoolOnce.Do(func() {
		concurrentCallbackJobPool = pool.NewSyncPoolWrapper[*ConcurrentCallbackJob](
			func() *ConcurrentCallbackJob {
				return &ConcurrentCallbackJob{}
			},
			func() pool.IStatsRecorder {
				if config.IsDebug() {
					return pool.NewStatsRecorder("ConcurrentCallbackJobPool")
				} else {
					return pool.NewNoStatsRecorder()
				}
			}(),
			pool.WithReset[*ConcurrentCallbackJob](func(j *ConcurrentCallbackJob) {
				j.Reset()
			}),
			pool.WithRef[*ConcurrentCallbackJob](func(j *ConcurrentCallbackJob) {
				j.Ref()
			}),
			pool.WithUnRef[*ConcurrentCallbackJob](func(j *ConcurrentCallbackJob) bool {
				return j.UnRef()
			}),
		)
	})
	return concurrentCallbackJobPool
}

var sysCtlJobPool pool.IPool[*SysCtlJob]
var sysCtlJobPoolOnce sync.Once

func getSysCtlJobPool() pool.IPool[*SysCtlJob] {
	sysCtlJobPoolOnce.Do(func() {
		sysCtlJobPool = pool.NewSyncPoolWrapper[*SysCtlJob](
			func() *SysCtlJob {
				return &SysCtlJob{}
			},
			func() pool.IStatsRecorder {
				if config.IsDebug() {
					return pool.NewStatsRecorder("SysCtlJobPool")
				} else {
					return pool.NewNoStatsRecorder()
				}
			}(),
			pool.WithReset[*SysCtlJob](func(j *SysCtlJob) {
				j.Reset()
			}),
			pool.WithRef[*SysCtlJob](func(j *SysCtlJob) {
				j.Ref()
			}),
			pool.WithUnRef[*SysCtlJob](func(j *SysCtlJob) bool {
				return j.UnRef()
			}),
		)
	})
	return sysCtlJobPool
}

var internalEventJobPool pool.IPool[*InternalEventJob]
var internalEventJobPoolOnce sync.Once

func getInternalEventJobPool() pool.IPool[*InternalEventJob] {
	internalEventJobPoolOnce.Do(func() {
		internalEventJobPool = pool.NewSyncPoolWrapper[*InternalEventJob](
			func() *InternalEventJob {
				return &InternalEventJob{}
			},
			func() pool.IStatsRecorder {
				if config.IsDebug() {
					return pool.NewStatsRecorder("InternalEventJobPool")
				} else {
					return pool.NewNoStatsRecorder()
				}
			}(),
			pool.WithReset[*InternalEventJob](func(j *InternalEventJob) {
				j.Reset()
			}),
			pool.WithRef[*InternalEventJob](func(j *InternalEventJob) {
				j.Ref()
			}),
			pool.WithUnRef[*InternalEventJob](func(j *InternalEventJob) bool {
				return j.UnRef()
			}),
		)
	})
	return internalEventJobPool
}
