package errorx

import (
	"errors"
	"testing"
)

func TestMarshalUnmarshalRoundTrip(t *testing.T) {
	// 3层错误链
	err1 := New(1001, "rpc timeout").
		WithField("target", "UserService").
		WithField("method", "Login")
	err2 := Wrap(err1, "load user failed").
		WithField("uid", 12345)
	err3 := WrapWithCode(err2, 5001, "api error").
		WithField("gateway", "gw-01")

	// Marshal
	data := MarshalToBytes(err3)
	t.Logf("wire bytes len: %d", len(data))

	if len(data) == 0 {
		t.Fatal("MarshalToBytes returned empty")
	}

	// Unmarshal
	restored := UnmarshalFromBytes(data)
	if restored == nil {
		t.Fatal("UnmarshalFromBytes returned nil")
	}

	ex, ok := restored.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T", restored)
	}

	// 验证外层
	if ex.Code() != 5001 {
		t.Errorf("code = %d, want 5001", ex.Code())
	}
	if ex.Message() != "api error" {
		t.Errorf("msg = %q, want %q", ex.Message(), "api error")
	}
	fields := ex.GetFields()
	if len(fields) != 1 || fields[0].Key != "gateway" {
		t.Errorf("fields = %v, want [{gateway gw-01}]", fields)
	}

	// 验证 caller 保留
	file, line := ex.Caller()
	if file == "" || line == 0 {
		t.Error("caller should be preserved after round-trip")
	}

	// 验证 errors.Is 按码匹配（还原后仍可工作）
	sentinel := New(1001, "")
	if !errors.Is(restored, sentinel) {
		t.Error("errors.Is(restored, sentinel{1001}) should be true")
	}
	if !HasCode(restored, 5001) {
		t.Error("HasCode(restored, 5001) should be true")
	}
	if !HasCode(restored, 1001) {
		t.Error("HasCode(restored, 1001) should be true — deep in chain")
	}

	// 验证 AllFields 收集全链路
	allFields := AllFields(restored)
	if len(allFields) != 4 {
		t.Errorf("AllFields count = %d, want 4 (gateway + uid + target + method)", len(allFields))
	}

	// 验证 RootCause
	root := RootCause(restored)
	if rootEx, ok := root.(*Error); ok {
		if rootEx.Code() != 1001 {
			t.Errorf("root code = %d, want 1001", rootEx.Code())
		}
	} else {
		t.Error("root should be *Error")
	}
}

func TestMarshalStdError(t *testing.T) {
	// 标准 error 也能序列化（退化为仅 msg 的 ErrorDetail）
	stdErr := errors.New("standard error")
	data := MarshalToBytes(stdErr)
	if len(data) == 0 {
		t.Fatal("should not be empty")
	}
	restored := UnmarshalFromBytes(data)
	if restored == nil {
		t.Fatal("should not be nil")
	}
	if restored.Error() != "standard error" {
		t.Errorf("got %q", restored.Error())
	}
}

func TestMarshalNil(t *testing.T) {
	data := MarshalToBytes(nil)
	if data != nil {
		t.Errorf("got %v, want nil", data)
	}
}

func TestUnmarshalNilBytes(t *testing.T) {
	restored := UnmarshalFromBytes(nil)
	if restored != nil {
		t.Error("nil bytes should return nil")
	}
}

func TestUnmarshalEmptyBytes(t *testing.T) {
	restored := UnmarshalFromBytes([]byte{})
	if restored != nil {
		t.Error("empty bytes should return nil")
	}
}

func TestMarshalWithStdCause(t *testing.T) {
	// *Error 包装标准 error
	stdErr := errors.New("context deadline exceeded")
	wrapped := WrapWithCode(stdErr, 1001, "rpc timeout")

	data := MarshalToBytes(wrapped)
	restored := UnmarshalFromBytes(data)
	if restored == nil {
		t.Fatal("should not be nil")
	}

	// 外层 code 保留
	if !HasCode(restored, 1001) {
		t.Error("code 1001 should be preserved")
	}

	// root cause 是纯文本节点
	root := RootCause(restored)
	if root.Error() != "context deadline exceeded" {
		t.Errorf("root = %q", root.Error())
	}
}

func TestChainDepthLimit(t *testing.T) {
	// 构造超长链，验证不会栈溢出
	var err error = New(1, "base")
	for i := 0; i < 100; i++ {
		err = Wrap(err, "layer")
	}
	data := MarshalToBytes(err)
	restored := UnmarshalFromBytes(data)
	if restored == nil {
		t.Fatal("should not be nil even with deep chain (truncated at maxChainDepth)")
	}
}

func TestProtoSizeVsJSON(t *testing.T) {
	// 展示 proto 编码大小
	e := WrapWithCode(
		Wrap(New(1001, "rpc call timeout").
			WithField("target", "UserService").
			WithField("method", "GetProfile"), "load user failed").
			WithField("uid", 12345),
		5001, "api error").
		WithField("gateway", "gw-01")

	protoBytes := MarshalToBytes(e)
	t.Logf("proto size: %d bytes", len(protoBytes))
}

func BenchmarkMarshalToBytes(b *testing.B) {
	e := WrapWithCode(
		Wrap(New(1001, "timeout").WithField("svc", "User"), "handle failed"),
		2001, "gateway error",
	)
	for i := 0; i < b.N; i++ {
		_ = MarshalToBytes(e)
	}
}

func BenchmarkUnmarshalFromBytes(b *testing.B) {
	e := WrapWithCode(
		Wrap(New(1001, "timeout").WithField("svc", "User"), "handle failed"),
		2001, "gateway error",
	)
	data := MarshalToBytes(e)
	for i := 0; i < b.N; i++ {
		_ = UnmarshalFromBytes(data)
	}
}
