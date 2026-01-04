// Package event
// @Title  并发回调事件信封
// @Description  用于投递并发任务回调的专用信封
// @Author  yr  2026/1/4
// @Update  yr  2026/1/4
package event

import (
	"context"
	"sync"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
)

// CallbackEnvelope 并发回调内部事件信封
type CallbackEnvelope struct {
	InternalEnvelope[inf.IConcurrentCallback]
}

var callbackEnvelopePool pool.IPool[*CallbackEnvelope]
var callbackEnvelopePoolOnce sync.Once

func getCallbackEnvelopePool() pool.IPool[*CallbackEnvelope] {
	callbackEnvelopePoolOnce.Do(func() {
		callbackEnvelopePool = pool.NewSyncPoolWrapper(
			func() *CallbackEnvelope { return &CallbackEnvelope{} },
			func() pool.IStatsRecorder {
				if config.IsDebug() {
					return pool.NewStatsRecorder("callbackEnvelopePool")
				}
				return pool.NewNoStatsRecorder()
			}(),
			pool.WithRef(func(e *CallbackEnvelope) { e.Ref() }),
			pool.WithUnRef(func(e *CallbackEnvelope) bool { return e.UnRef() }),
			pool.WithReset(func(e *CallbackEnvelope) { e.Reset() }),
		)
	})
	return callbackEnvelopePool
}

// NewCallbackEnvelope 创建并发回调事件包装
//
// 参数：
//   - callback: 回调实例
func NewCallbackEnvelope(callback inf.IConcurrentCallback) *CallbackEnvelope {
	e := getCallbackEnvelopePool().Get()
	e.Type = ServiceConcurrentCallback
	e.Priority = def.PriorityNormal
	e.DispatcherKey = ""
	e.Payload = callback
	return e
}

// NewCallbackEnvelopeWithName 创建并发回调事件包装（带分发键）
//
// 参数：
//   - callback: 回调实例
//   - dispatcherKey: 分发键
func NewCallbackEnvelopeWithName(callback inf.IConcurrentCallback, dispatcherKey string) *CallbackEnvelope {
	e := getCallbackEnvelopePool().Get()
	e.Type = ServiceConcurrentCallback
	e.Priority = def.PriorityNormal
	e.DispatcherKey = dispatcherKey
	e.Payload = callback
	return e
}

// Release 释放信封回对象池
//
// 注意：Callback 的生命周期由调用方管理，Envelope 不负责释放 Callback
func (e *CallbackEnvelope) Release() {
	getCallbackEnvelopePool().Put(e)
}

// DoCallback 实现 IConcurrentCallback 接口，便于处理时直接调用
func (e *CallbackEnvelope) DoCallback(ctx context.Context) {
	if e.Payload != nil {
		e.Payload.DoCallback(ctx)
	}
}

// GetName 实现 INamed 接口
func (e *CallbackEnvelope) GetName() string {
	if e.Payload != nil {
		return e.Payload.GetName()
	}
	return ""
}
