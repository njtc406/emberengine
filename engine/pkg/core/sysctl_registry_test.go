// Package core
// 模块名: SysCtl 注册中心单元测试
// 功能描述: 验证注册/查询/覆盖语义；handler 通过 mailbox 串行执行的契约由 mailbox 自身测试覆盖
// 作者:  yr  2026/4/27
// 最后更新:  yr  2026/4/27
package core

import (
	"context"
	"testing"
)

func TestSysCtlRegistry_RegisterAndLookup(t *testing.T) {
	r := newSysCtlRegistry()

	called := 0
	h := func(_ context.Context, args []any) error {
		called++
		if len(args) != 2 || args[0].(string) != "a" || args[1].(int) != 42 {
			t.Errorf("unexpected args: %v", args)
		}
		return nil
	}
	if old := r.register("foo", h); old != nil {
		t.Fatalf("expected nil old handler on first register, got %v", old)
	}

	got, ok := r.lookup("foo")
	if !ok || got == nil {
		t.Fatalf("lookup failed after register")
	}
	if err := got(context.Background(), []any{"a", 42}); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if called != 1 {
		t.Fatalf("expected called=1, got %d", called)
	}
}

func TestSysCtlRegistry_OverrideReturnsOld(t *testing.T) {
	r := newSysCtlRegistry()
	h1 := func(_ context.Context, _ []any) error { return nil }
	h2 := func(_ context.Context, _ []any) error { return nil }
	r.register("cmd", h1)
	old := r.register("cmd", h2)
	if old == nil {
		t.Fatalf("expected old handler returned on override")
	}
}

func TestSysCtlRegistry_LookupMissing(t *testing.T) {
	r := newSysCtlRegistry()
	if h, ok := r.lookup("missing"); ok || h != nil {
		t.Fatalf("expected lookup miss, got ok=%v h=%v", ok, h)
	}
}

func TestSysCtlRegistry_RejectsEmptyOrNil(t *testing.T) {
	r := newSysCtlRegistry()
	if old := r.register("", func(_ context.Context, _ []any) error { return nil }); old != nil {
		t.Fatalf("empty name should be rejected, got old=%v", old)
	}
	if old := r.register("x", nil); old != nil {
		t.Fatalf("nil handler should be rejected, got old=%v", old)
	}
	if _, ok := r.lookup(""); ok {
		t.Fatalf("empty name should not be registered")
	}
	if _, ok := r.lookup("x"); ok {
		t.Fatalf("nil handler should not be registered")
	}
}

func TestSysCtlRegistry_NamesSnapshot(t *testing.T) {
	r := newSysCtlRegistry()
	r.register("a", func(_ context.Context, _ []any) error { return nil })
	r.register("b", func(_ context.Context, _ []any) error { return nil })
	names := r.names()
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %v", names)
	}
	seen := map[string]bool{}
	for _, n := range names {
		seen[n] = true
	}
	if !seen["a"] || !seen["b"] {
		t.Fatalf("missing expected names: %v", names)
	}
}
