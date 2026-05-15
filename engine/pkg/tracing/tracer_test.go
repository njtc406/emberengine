package tracing

import (
	"context"
	"errors"
	"testing"
)

func TestNoopTracer_IsEnabled(t *testing.T) {
	tr := GlobalTracer()
	if tr.IsEnabled() {
		t.Error("default tracer should not be enabled")
	}
}

func TestNoopTracer_Start_ReturnsOriginalCtx(t *testing.T) {
	tr := GlobalTracer()
	origCtx := context.WithValue(context.Background(), "key", "val")

	ctx, span := tr.Start(origCtx, "test.op")
	defer span.End()

	// noop tracer 应该返回原始 context
	if ctx != origCtx {
		t.Error("noop tracer should return the original context")
	}
}

func TestNoopSpan_Methods_NoPanic(t *testing.T) {
	tr := GlobalTracer()
	_, span := tr.Start(context.Background(), "test.op")

	// 所有方法都不应 panic
	span.SetAttribute("key", "value")
	span.RecordError(errors.New("test error"))
	_ = span.SpanContext()
	span.End()
}

func TestSetGlobalTracer_Nil_FallsBackToNoop(t *testing.T) {
	SetGlobalTracer(nil)
	tr := GlobalTracer()
	if tr.IsEnabled() {
		t.Error("nil tracer should fallback to noop")
	}
}

func TestSetGlobalTracer_Custom(t *testing.T) {
	custom := &mockTracer{enabled: true}
	SetGlobalTracer(custom)
	defer SetGlobalTracer(nil) // 恢复 noop

	tr := GlobalTracer()
	if !tr.IsEnabled() {
		t.Error("custom tracer should be enabled")
	}

	ctx, span := tr.Start(context.Background(), "mock.op")
	defer span.End()

	ms := span.(*mockSpan)
	if ms.operationName != "mock.op" {
		t.Errorf("operationName = %q, want %q", ms.operationName, "mock.op")
	}
	_ = ctx
}

// --- mock tracer for testing ---

type mockTracer struct {
	enabled bool
}

func (m *mockTracer) Start(ctx context.Context, op string) (context.Context, ISpan) {
	span := &mockSpan{ctx: ctx, operationName: op}
	return ctx, span
}

func (m *mockTracer) IsEnabled() bool { return m.enabled }

type mockSpan struct {
	ctx           context.Context
	operationName string
	ended         bool
}

func (m *mockSpan) End()                                 { m.ended = true }
func (m *mockSpan) SetAttribute(_ string, _ interface{}) {}
func (m *mockSpan) RecordError(_ error)                  {}
func (m *mockSpan) SpanContext() context.Context         { return m.ctx }
