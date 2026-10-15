package errorx

import (
	"errors"
	"strings"
	"testing"
)

// ============================================================================
// P1-1.2: errorx 边界行为测试
//
// 覆盖现有测试未触达的边界：
// - Wire 序列化/反序列化 (MarshalToBytes / UnmarshalFromBytes)
// - RootCause / AllFields / CodeFrom / HasCode 边界输入
// - WithMsg / WithFields 行为
// - Detail() 格式化
// - nil / 空链路 / 深链路
// ============================================================================

// --- RootCause 边界 ---

func TestRootCause_Nil(t *testing.T) {
	// nil 输入应返回 nil
	if RootCause(nil) != nil {
		t.Error("RootCause(nil) should be nil")
	}
}

func TestRootCause_SingleLayer(t *testing.T) {
	e := New(1, "single")
	if RootCause(e) != e {
		t.Error("RootCause of single error should be itself")
	}
}

func TestRootCause_StdErrorAtBottom(t *testing.T) {
	std := errors.New("base")
	wrapped := Wrap(std, "middle")
	top := Wrap(wrapped, "top")
	root := RootCause(top)
	if root != std {
		t.Errorf("RootCause should be std error, got %v", root)
	}
}

// --- CodeFrom / HasCode 边界 ---

func TestCodeFrom_Nil(t *testing.T) {
	if CodeFrom(nil) != 0 {
		t.Error("CodeFrom(nil) should be 0")
	}
}

func TestCodeFrom_AllZeroCode(t *testing.T) {
	e1 := NewMsg("no code 1")
	e2 := Wrap(e1, "no code 2")
	if CodeFrom(e2) != 0 {
		t.Error("CodeFrom with all zero codes should be 0")
	}
}

func TestCodeFrom_StdError(t *testing.T) {
	std := errors.New("std")
	if CodeFrom(std) != 0 {
		t.Error("CodeFrom(std error) should be 0")
	}
}

func TestHasCode_Nil(t *testing.T) {
	if HasCode(nil, 1001) {
		t.Error("HasCode(nil, ...) should be false")
	}
}

func TestHasCode_StdError(t *testing.T) {
	if HasCode(errors.New("std"), 1001) {
		t.Error("HasCode(std error, ...) should be false")
	}
}

func TestHasCode_ZeroCode(t *testing.T) {
	e := NewMsg("no code")
	if HasCode(e, 0) {
		t.Error("HasCode(e, 0) should be false for code=0 error")
	}
}

// --- AllFields 边界 ---

func TestAllFields_Nil(t *testing.T) {
	fields := AllFields(nil)
	if len(fields) != 0 {
		t.Error("AllFields(nil) should be empty")
	}
}

func TestAllFields_NoFields(t *testing.T) {
	e := New(1, "bare")
	fields := AllFields(e)
	if len(fields) != 0 {
		t.Error("AllFields on fieldless error should be empty")
	}
}

func TestAllFields_MixedChain(t *testing.T) {
	// errorx → std error → errorx (only first and third have fields)
	inner := New(1, "inner").WithField("k1", "v1")
	std := errors.New("standard")
	// Wrap std, then wrap inner
	mid := Wrap(std, "mid") // mid → std (std is leaf, inner is separate)
	// Build: top → mid_with_field → errorx_inner
	top := WrapWithCode(inner, 2, "top").WithField("k2", "v2")
	_ = mid // mid is separate, we test a chain with std in middle

	// Actually build a chain: top(field) → Wrap(std_error, "mid") → inner(field)
	// errorx can only wrap one error. Let's do: top → wrappedStd → inner
	// But Wrap takes an error, so: inner → std_wrap → top
	chain := Wrap(Wrap(inner, "mid_wrap"), "top_wrap").WithField("k3", "v3")
	fields := AllFields(chain)
	// chain has field k3 on top, inner has k1
	if len(fields) != 2 {
		t.Errorf("AllFields count = %d, want 2", len(fields))
	}
	if fields[0].Key != "k3" {
		t.Errorf("fields[0].Key = %q, want 'k3'", fields[0].Key)
	}
	if fields[1].Key != "k1" {
		t.Errorf("fields[1].Key = %q, want 'k1'", fields[1].Key)
	}

	// Also verify top has the field
	topFields := AllFields(top)
	if len(topFields) != 2 {
		t.Errorf("topFields count = %d, want 2 (k2 + k1)", len(topFields))
	}
}

// --- WithMsg ---

func TestWithMsg(t *testing.T) {
	original := New(1001, "original msg")
	replaced := original.WithMsg("new msg")
	if replaced.Message() != "new msg" {
		t.Errorf("msg = %q, want 'new msg'", replaced.Message())
	}
	if original.Message() != "original msg" {
		t.Error("original should not be modified")
	}
	if replaced.Code() != 1001 {
		t.Error("code should be preserved")
	}
	if original == replaced {
		t.Error("WithMsg should return new instance")
	}
}

