// Package event
// @Title  泛型事件信封
// @Description  统一的事件包装器，用于 mailbox 投递
// @Author  yr  2026/1/4
// @Update  yr  2026/1/4
package event

import (
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/dto"
)

// InternalEnvelope 内部事件包装器（泛型）
//
// 与 msgenvelope.MsgEnvelope 的区别：
//   - MsgEnvelope: 用于服务间 RPC 通信，包含完整的请求/响应元信息
//   - InternalEnvelope: 用于服务内部事件投递（Timer、Callback），轻量级
//
// 设计目标：
//   - Worker 只处理实现了 IEvent 的类型
//   - 业务类型（Timer、Callback）只需实现自己的业务接口
//   - 调度相关的 Type/Priority/DispatcherKey 由 Envelope 统一提供
//
// 类型参数 T：实际业务载荷类型（如 ITimer、IConcurrentCallback）
type InternalEnvelope[T any] struct {
	dto.DataRef

	Type          int32
	Priority      def.Priority
	DispatcherKey string
	Payload       T // 实际业务数据
}

// GetType 返回事件类型
func (e *InternalEnvelope[T]) GetType() int32 {
	return e.Type
}

// GetPriority 返回事件优先级
func (e *InternalEnvelope[T]) GetPriority() def.Priority {
	return e.Priority
}

// GetDispatcherKey 返回分发键
func (e *InternalEnvelope[T]) GetDispatcherKey() string {
	return e.DispatcherKey
}

// Reset 重置信封（用于对象池复用）
func (e *InternalEnvelope[T]) Reset() {
	e.Type = 0
	e.Priority = def.PriorityNormal
	e.DispatcherKey = ""
	var zero T
	e.Payload = zero
}
