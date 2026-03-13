package errorx

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// --- sentinel errors for testing ---

var (
	ErrTimeout  = New(1001, "rpc timeout")
	ErrNotFound = New(1002, "service not found")
	ErrInternal = New(2001, "internal error")
	ErrNoCode   = NewMsg("no code error")
)

func TestNew(t *testing.T) {
	e := New(1001, "timeout")
	if e.Code() != 1001 {
		t.Errorf("code = %d, want 1001", e.Code())
	}
	if e.Message() != "timeout" {
		t.Errorf("msg = %q, want %q", e.Message(), "timeout")
	}
	file, line := e.Caller()
	if file == "" || line == 0 {
		t.Error("caller not captured")
	}
}

func TestNewMsg(t *testing.T) {
	e := NewMsg("something failed")
	if e.Code() != 0 {
		t.Errorf("code = %d, want 0", e.Code())
	}
	if e.Message() != "something failed" {
		t.Errorf("msg = %q", e.Message())
	}
}

func TestWrapNil(t *testing.T) {
	if Wrap(nil, "msg") != nil {
		t.Error("Wrap(nil) should return nil")
	}
	if Wrapf(nil, "msg %d", 1) != nil {
		t.Error("Wrapf(nil) should return nil")
	}
	if WrapWithCode(nil, 1, "msg") != nil {
		t.Error("WrapWithCode(nil) should return nil")
	}
}

func TestErrorChain(t *testing.T) {
	// 模拟3层错误传递
	// Layer 1: 底层 RPC 超时
	err1 := New(1001, "call UserService.Login timeout").
		WithField("service", "UserService").
		WithField("method", "Login")

	// Layer 2: 中间层 wrap
	err2 := Wrap(err1, "handle login request failed")

	// Layer 3: 顶层 wrap with code
	err3 := WrapWithCode(err2, 2001, "gateway error").
		WithField("gateway", "gw-01")

	// 输出完整链路
	t.Logf("Error(): %s", err3.Error())
	t.Logf("Detail():\n%s", err3.Detail())

	// 验证 errors.Is 按码匹配
	if !errors.Is(err3, ErrTimeout) {
		t.Error("errors.Is(err3, ErrTimeout) should be true — code 1001 exists in chain")
	}
	if !errors.Is(err3, ErrInternal) {
		t.Error("errors.Is(err3, ErrInternal) should be true — code 2001 is err3 itself")
	}
	if errors.Is(err3, ErrNotFound) {
		t.Error("errors.Is(err3, ErrNotFound) should be false — code 1002 not in chain")
	}

	// HasCode — 遍历链路
	if !HasCode(err3, 1001) {
		t.Error("HasCode(err3, 1001) should be true")
	}
	if !HasCode(err3, 2001) {
		t.Error("HasCode(err3, 2001) should be true")
	}
	if HasCode(err3, 9999) {
		t.Error("HasCode(err3, 9999) should be false")
	}

	// CodeFrom — 第一个非零 code
	if c := CodeFrom(err3); c != 2001 {
		t.Errorf("CodeFrom(err3) = %d, want 2001 (outermost)", c)
	}

	// RootCause — 最底层
	root := RootCause(err3)
	if root != err1 {
		t.Errorf("RootCause should be err1, got %v", root)
	}

	// AllFields — 收集全链路 fields
	fields := AllFields(err3)
	if len(fields) != 3 {
		t.Errorf("AllFields count = %d, want 3", len(fields))
	}
	// 顺序: 外层 → 内层 (gateway, service, method)
	keys := make([]string, len(fields))
	for i, f := range fields {
		keys[i] = f.Key
	}
	t.Logf("AllFields keys: %v", keys)
}

func TestErrorsAs(t *testing.T) {
	stdErr := fmt.Errorf("standard error")
	wrapped := WrapWithCode(stdErr, 3001, "wrapped")

	var ex *Error
	if !errors.As(wrapped, &ex) {
		t.Error("errors.As should find *Error")
	}
	if ex.Code() != 3001 {
		t.Errorf("code = %d, want 3001", ex.Code())
	}
}

func TestIsWithStdError(t *testing.T) {
	stdErr := fmt.Errorf("base error")
	wrapped := Wrap(stdErr, "wrapped")

	// 标准 error 通过 Unwrap 链被找到
	if !errors.Is(wrapped, stdErr) {
		t.Error("errors.Is should find stdErr via Unwrap chain")
	}
}

func TestIsCodeZero(t *testing.T) {
	// code=0 的 error 不参与码匹配
	e1 := NewMsg("no code 1")
	e2 := NewMsg("no code 2")
	if errors.Is(e1, e2) {
		t.Error("code=0 errors should not match each other via Is")
	}
}

