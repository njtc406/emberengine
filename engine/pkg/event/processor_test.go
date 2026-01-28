// Package event
// @Title  同步事件触发器测试
// @Description  Trigger的单元测试
// @Author  yr  2026/1/31
// @Update  yr  2026/1/31
package event

import (
	"context"
	"os"
	"sync/atomic"
	"testing"

	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/log"
)

func TestMain(m *testing.M) {
	// Processor 在 panic recover / error 路径会写日志；确保 SysLogger 初始化避免 nil panic。
	log.Init(&log.LoggerConf{Stdout: true, Caller: false, Color: false, Level: "error"}, true)
	os.Exit(m.Run())
}

func registerTyped[T any](t *testing.T, h *Handler, eventType def.EventType, name string, cb func(ctx context.Context, data T)) {
	t.Helper()
	err := h.RegisterEvent(eventType, name, func(ctx context.Context, data any) error {
		v, ok := data.(T)
		if !ok {
			return nil
		}
		cb(ctx, v)
		return nil
	})
	if err != nil {
		t.Fatalf("RegisterEvent failed: %v", err)
	}
}

// TestData 测试用数据结构
type TestData struct {
	Message string
	Value   int
}

func TestTrigger_Basic(t *testing.T) {
	trigger := NewTrigger()
	trigger.Init(nil)

	handler := NewTriggerHandler()
	handler.Init(trigger)

	var callCount int32
	var receivedData *TestData

	registerTyped(t, handler, def.EventType(1), "test-handler", func(ctx context.Context, data *TestData) {
		atomic.AddInt32(&callCount, 1)
		receivedData = data
	})

	// 触发事件
	trigger.Trigger(context.Background(), def.EventType(1), &TestData{Message: "hello", Value: 42})

	if callCount != 1 {
		t.Errorf("expected callCount=1, got %d", callCount)
	}
	if receivedData == nil || receivedData.Message != "hello" || receivedData.Value != 42 {
		t.Errorf("expected receivedData={hello, 42}, got %+v", receivedData)
	}

	// 通过名称取消注册
	handler.UnregisterEvent(def.EventType(1), "test-handler")

	// 再次触发，不应该调用handler
	trigger.Trigger(context.Background(), def.EventType(1), &TestData{Message: "world", Value: 100})

	if callCount != 1 {
		t.Errorf("after unregister, expected callCount=1, got %d", callCount)
	}
}

func TestTrigger_MultipleHandlers(t *testing.T) {
	trigger := NewTrigger()
	trigger.Init(nil)

	handler := NewTriggerHandler()
	handler.Init(trigger)

	called := make(map[int]bool)
	registerTyped(t, handler, def.EventType(1), "handler-1", func(ctx context.Context, data string) { called[1] = true })
	registerTyped(t, handler, def.EventType(1), "handler-2", func(ctx context.Context, data string) { called[2] = true })
	registerTyped(t, handler, def.EventType(1), "handler-3", func(ctx context.Context, data string) { called[3] = true })

	// 触发事件
	trigger.Trigger(context.Background(), def.EventType(1), "test")

	if len(called) != 3 || !called[1] || !called[2] || !called[3] {
		t.Errorf("expected all 3 handlers called, got %+v", called)
	}
}

func TestTrigger_MultipleModules(t *testing.T) {
	trigger := NewTrigger()
	trigger.Init(nil)

	// 模拟两个module，各有自己的handler
	handler1 := NewTriggerHandler()
	handler1.Init(trigger)

	handler2 := NewTriggerHandler()
	handler2.Init(trigger)

	var module1Called, module2Called bool

	registerTyped(t, handler1, def.EventType(1), "module1-handler", func(ctx context.Context, data int) {
		module1Called = true
	})
	registerTyped(t, handler2, def.EventType(1), "module2-handler", func(ctx context.Context, data int) {
		module2Called = true
	})

	// 触发事件，两个module都应该收到
	trigger.Trigger(context.Background(), def.EventType(1), 123)

	if !module1Called {
		t.Error("expected module1 handler to be called")
	}
	if !module2Called {
		t.Error("expected module2 handler to be called")
	}

	// 只销毁module1的handler
	handler1.Destroy()

	module1Called = false
	module2Called = false

	// 再次触发，只有module2应该收到
	trigger.Trigger(context.Background(), def.EventType(1), 456)

	if module1Called {
		t.Error("expected module1 handler NOT to be called after destroy")
	}
	if !module2Called {
		t.Error("expected module2 handler to be called")
	}
}

