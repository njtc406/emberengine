// Package job
package job

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
)

// runtimeDebug 控制池统计是否启用。debug=false 时使用 no-op recorder，
// 池统计零开销；debug=true 时注册到全局 stats registry，便于排查泄漏。
var runtimeDebug atomic.Bool

func SetDebug(enabled bool) { runtimeDebug.Store(enabled) }

// ===== Job 工厂注册表 =====
//
// jobFactory 在节点启动早期由 init/手动 RegisterJobFactory 完成填充，首次
// CreateJob 调用（或显式 FreezeJobFactory）会冻结注册表，之后不再允许变更。

var (
	jobFactoryFrozen atomic.Bool
	jobFactoryMu     sync.Mutex
)

type Creator func() inf.IMailboxJob
type Getter func(inf.IMailboxJob) any

type jobEntry struct {
	creator Creator
	getter  Getter
}

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
	jobFactoryMu.Lock()
	defer jobFactoryMu.Unlock()
	if jobFactoryFrozen.Load() {
		return fmt.Errorf("job factory is frozen, cannot register type %d after first use", jobType)
	}
	if _, ok := jobFactory[jobType]; ok {
		return fmt.Errorf("job type %d is already registered", jobType)
	}
	jobFactory[jobType] = jobEntry{creator, getter}
	return nil
}

// FreezeJobFactory 显式冻结 job factory 注册表。
//
// 推荐调用点：Node.Start 在用户 init 完成后、业务服务启动前。
// 用途：避免"某个包先调用 CreateJob 触发隐式冻结导致其他 init 中的
// RegisterJobFactory 失败"的 init 顺序依赖问题。
//
// 重复调用安全。
func FreezeJobFactory() {
	if jobFactoryFrozen.Load() {
		return
	}
	jobFactoryMu.Lock()
	jobFactoryFrozen.Store(true)
	jobFactoryMu.Unlock()
}

// CreateJob 按类型创建一个 job。首次调用时冻结注册表。未注册返回 (nil, false)。
func CreateJob(jobType def.MailboxJobType) (inf.IMailboxJob, bool) {
	freezeIfNeeded()
	entry, ok := jobFactory[jobType]
	if !ok {
		return nil, false
	}
	return entry.creator(), true
}

// GetJobPayload 根据 job 类型获取其负载（any 形式）。未注册返回 nil。
func GetJobPayload(job inf.IMailboxJob) any {
	freezeIfNeeded()
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

func freezeIfNeeded() {
	if !jobFactoryFrozen.Load() {
		jobFactoryMu.Lock()
		jobFactoryFrozen.Store(true)
		jobFactoryMu.Unlock()
	}
}

// ===== 具体 Job 对象池 =====
//
// 所有 Job 子类型共享相同的"池接入模式"：Reset / Ref（CAS 标记 in-use）/
// UnRef（CAS 释放 in-use，幂等保护双重 Release）。原本每个类型的 30 行
// `pool.NewSyncPoolWrapper(...)` 样板被合并为下面单一 `newJobPool` 模板。

// poolableJob 描述池中对象必须实现的协议。所有 Job 子类型通过组合
// `Job[T]` 自动满足（Reset/Ref/UnRef 由 Job 与 dto.DataRef 提供）。
type poolableJob interface {
	Reset()
	Ref()
	UnRef() bool
}

func newJobPool[T poolableJob](name string, ctor func() T) pool.IPool[T] {
	recorder := pool.NewSwitchableStatsRecorder(name, runtimeDebug.Load)
	return pool.NewSyncPoolWrapper[T](
		ctor,
		recorder,
		pool.WithReset[T](func(j T) { j.Reset() }),
		pool.WithRef[T](func(j T) { j.Ref() }),
		pool.WithUnRef[T](func(j T) bool { return j.UnRef() }),
	)
}

// ===== 各 Job 类型的池单例 =====
//
// 池在首次使用时通过 sync.Once 懒加载，可被 init 间序无关地访问。
// 使用 helper `newJobPool[T]` 后每个池仅 3 行。

var (
	msgJobPool     pool.IPool[*RpcJob]
	msgJobPoolOnce sync.Once

	eventBusJobPool     pool.IPool[*EventBusJob]
	eventBusJobPoolOnce sync.Once

	timerJobPool     pool.IPool[*TimerJob]
	timerJobPoolOnce sync.Once

	concurrentCallbackJobPool     pool.IPool[*ConcurrentCallbackJob]
	concurrentCallbackJobPoolOnce sync.Once

	sysCtlJobPool     pool.IPool[*SysCtlJob]
	sysCtlJobPoolOnce sync.Once
)

func getMsgJobPool() pool.IPool[*RpcJob] {
	msgJobPoolOnce.Do(func() {
		msgJobPool = newJobPool[*RpcJob]("MsgJobPool", func() *RpcJob { return &RpcJob{} })
	})
	return msgJobPool
}

func getEventBusJobPool() pool.IPool[*EventBusJob] {
	eventBusJobPoolOnce.Do(func() {
		eventBusJobPool = newJobPool[*EventBusJob]("EventBusJobPool", func() *EventBusJob { return &EventBusJob{} })
	})
	return eventBusJobPool
}

func getTimerJobPool() pool.IPool[*TimerJob] {
	timerJobPoolOnce.Do(func() {
		timerJobPool = newJobPool[*TimerJob]("TimerJobPool", func() *TimerJob { return &TimerJob{} })
	})
	return timerJobPool
}

func getConcurrentCallbackJobPool() pool.IPool[*ConcurrentCallbackJob] {
	concurrentCallbackJobPoolOnce.Do(func() {
		concurrentCallbackJobPool = newJobPool[*ConcurrentCallbackJob]("ConcurrentCallbackJobPool", func() *ConcurrentCallbackJob { return &ConcurrentCallbackJob{} })
	})
	return concurrentCallbackJobPool
}

func getSysCtlJobPool() pool.IPool[*SysCtlJob] {
	sysCtlJobPoolOnce.Do(func() {
		sysCtlJobPool = newJobPool[*SysCtlJob]("SysCtlJobPool", func() *SysCtlJob { return &SysCtlJob{} })
	})
	return sysCtlJobPool
}
