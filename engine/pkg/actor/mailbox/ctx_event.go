// Package mailbox
// @Title  上下文事件包装器
// @Description  包装 context.Context 和 IEvent，用于在队列中同时传递两者
// @Author  yr  2025/12/25
// @Update  yr  2025/12/25
package mailbox

import (
	"context"
	"sync"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
)

// CtxEvent 包装 context 和 event，用于在 mailbox 队列中传递
// 实现 IEvent 接口以便放入队列
type CtxEvent struct {
	Ctx   context.Context
	Event inf.IEvent
}

// 实现 IEvent 接口（委托给内部 Event）

func (c *CtxEvent) GetType() int32 {
	return c.Event.GetType()
}

func (c *CtxEvent) GetPriority() def.Priority {
	return c.Event.GetPriority()
}

func (c *CtxEvent) GetDispatcherKey() string {
	return c.Event.GetDispatcherKey()
}

func (c *CtxEvent) IsRef() bool {
	return c.Event.IsRef()
}

func (c *CtxEvent) Ref() {
	c.Event.Ref()
}

func (c *CtxEvent) UnRef() bool {
	return c.Event.UnRef()
}

func (c *CtxEvent) Release() {
	// CtxEvent 只是包装器，不负责释放内部 event
	// 内部 event 由使用方（如 InvokeMessage 之后的业务逻辑）负责释放
	c.Event = nil
	c.Ctx = nil
	// 回收包装器本身
	getCtxEventPool().Put(c)
}

// 对象池

var ctxEventPool pool.IPool[*CtxEvent]
var ctxEventPoolOnce sync.Once

func getCtxEventPool() pool.IPool[*CtxEvent] {
	ctxEventPoolOnce.Do(func() {
		ctxEventPool = pool.NewSyncPoolWrapper(
			func() *CtxEvent {
				return &CtxEvent{}
			},
			func() pool.IStatsRecorder {
				if config.IsDebug() {
					return pool.NewStatsRecorder("ctxEventPool")
				} else {
					return pool.NewNoStatsRecorder()
				}
			}(),
			pool.WithReset(func(t *CtxEvent) {
				t.Ctx = nil
				t.Event = nil
			}),
			pool.WithRef(func(t *CtxEvent) {
				// CtxEvent 本身不需要引用计数，依赖内部 Event 的计数
			}),
			pool.WithUnRef(func(t *CtxEvent) bool {
				// CtxEvent 本身不需要引用计数
				return true
			}),
		)
	})
	return ctxEventPool
}

// NewCtxEvent 从对象池获取一个 CtxEvent
func NewCtxEvent(ctx context.Context, evt inf.IEvent) *CtxEvent {
	ce := getCtxEventPool().Get()
	ce.Ctx = ctx
	ce.Event = evt
	return ce
}

// UnwrapCtxEvent 解包 CtxEvent，返回 ctx 和原始 event
// 如果不是 CtxEvent，返回 background context 和原始 event
func UnwrapCtxEvent(evt inf.IEvent) (context.Context, inf.IEvent) {
	if ce, ok := evt.(*CtxEvent); ok {
		return ce.Ctx, ce.Event
	}
	return context.Background(), evt
}