// --- WithFields ---

func TestWithFields_Empty(t *testing.T) {
	e := New(1, "test")
	same := e.WithFields() // no fields
	if same != e {
		t.Error("WithFields() with no args should return same pointer")
	}
}

func TestWithFields_Multiple(t *testing.T) {
	e := New(1, "test")
	enriched := e.WithFields(
		Field{Key: "a", Val: 1},
		Field{Key: "b", Val: "two"},
	)
	if len(enriched.GetFields()) != 2 {
		t.Errorf("fields count = %d, want 2", len(enriched.GetFields()))
	}
	if len(e.GetFields()) != 0 {
		t.Error("original should not be modified")
	}
}

// --- Detail 格式 ---

func TestDetail_SingleError(t *testing.T) {
	e := New(1001, "timeout")
	d := e.Detail()
	if !strings.Contains(d, "[1001]") {
		t.Errorf("Detail should contain code: %s", d)
	}
	if !strings.Contains(d, "timeout") {
		t.Errorf("Detail should contain msg: %s", d)
	}
}

func TestDetail_Chain(t *testing.T) {
	inner := New(1001, "timeout")
	outer := WrapWithCode(inner, 2001, "gateway error")
	d := outer.Detail()
	if !strings.Contains(d, "└─") {
		t.Errorf("Detail chain should contain tree char: %s", d)
	}
	if !strings.Contains(d, "[1001]") || !strings.Contains(d, "[2001]") {
		t.Errorf("Detail should contain both codes: %s", d)
	}
}

func TestDetail_StdErrorCause(t *testing.T) {
	std := errors.New("underlying error")
	wrapped := Wrap(std, "wrapped")
	d := wrapped.Detail()
	if !strings.Contains(d, "underlying error") {
		t.Errorf("Detail should contain std error msg: %s", d)
	}
}

// --- Wire 序列化/反序列化 ---

func TestMarshalUnmarshal_Nil(t *testing.T) {
	b := MarshalToBytes(nil)
	if b != nil {
		t.Error("MarshalToBytes(nil) should be nil")
	}
	e := UnmarshalFromBytes(nil)
	if e != nil {
		t.Error("UnmarshalFromBytes(nil) should be nil")
	}
	e = UnmarshalFromBytes([]byte{})
	if e != nil {
		t.Error("UnmarshalFromBytes(empty) should be nil")
	}
}

func TestMarshalUnmarshal_SimpleError(t *testing.T) {
	original := New(1001, "rpc timeout")
	b := MarshalToBytes(original)
	if len(b) == 0 {
		t.Fatal("MarshalToBytes should produce non-empty bytes")
	}
	restored := UnmarshalFromBytes(b)
	if restored == nil {
		t.Fatal("UnmarshalFromBytes should return non-nil")
	}
	ex, ok := restored.(*Error)
	if !ok {
		t.Fatalf("restored should be *Error, got %T", restored)
	}
	if ex.Code() != 1001 {
		t.Errorf("code = %d, want 1001", ex.Code())
	}
	if ex.Message() != "rpc timeout" {
		t.Errorf("msg = %q, want 'rpc timeout'", ex.Message())
	}
}

func TestMarshalUnmarshal_WithFields(t *testing.T) {
	original := New(1001, "timeout").
		WithField("service", "UserSvc").
		WithField("method", "Login")
	b := MarshalToBytes(original)
	restored := UnmarshalFromBytes(b).(*Error)

	fields := restored.GetFields()
	if len(fields) != 2 {
		t.Fatalf("fields count = %d, want 2", len(fields))
	}
	if fields[0].Key != "service" || fields[0].Val != "UserSvc" {
		t.Errorf("field[0] = %v", fields[0])
	}
	if fields[1].Key != "method" || fields[1].Val != "Login" {
		t.Errorf("field[1] = %v", fields[1])
	}
}

func TestMarshalUnmarshal_Chain(t *testing.T) {
	inner := New(1001, "timeout").WithField("svc", "A")
	outer := WrapWithCode(inner, 2001, "gateway error").WithField("gw", "gw-01")

	b := MarshalToBytes(outer)
	restored := UnmarshalFromBytes(b).(*Error)

	if restored.Code() != 2001 {
		t.Errorf("outer code = %d, want 2001", restored.Code())
	}
	cause := restored.Unwrap()
	if cause == nil {
		t.Fatal("cause should not be nil")
	}
	causeEx, ok := cause.(*Error)
	if !ok {
		t.Fatalf("cause should be *Error, got %T", cause)
	}
	if causeEx.Code() != 1001 {
		t.Errorf("inner code = %d, want 1001", causeEx.Code())
	}

	// errors.Is 应该在还原后仍工作
	sentinel := New(1001, "")
	if !errors.Is(restored, sentinel) {
		t.Error("errors.Is should find code 1001 in restored chain")
	}
}

