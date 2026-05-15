package msgenvelope

import (
	"context"
	"testing"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/utils/emberctx"
	"github.com/njtc406/emberengine/engine/pkg/utils/xcontext"
)

// --- P2-5: Envelope ContextHeaders round-trip 验证 ---

func TestToProtoMsg_PreservesTraceID(t *testing.T) {
	ctx := emberctx.AddHeader(context.Background(), def.DefaultTraceIdKey, "trace-envelope-test")
	ctx = emberctx.AddHeader(ctx, "custom-header", "custom-value")

	envelope := NewMsgEnvelope()
	defer envelope.Release()

	meta := NewMeta()
	meta.SetReceiverPid(&actor.PID{ServiceUid: "svc-1"})
	meta.SetReqId(42)

	data := NewData()
	data.SetMethod("TestMethod")
	data.SetNeedResponse(false)

	envelope.SetMeta(meta)
	envelope.SetData(data)

	msg, err := envelope.ToProtoMsg(ctx)
	if err != nil {
		t.Fatalf("ToProtoMsg: %v", err)
	}
	defer ReleaseMessage(msg)

	// ContextHeaders 应该包含 traceId
	if msg.ContextHeaders == nil {
		t.Fatal("ContextHeaders should not be nil")
	}
	if got := msg.ContextHeaders[def.DefaultTraceIdKey]; got != "trace-envelope-test" {
		t.Errorf("traceId in ContextHeaders = %q, want %q", got, "trace-envelope-test")
	}
	if got := msg.ContextHeaders["custom-header"]; got != "custom-value" {
		t.Errorf("custom-header in ContextHeaders = %q, want %q", got, "custom-value")
	}
}

func TestToProtoMsg_NoTraceID_EmptyHeaders(t *testing.T) {
	// 没有 traceId 的 context 也不应该 panic
	ctx := context.Background()

	envelope := NewMsgEnvelope()
	defer envelope.Release()

	meta := NewMeta()
	meta.SetReceiverPid(&actor.PID{ServiceUid: "svc-2"})
	meta.SetReqId(1)

	data := NewData()
	data.SetMethod("NoTrace")
	data.SetNeedResponse(false)

	envelope.SetMeta(meta)
	envelope.SetData(data)

	msg, err := envelope.ToProtoMsg(ctx)
	if err != nil {
		t.Fatalf("ToProtoMsg: %v", err)
	}
	defer ReleaseMessage(msg)

	// headers 可以是 nil 或空 map，不应 panic
	if msg.ContextHeaders != nil {
		if _, ok := msg.ContextHeaders[def.DefaultTraceIdKey]; ok {
			t.Error("empty ctx should not have traceId in headers")
		}
	}
}

func TestContextHeaders_RoundTrip(t *testing.T) {
	// 模拟完整的序列化→反序列化 round-trip
	originalTraceID := "trace-roundtrip-abc-123"
	ctx := emberctx.AddHeader(context.Background(), def.DefaultTraceIdKey, originalTraceID)
	ctx = emberctx.AddHeader(ctx, "user-id", "u-999")

	envelope := NewMsgEnvelope()
	defer envelope.Release()

	meta := NewMeta()
	meta.SetReceiverPid(&actor.PID{ServiceUid: "target"})
	meta.SetReqId(100)

	data := NewData()
	data.SetMethod("RoundTrip")
	data.SetNeedResponse(true)

	envelope.SetMeta(meta)
	envelope.SetData(data)

	msg, err := envelope.ToProtoMsg(ctx)
	if err != nil {
		t.Fatalf("ToProtoMsg: %v", err)
	}
	defer ReleaseMessage(msg)

	// 模拟远程端：从 ContextHeaders 重建 context
	restoredCtx := xcontext.New(context.Background())
	restoredHeaders := make(map[string]any, len(msg.ContextHeaders))
	for k, v := range msg.ContextHeaders {
		restoredHeaders[k] = v
	}
	restoredCtx.AddHeaders(restoredHeaders)

	// 验证 traceId 存活
	if got := restoredCtx.GetTranceId(); got != originalTraceID {
		t.Errorf("restored traceId = %q, want %q", got, originalTraceID)
	}

	// 验证自定义 header 存活
	if got := restoredCtx.GetHeader("user-id"); got != "u-999" {
		t.Errorf("restored user-id = %v, want %q", got, "u-999")
	}
}

func TestContextHeaders_EmptyTraceID_CompatibleBehavior(t *testing.T) {
	// 如果远程端发送的 headers 不包含 traceId，行为应兼容
	wireHeaders := map[string]string{
		"some-key": "some-val",
	}

	restoredCtx := xcontext.New(context.Background())
	restoredHeaders := make(map[string]any, len(wireHeaders))
	for k, v := range wireHeaders {
		restoredHeaders[k] = v
	}
	restoredCtx.AddHeaders(restoredHeaders)

	// 没有 traceId 不应 panic
	got := restoredCtx.GetTranceId()
	// 空值或空字符串均可接受
	_ = got
}