func TestTrigger_DifferentEventTypes(t *testing.T) {
	trigger := NewTrigger()
	trigger.Init(nil)

	handler := NewTriggerHandler()
	handler.Init(trigger)

	var type1Called, type2Called bool

	registerTyped(t, handler, def.EventType(1), "type1-handler", func(ctx context.Context, data string) {
		type1Called = true
	})
	registerTyped(t, handler, def.EventType(2), "type2-handler", func(ctx context.Context, data string) {
		type2Called = true
	})

	// 只触发类型1
	trigger.Trigger(context.Background(), def.EventType(1), "test")

	if !type1Called {
		t.Error("expected type1 handler to be called")
	}
	if type2Called {
		t.Error("expected type2 handler NOT to be called")
	}
}

func TestTrigger_UnregisterAll(t *testing.T) {
	trigger := NewTrigger()
	trigger.Init(nil)

	handler := NewTriggerHandler()
	handler.Init(trigger)

	var callCount int32

	registerTyped(t, handler, def.EventType(1), "handler-a", func(ctx context.Context, data string) {
		atomic.AddInt32(&callCount, 1)
	})
	registerTyped(t, handler, def.EventType(1), "handler-b", func(ctx context.Context, data string) {
		atomic.AddInt32(&callCount, 1)
	})
	registerTyped(t, handler, def.EventType(2), "handler-c", func(ctx context.Context, data string) {
		atomic.AddInt32(&callCount, 1)
	})

	// 取消注册该handler下所有事件
	handler.UnregisterAll()

	// 触发事件，不应该调用任何handler
	trigger.Trigger(context.Background(), def.EventType(1), "test")
	trigger.Trigger(context.Background(), def.EventType(2), "test")

	if callCount != 0 {
		t.Errorf("after UnregisterAll, expected callCount=0, got %d", callCount)
	}
}

func TestTrigger_GetRegisteredEvents(t *testing.T) {
	trigger := NewTrigger()
	trigger.Init(nil)

	handler := NewTriggerHandler()
	handler.Init(trigger)

	registerTyped(t, handler, def.EventType(1), "login-handler", func(ctx context.Context, data string) {})
	registerTyped(t, handler, def.EventType(1), "audit-handler", func(ctx context.Context, data string) {})
	registerTyped(t, handler, def.EventType(2), "notify-handler", func(ctx context.Context, data string) {})

	events := handler.GetRegisteredEvents()

	if len(events[def.EventType(1)]) != 2 {
		t.Errorf("expected 2 handlers for eventType 1, got %d", len(events[def.EventType(1)]))
	}
	if len(events[def.EventType(2)]) != 1 {
		t.Errorf("expected 1 handler for eventType 2, got %d", len(events[def.EventType(2)]))
	}
}

func TestTrigger_PanicRecovery(t *testing.T) {
	trigger := NewTrigger()
	trigger.Init(nil)

	handler := NewTriggerHandler()
	handler.Init(trigger)

	var secondHandlerCalled bool

	// 第一个handler会panic
	err := handler.RegisterEvent(def.EventType(1), "panic-handler", func(ctx context.Context, data any) error {
		panic("test panic")
	})
	if err != nil {
		t.Fatalf("RegisterEvent failed: %v", err)
	}

	// 第二个handler应该仍然被调用
	registerTyped(t, handler, def.EventType(1), "normal-handler", func(ctx context.Context, data string) { secondHandlerCalled = true })

	// 触发事件，不应该panic
	trigger.Trigger(context.Background(), def.EventType(1), "test")

	if !secondHandlerCalled {
		t.Error("expected second handler to be called even after first handler panicked")
	}
}

func TestTrigger_HasHandler(t *testing.T) {
	trigger := NewTrigger()
	trigger.Init(nil)

	handler := NewTriggerHandler()
	handler.Init(trigger)

	if trigger.HasHandler(def.EventType(1)) {
		t.Error("expected HasHandler to return false for unregistered event type")
	}

	registerTyped(t, handler, def.EventType(1), "test-handler", func(ctx context.Context, data string) {})

	if !trigger.HasHandler(def.EventType(1)) {
		t.Error("expected HasHandler to return true after registration")
	}

	handler.UnregisterEvent(def.EventType(1), "test-handler")

	if trigger.HasHandler(def.EventType(1)) {
		t.Error("expected HasHandler to return false after unregister")
	}
}

func TestTrigger_TypeSafety(t *testing.T) {
	trigger := NewTrigger()
	trigger.Init(nil)

	handler := NewTriggerHandler()
	handler.Init(trigger)

	var receivedValue int

	// 注册期望int类型的handler
	registerTyped(t, handler, def.EventType(1), "int-handler", func(ctx context.Context, data int) { receivedValue = data })

	// 传入正确类型
	trigger.Trigger(context.Background(), def.EventType(1), 42)
	if receivedValue != 42 {
		t.Errorf("expected receivedValue=42, got %d", receivedValue)
	}

	// 传入错误类型，handler不应该被调用（类型断言失败）
	receivedValue = 0
	trigger.Trigger(context.Background(), def.EventType(1), "wrong type")
	if receivedValue != 0 {
		t.Errorf("expected receivedValue=0 (handler not called), got %d", receivedValue)
	}
}
