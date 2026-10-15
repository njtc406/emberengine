// Package tracing 定义最小可用的分布式追踪接口。
//
// P2 阶段只预留接口和 noop 实现，不引入 OpenTelemetry SDK 依赖。
// 后续 P3 接入 OTel 时，只需提供 OTel 适配器实现这些接口。
//
// 设计要点：
//   - ITracer 负责从 context 创建/恢复 Span
//   - ISpan 负责 Span 生命周期和属性记录
//   - noopTracer/noopSpan 是零开销默认实现
//   - 通过 SetGlobalTracer 注入，框架内部通过 GlobalTracer() 获取
package tracing

import (
	"context"
	"sync/atomic"
)

// ISpan 表示一个追踪 Span。
type ISpan interface {
	// End 结束 Span。
	End()

	// SetAttribute 设置 Span 属性。
	SetAttribute(key string, value interface{})

	// RecordError 记录错误到 Span。
	RecordError(err error)

	// SpanContext 返回包含 Span 信息的 context（用于向下传播）。
	SpanContext() context.Context
}

// ITracer 负责创建 Span。
type ITracer interface {
	// Start 创建新 Span 并返回携带 Span 的 context。
	// operationName 标识操作（如 "rpc.Call", "event.Publish"）。
	Start(ctx context.Context, operationName string) (context.Context, ISpan)

	// IsEnabled 返回 tracer 是否启用（noop 返回 false）。
	IsEnabled() bool
}

// --- noop 实现 ---

type noopSpan struct {
	ctx context.Context
}

func (n *noopSpan) End()                                 {}
func (n *noopSpan) SetAttribute(_ string, _ interface{}) {}
func (n *noopSpan) RecordError(_ error)                  {}
func (n *noopSpan) SpanContext() context.Context         { return n.ctx }

type noopTracer struct{}

func (n *noopTracer) Start(ctx context.Context, _ string) (context.Context, ISpan) {
	return ctx, &noopSpan{ctx: ctx}
}
func (n *noopTracer) IsEnabled() bool { return false }

// --- 全局 tracer ---

// tracerHolder 包装 ITracer，保证 atomic.Value 存储类型一致。
type tracerHolder struct {
	tracer ITracer
}

var globalTracer atomic.Value

func init() {
	globalTracer.Store(tracerHolder{tracer: &noopTracer{}})
}

// SetGlobalTracer 设置全局 tracer（通常在 Node 初始化时调用一次）。
func SetGlobalTracer(t ITracer) {
	if t == nil {
		t = &noopTracer{}
	}
	globalTracer.Store(tracerHolder{tracer: t})
}

// GlobalTracer 返回当前全局 tracer。
func GlobalTracer() ITracer {
	return globalTracer.Load().(tracerHolder).tracer
}
