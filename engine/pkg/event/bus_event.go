// Package event
// @Title  消息事件
// @Description  EventBus 内部使用的事件包装，将 ctx 和 actor.Event 配对
// @Author  yr  2025/4/10
// @Update  yr  2025/12/26
package event

import (
	"context"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/utils/xcontext"
)

// busEvent 是 EventBus 内部使用的事件包装
// 将 context.Context 和 *actor.Event 配对，用于在 EventBus 内部传递
// 注意：这不是 IEvent 实现，只是内部数据结构
type busEvent struct {
	ctx   context.Context
	event *actor.Event
}

// newBusEvent 创建新的 busEvent
func newBusEvent(ctx context.Context, e *actor.Event) *busEvent {
	return &busEvent{
		ctx:   ctx,
		event: e,
	}
}

// getDispatcherKey 从 actor.Event 获取 dispatcher key（现在是显式字段）
func (be *busEvent) getDispatcherKey() string {
	if be.event == nil {
		return ""
	}
	return be.event.DispatcherKey
}

// getPriority 从 actor.Event 获取优先级（现在是显式字段）
func (be *busEvent) getPriority() def.Priority {
	if be.event == nil {
		return def.PriorityNormal
	}
	return def.Priority(be.event.Priority)
}

// buildContextFromHeaders 从 actor.Event 的 ContextHeaders 构建 context
func buildContextFromHeaders(headers map[string]string) context.Context {
	ctx := xcontext.New(context.Background())
	for k, v := range headers {
		ctx.AddHeader(k, v)
	}
	return ctx
}
