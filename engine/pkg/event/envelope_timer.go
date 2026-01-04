// Package event
// @Title  定时器事件信封
// @Description  用于投递定时器回调的专用信封
// @Author  yr  2026/1/4
// @Update  yr  2026/1/4
package event

import (
	"sync"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
)

// TimerEnvelope 定时器内部事件信封
type TimerEnvelope struct {
	InternalEnvelope[timingwheel.ITimer]
}

var timerEnvelopePool pool.IPool[*TimerEnvelope]
var timerEnvelopePoolOnce sync.Once

func getTimerEnvelopePool() pool.IPool[*TimerEnvelope] {
	timerEnvelopePoolOnce.Do(func() {
		timerEnvelopePool = pool.NewSyncPoolWrapper(
			func() *TimerEnvelope { return &TimerEnvelope{} },
			func() pool.IStatsRecorder {
				if config.IsDebug() {
					return pool.NewStatsRecorder("timerEnvelopePool")
				}
				return pool.NewNoStatsRecorder()
			}(),
			pool.WithRef(func(e *TimerEnvelope) { e.Ref() }),
			pool.WithUnRef(func(e *TimerEnvelope) bool { return e.UnRef() }),
			pool.WithReset(func(e *TimerEnvelope) { e.Reset() }),
		)
	})
	return timerEnvelopePool
}

// NewTimerEnvelope 创建定时器事件包装
//
// 参数：
//   - timer: 定时器实例
//   - dispatcherKey: 分发键（通常使用 timer.GetName() 保证相同回调在同一 worker 处理）
func NewTimerEnvelope(timer timingwheel.ITimer, dispatcherKey string) *TimerEnvelope {
	e := getTimerEnvelopePool().Get()
	e.Type = ServiceTimerCallback
	e.Priority = def.PriorityNormal
	e.DispatcherKey = dispatcherKey
	e.Payload = timer
	return e
}

// Release 释放信封回对象池
//
// 注意：Timer 有自己的生命周期管理，Envelope 不负责释放 Timer
func (e *TimerEnvelope) Release() {
	getTimerEnvelopePool().Put(e)
}