func TestWithFieldImmutable(t *testing.T) {
	// WithField 不应修改 sentinel
	original := New(1001, "timeout")
	enriched := original.WithField("service", "test")

	if len(original.GetFields()) != 0 {
		t.Error("original should not be modified")
	}
	if len(enriched.GetFields()) != 1 {
		t.Error("enriched should have 1 field")
	}
	if original == enriched {
		t.Error("WithField should return a new *Error")
	}
	// 码应该相同
	if enriched.Code() != original.Code() {
		t.Error("code should be preserved")
	}
}

func TestCombineErrors(t *testing.T) {
	// 全 nil
	if CombineErrors(nil, nil) != nil {
		t.Error("all nil should return nil")
	}

	// 单个
	e := New(1, "one")
	if CombineErrors(nil, e, nil) != e {
		t.Error("single non-nil should return as-is")
	}

	// 多个 — errors.Is 可遍历
	e1 := New(1001, "timeout")
	e2 := New(1002, "not found")
	combined := CombineErrors(e1, e2)
	if combined == nil {
		t.Fatal("combined should not be nil")
	}
	if !errors.Is(combined, ErrTimeout) {
		t.Error("combined should contain ErrTimeout")
	}
	if !errors.Is(combined, ErrNotFound) {
		t.Error("combined should contain ErrNotFound")
	}
}

func TestErrorOutput(t *testing.T) {
	e := New(1001, "timeout").WithField("svc", "User")
	s := e.Error()
	if !strings.Contains(s, "[1001]") {
		t.Errorf("output should contain code: %s", s)
	}
	if !strings.Contains(s, "svc=User") {
		t.Errorf("output should contain field: %s", s)
	}
	if !strings.Contains(s, ".go:") {
		t.Errorf("output should contain caller: %s", s)
	}
}

func TestShortFile(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"a/b/c/file.go", "c/file.go"},
		{"/home/user/go/src/pkg/errorx/errorx.go", "errorx/errorx.go"},
		{"file.go", "file.go"},
		{"pkg/file.go", "pkg/file.go"},
		{"e:\\pro\\src\\pkg\\errorx\\errorx.go", "errorx\\errorx.go"},
	}
	for _, tt := range tests {
		got := shortFile(tt.input)
		if got != tt.want {
			t.Errorf("shortFile(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestWrapf(t *testing.T) {
	base := New(1001, "timeout")
	wrapped := Wrapf(base, "calling %s.%s failed", "UserSvc", "Login")
	if wrapped.Message() != "calling UserSvc.Login failed" {
		t.Errorf("msg = %q", wrapped.Message())
	}
	if wrapped.Unwrap() != base {
		t.Error("cause should be base")
	}
}

// --- 输出示例 ---

func ExampleError_chain() {
	// Layer 1: RPC 底层超时
	err1 := New(1001, "rpc call timeout").
		WithField("target", "UserService").
		WithField("method", "GetProfile")

	// Layer 2: 业务层 wrap
	err2 := Wrap(err1, "load user profile failed").
		WithField("uid", 12345)

	// Layer 3: API 层 wrap with code
	err3 := WrapWithCode(err2, 5001, "api error")

	// 按码分支
	if errors.Is(err3, New(1001, "")) {
		fmt.Println("detected: rpc timeout")
	}
	if HasCode(err3, 1001) {
		fmt.Println("has code 1001")
	}
	fmt.Println("first code:", CodeFrom(err3))

	// Output:
	// detected: rpc timeout
	// has code 1001
	// first code: 5001
}

// --- Benchmark ---

func BenchmarkNew(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = New(1001, "timeout")
	}
}

func BenchmarkWrap(b *testing.B) {
	base := New(1001, "timeout")
	for i := 0; i < b.N; i++ {
		_ = Wrap(base, "wrapped")
	}
}

func BenchmarkWithField(b *testing.B) {
	base := New(1001, "timeout")
	for i := 0; i < b.N; i++ {
		_ = base.WithField("key", "value")
	}
}

func BenchmarkErrorString(b *testing.B) {
	e := WrapWithCode(
		Wrap(New(1001, "timeout").WithField("svc", "User"), "handle failed"),
		2001, "gateway error",
	)
	for i := 0; i < b.N; i++ {
		_ = e.Error()
	}
}

func BenchmarkHasCode(b *testing.B) {
	e := WrapWithCode(
		Wrap(New(1001, "timeout"), "mid"),
		2001, "top",
	)
	for i := 0; i < b.N; i++ {
		_ = HasCode(e, 1001)
	}
}
