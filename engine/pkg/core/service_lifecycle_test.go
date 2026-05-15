package core

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/njtc406/emberengine/engine/pkg/def"
)

// ============================================================================
// P1-3.2: Service 生命周期状态机测试
//
// 覆盖场景：
// - Stop 幂等：多次调用不 panic
// - Stop 并发安全：多 goroutine 同时 Stop 不 panic
// - Start 对 initErr 的保护
// - 状态机转换基本规则
// ============================================================================

// --- Stop 幂等 ---

func TestServiceStop_Idempotent(t *testing.T) {
	s := &Service{}
	// 设置为运行状态
	atomic.StoreInt32(&s.status, def.SvcStatusRunning)

	// 第一次 Stop
	s.Stop()
	if atomic.LoadInt32(&s.status) != def.SvcStatusClosed {
		t.Errorf("status after first Stop = %d, want %d", atomic.LoadInt32(&s.status), def.SvcStatusClosed)
	}

	// 第二次 Stop 不应 panic
	s.Stop()
	if atomic.LoadInt32(&s.status) != def.SvcStatusClosed {
		t.Errorf("status after second Stop = %d, want %d", atomic.LoadInt32(&s.status), def.SvcStatusClosed)
	}
}

// --- Stop 从未初始化状态 ---

func TestServiceStop_FromUnknownStatus(t *testing.T) {
	s := &Service{}
	// 状态为 SvcStatusUnknown (0)
	s.Stop()
	// 应该进入 Closed
	status := atomic.LoadInt32(&s.status)
	if status != def.SvcStatusClosed {
		t.Errorf("status after Stop from Unknown = %d, want %d", status, def.SvcStatusClosed)
	}
}

// --- Stop 并发安全 ---

func TestServiceStop_Concurrent(t *testing.T) {
	s := &Service{}
	atomic.StoreInt32(&s.status, def.SvcStatusRunning)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Stop() // 不应 panic
		}()
	}
	wg.Wait()

	if atomic.LoadInt32(&s.status) != def.SvcStatusClosed {
		t.Errorf("status after concurrent Stop = %d, want %d", atomic.LoadInt32(&s.status), def.SvcStatusClosed)
	}
}

// --- Start 对 initErr 的保护 ---

func TestServiceStart_WithInitErr(t *testing.T) {
	s := &Service{}
	s.initErr = &testInitError{msg: "init failed"}

	err := s.Start()
	if err == nil {
		t.Fatal("Start should fail when initErr is set")
	}
	if atomic.LoadInt32(&s.status) != def.SvcStatusUnknown {
		t.Errorf("status should remain Unknown when initErr is set, got %d", atomic.LoadInt32(&s.status))
	}
}

// --- Start 从非 Init 状态被拒绝 ---

func TestServiceStart_FromRunningStatus(t *testing.T) {
	s := &Service{}
	atomic.StoreInt32(&s.status, def.SvcStatusRunning)

	err := s.Start()
	if err == nil {
		t.Fatal("Start from Running should fail")
	}
}

func TestServiceStart_FromClosedStatus(t *testing.T) {
	s := &Service{}
	atomic.StoreInt32(&s.status, def.SvcStatusClosed)

	err := s.Start()
	if err == nil {
		t.Fatal("Start from Closed should fail")
	}
}

// --- setStatus 规则 ---

func TestSetStatus_IgnoresSameStatus(t *testing.T) {
	s := &Service{}
	atomic.StoreInt32(&s.status, def.SvcStatusRunning)
	s.setStatus(def.SvcStatusRunning) // 应该是 no-op
	if atomic.LoadInt32(&s.status) != def.SvcStatusRunning {
		t.Error("setStatus to same value should be no-op")
	}
}

func TestSetStatus_IgnoresAfterClosed(t *testing.T) {
	s := &Service{}
	atomic.StoreInt32(&s.status, def.SvcStatusClosed)
	s.setStatus(def.SvcStatusRunning) // 不应改变
	if atomic.LoadInt32(&s.status) != def.SvcStatusClosed {
		t.Error("setStatus should not modify Closed status")
	}
}

// --- 辅助类型 ---

type testInitError struct {
	msg string
}

func (e *testInitError) Error() string { return e.msg }
