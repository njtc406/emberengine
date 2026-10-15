package emberctx

import (
	"context"
	"testing"
	"time"
)

type typedNilCtx struct{}

func (*typedNilCtx) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*typedNilCtx) Done() <-chan struct{}       { return nil }
func (*typedNilCtx) Err() error                  { return nil }
func (*typedNilCtx) Value(key any) any           { return nil }

func TestGetHeaderValue_NilContext_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("expected no panic, got: %v", r)
		}
	}()

	if got := GetHeaderValue(nil, "k"); got != nil {
		t.Fatalf("expected nil, got: %v", got)
	}
}

func TestGetHeaderValue_TypedNilContext_NoPanic(t *testing.T) {
	var p *typedNilCtx = nil
	var ctx context.Context = p // interface != nil, but underlying pointer is nil

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("expected no panic, got: %v", r)
		}
	}()

	if got := GetHeaderValue(ctx, "k"); got != nil {
		t.Fatalf("expected nil, got: %v", got)
	}
}

func TestWithHeader_NilContext_UsesBackground(t *testing.T) {
	ctx := WithHeader(nil, map[string]any{"a": 1})
	if ctx == nil {
		t.Fatal("expected non-nil ctx")
	}
	if got := GetHeaderValue(ctx, "a"); got != 1 {
		t.Fatalf("expected 1, got: %v", got)
	}
}

func TestAddHeader_TypedNilContext_UsesBackground(t *testing.T) {
	var p *typedNilCtx = nil
	var ctx context.Context = p

	ctx = AddHeader(ctx, "a", 1)
	if got := GetHeaderValue(ctx, "a"); got != 1 {
		t.Fatalf("expected 1, got: %v", got)
	}
}