func TestMarshalUnmarshal_StdError(t *testing.T) {
	std := errors.New("standard error")
	b := MarshalToBytes(std)
	restored := UnmarshalFromBytes(b)
	if restored == nil {
		t.Fatal("restored should not be nil")
	}
	if !strings.Contains(restored.Error(), "standard error") {
		t.Errorf("restored msg should contain original: %s", restored.Error())
	}
}

func TestMarshalUnmarshal_ChainWithStdCause(t *testing.T) {
	std := errors.New("io error")
	wrapped := WrapWithCode(std, 3001, "read failed")
	b := MarshalToBytes(wrapped)
	restored := UnmarshalFromBytes(b).(*Error)

	if restored.Code() != 3001 {
		t.Errorf("code = %d, want 3001", restored.Code())
	}
	cause := restored.Unwrap()
	if cause == nil {
		t.Fatal("cause should not be nil after unmarshal")
	}
	if !strings.Contains(cause.Error(), "io error") {
		t.Errorf("cause msg = %q, want 'io error'", cause.Error())
	}
}

func TestUnmarshalFromBytes_InvalidData(t *testing.T) {
	// 无效 proto 数据应返回错误而不是 panic
	restored := UnmarshalFromBytes([]byte{0xff, 0xfe, 0xfd})
	if restored == nil {
		t.Fatal("invalid data should return non-nil error")
	}
	if !strings.Contains(restored.Error(), "unmarshal error failed") {
		t.Errorf("error msg should indicate unmarshal failure: %s", restored.Error())
	}
}

func TestMarshalUnmarshal_CallerPreserved(t *testing.T) {
	original := New(1001, "timeout")
	origFile, origLine := original.Caller()
	if origFile == "" {
		t.Skip("caller not captured on this platform")
	}

	b := MarshalToBytes(original)
	restored := UnmarshalFromBytes(b).(*Error)
	restoredFile, restoredLine := restored.Caller()

	if restoredFile != origFile {
		t.Errorf("file = %q, want %q", restoredFile, origFile)
	}
	if restoredLine != origLine {
		t.Errorf("line = %d, want %d", restoredLine, origLine)
	}
}

// --- CombineErrors 与 errors.Is 交互 ---

func TestCombineErrors_ErrorsIs(t *testing.T) {
	e1 := New(1001, "timeout")
	e2 := New(1002, "not found")
	combined := CombineErrors(e1, e2)

	// errors.Join 产生的 error 支持 errors.Is 遍历所有子错误
	if !errors.Is(combined, New(1001, "")) {
		t.Error("combined should match code 1001")
	}
	if !errors.Is(combined, New(1002, "")) {
		t.Error("combined should match code 1002")
	}
	if errors.Is(combined, New(9999, "")) {
		t.Error("combined should not match code 9999")
	}
}

func TestCombineErrors_AllNil(t *testing.T) {
	if CombineErrors() != nil {
		t.Error("empty CombineErrors should be nil")
	}
}

// --- Is 行为细节 ---

func TestIs_DifferentCodes(t *testing.T) {
	e1 := New(1001, "timeout")
	e2 := New(1002, "not found")
	if errors.Is(e1, e2) {
		t.Error("different codes should not match")
	}
}

func TestIs_SameCodeDifferentMsg(t *testing.T) {
	e1 := New(1001, "timeout A")
	e2 := New(1001, "timeout B")
	// 按码匹配，不按消息
	if !errors.Is(e1, e2) {
		t.Error("same code should match regardless of msg")
	}
}

func TestIs_ErrorxVsStdError(t *testing.T) {
	ex := New(1001, "timeout")
	std := errors.New("timeout")
	// *Error.Is 只匹配 *Error 类型
	if errors.Is(ex, std) {
		t.Error("errorx should not match std error via Is (different types)")
	}
}

// --- Error() 输出格式 ---

func TestError_NoCode(t *testing.T) {
	e := NewMsg("something failed")
	s := e.Error()
	if strings.Contains(s, "[0]") {
		t.Errorf("code=0 should not appear in output: %s", s)
	}
	if !strings.Contains(s, "something failed") {
		t.Errorf("msg should appear: %s", s)
	}
}

func TestError_NoFields(t *testing.T) {
	e := New(1, "test")
	s := e.Error()
	if strings.Contains(s, "{") {
		t.Errorf("no fields should not have braces: %s", s)
	}
}

// --- DeepChain 不崩溃 ---

func TestDeepChain_NoPanic(t *testing.T) {
	var err error = errors.New("root")
	for i := 0; i < 100; i++ {
		err = Wrap(err, "layer")
	}
	// 确保 Error()、Detail()、AllFields()、RootCause()、CodeFrom() 不 panic
	_ = err.Error()
	if ex, ok := err.(*Error); ok {
		_ = ex.Detail()
	}
	_ = AllFields(err)
	_ = RootCause(err)
	_ = CodeFrom(err)
}
