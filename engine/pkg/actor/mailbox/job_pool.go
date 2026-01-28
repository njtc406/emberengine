// Package mailbox
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2026/1/29 00:52
// 最后更新:  yr  2026/1/29 00:52
package mailbox

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
)

type jobCreator func() inf.IMailboxJob

var (
	jobFactoryOnce sync.Once
	jobFactoryVal  atomic.Value // map[def.MailboxJobType]jobCreator
)

func initJobFactory() {
	defaults := map[def.MailboxJobType]jobCreator{
		def.MailboxJobTypeRpc:                func() inf.IMailboxJob { return NewMsgJob() },
		def.MailboxJobTypeEvent:              func() inf.IMailboxJob { return NewEventBusJob() },
		def.MailboxJobTypeInternalEvent:      func() inf.IMailboxJob { return NewInternalEventJob() },
		def.MailboxJobTypeTimer:              func() inf.IMailboxJob { return NewTimerJob() },
		def.MailboxJobTypeConcurrentCallback: func() inf.IMailboxJob { return NewConcurrentCallbackJob() },
		def.MailboxJobSysCtl:                 func() inf.IMailboxJob { return NewSysCtlJob() },
	}
	jobFactoryVal.Store(defaults)
}

func getJobFactory() map[def.MailboxJobType]jobCreator {
	jobFactoryOnce.Do(initJobFactory)
	return jobFactoryVal.Load().(map[def.MailboxJobType]jobCreator)
}

// RegisterJobFactory 注册一个新的 job 创建器。
// 说明：实现为 copy-on-write，因此 CreateJob() 热路径无锁。
// replace=false 且已存在时返回 error。
func RegisterJobFactory(jobType def.MailboxJobType, creator jobCreator, replace bool) error {
	if creator == nil {
		return fmt.Errorf("jobFactory: nil creator for type %v", jobType)
	}
	jobFactoryOnce.Do(initJobFactory)

	old := jobFactoryVal.Load().(map[def.MailboxJobType]jobCreator)
	if _, exists := old[jobType]; exists && !replace {
		return fmt.Errorf("jobFactory: creator already registered for type %v", jobType)
	}

	newMap := make(map[def.MailboxJobType]jobCreator, len(old)+1)
	for k, v := range old {
		newMap[k] = v
	}
	newMap[jobType] = creator
	jobFactoryVal.Store(newMap)
	return nil
}

// MustRegisterJobFactory 类似 RegisterJobFactory，但失败直接 panic，适合在 init() 中使用。
func MustRegisterJobFactory(jobType def.MailboxJobType, creator jobCreator, replace bool) {
	if err := RegisterJobFactory(jobType, creator, replace); err != nil {
		panic(err)
	}
}

// CreateJob 按类型创建一个 job。
// 未注册则返回 (nil, false)。
func CreateJob(jobType def.MailboxJobType) (inf.IMailboxJob, bool) {
	creator, ok := getJobFactory()[jobType]
	if !ok {
		return nil, false
	}
	return creator(), true
}

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
