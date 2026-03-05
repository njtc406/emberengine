// Package mailbox
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2026/1/29 00:52
// 最后更新:  yr  2026/1/29 00:52
package job

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
)

var runtimeDebug atomic.Bool

func SetDebug(enabled bool) {
	runtimeDebug.Store(enabled)
}

type Creator func() inf.IMailboxJob
type Getter func(inf.IMailboxJob) any

type jobEntry struct {
	creator Creator
	getter  Getter
}

// jobFactory 静态注册表，请勿在运行时修改
var jobFactory = map[def.MailboxJobType]jobEntry{
	def.MailboxJobTypeRpc: {
		creator: func() inf.IMailboxJob { return NewRpcJob() },
		getter:  func(j inf.IMailboxJob) any { return j.(*RpcJob).GetPayload() },
	},
	def.MailboxJobTypeEvent: {
		creator: func() inf.IMailboxJob { return NewEventBusJob() },
		getter:  func(j inf.IMailboxJob) any { return j.(*EventBusJob).GetPayload() },
	},
	def.MailboxJobTypeTimer: {
		creator: func() inf.IMailboxJob { return NewTimerJob() },
		getter:  func(j inf.IMailboxJob) any { return j.(*TimerJob).GetPayload() },
	},
	def.MailboxJobTypeConcurrentCallback: {
		creator: func() inf.IMailboxJob { return NewConcurrentCallbackJob() },
		getter:  func(j inf.IMailboxJob) any { return j.(*ConcurrentCallbackJob).GetPayload() },
	},
	def.MailboxJobTypeSysCtl: {
		creator: func() inf.IMailboxJob { return NewSysCtlJob() },
		getter:  func(j inf.IMailboxJob) any { return j.(*SysCtlJob).GetPayload() },
	},
}

func RegisterJobFactory(jobType def.MailboxJobType, creator Creator, getter Getter) error {
	if _, ok := jobFactory[jobType]; ok {
		return fmt.Errorf("job type %d is already registered", jobType)
	}
	jobFactory[jobType] = jobEntry{creator, getter}
	return nil
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

var msgJobPool pool.IPool[*RpcJob]
var msgJobPoolOnce sync.Once

func getMsgJobPool() pool.IPool[*RpcJob] {
	msgJobPoolOnce.Do(func() {
		msgJobPool = pool.NewSyncPoolWrapper[*RpcJob](
			func() *RpcJob {
				return &RpcJob{}
			},
			func() pool.IStatsRecorder {
				if runtimeDebug.Load() {
					return pool.NewStatsRecorder("MsgJobPool")
				} else {
					return pool.NewNoStatsRecorder()
				}
			}(),
			pool.WithReset[*RpcJob](func(j *RpcJob) {
				j.Reset()
			}),
			pool.WithRef[*RpcJob](func(j *RpcJob) {
				j.Ref()
			}),
			pool.WithUnRef[*RpcJob](func(j *RpcJob) bool {
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
				if runtimeDebug.Load() {
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
				if runtimeDebug.Load() {
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
				if runtimeDebug.Load() {
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
				if runtimeDebug.Load() {
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
