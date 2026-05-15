package event

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/njtc406/emberengine/engine/pkg/def"
)

// ============================================================================
// P1-3.4: 事件系统补充测试
//
// 覆盖场景：
// - handler 返回 error 不影响后续 handler 执行
// - Destroy 后再 Trigger 不 panic
// - 并发注册/触发安全
// - 同一 handler 重复注册
// ============================================================================

func TestTrigger_HandlerErrorDoesNotStopOthers(t *testing.T) {
	trigger := NewTrigger()
	trigger.Init(nil)

	handler := NewTriggerHandler()
	handler.Init(trigger)

	var secondCalled bool

	// 第一个 handler 返回 error
	err := handler.RegisterEvent(def.EventType(100), "err-handler", func(ctx context.Context, data any) error {
		return fmt.Errorf("handler error")
	})
	if err != nil {
		t.Fatalf("RegisterEvent failed: %v", err)
	}

	// 第二个 handler 应该正常调用
	registerTyped(t, handler, def.EventType(100), "ok-handler", func(ctx context.Context, data string) {
		secondCalled = true
	})

	trigger.Trigger(context.Background(), def.EventType(100), "test")

	if !secondCalled {
		t.Error("second handler should be called even when first returns error")
	}
}

func TestTrigger_DestroyThenTrigger(t *testing.T) {
	trigger := NewTrigger()
	trigger.Init(nil)

	handler := NewTriggerHandler()
	handler.Init(trigger)

	registerTyped(t, handler, def.EventType(200), "to-destroy", func(ctx context.Context, data string) {})

	handler.Destroy()

	// Trigger 对已清理的事件类型不应 panic
	trigger.Trigger(context.Background(), def.EventType(200), "after-destroy")
}

func TestTrigger_ConcurrentRegisterAndTrigger(t *testing.T) {
	trigger := NewTrigger()
	trigger.Init(nil)

	var wg sync.WaitGroup
	var totalCalls int64

	// 并发注册 + 并发触发
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			h := NewTriggerHandler()
			h.Init(trigger)
			name := fmt.Sprintf("handler-%d", idx)
			registerTyped(t, h, def.EventType(300), name, func(ctx context.Context, data string) {
				atomic.AddInt64(&totalCalls, 1)
			})
		}(i)
	}
	wg.Wait()

	// 所有注册完成后并发触发
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			trigger.Trigger(context.Background(), def.EventType(300), "concurrent")
		}()
	}
	wg.Wait()

	// 10 handlers * 5 triggers = 50
	if totalCalls != 50 {
		t.Errorf("expected 50 calls, got %d", totalCalls)
	}
}

func TestTrigger_DuplicateNameRegistration(t *testing.T) {
	trigger := NewTrigger()
	trigger.Init(nil)

	handler := NewTriggerHandler()
	handler.Init(trigger)

	var callCount int32

	registerTyped(t, handler, def.EventType(400), "dup", func(ctx context.Context, data string) {
		atomic.AddInt32(&callCount, 1)
	})
	registerTyped(t, handler, def.EventType(400), "dup", func(ctx context.Context, data string) {
		atomic.AddInt32(&callCount, 10)
	})

	trigger.Trigger(context.Background(), def.EventType(400), "test")

	// BindHandler 使用 map[name]callback，同名注册会覆盖前一个
	if callCount != 10 {
		t.Errorf("expected last registered callback (10), got %d", callCount)
	}
}

func TestHandler_RegisterWithNilTrigger(t *testing.T) {
	handler := NewTriggerHandler()
	// 不调用 Init，trigger 为 nil

	err := handler.RegisterEvent(def.EventType(500), "test", func(ctx context.Context, data any) error {
		return nil
	})
	if err == nil {
		t.Fatal("RegisterEvent with nil trigger should return error")
	}
}

func TestHandler_TriggerWithNilProcessor(t *testing.T) {
	handler := NewTriggerHandler()
	// trigger 为 nil，不应 panic
	handler.Trigger(context.Background(), def.EventType(600), "noop")
}
