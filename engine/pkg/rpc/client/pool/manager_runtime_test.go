package pool

import (
	"context"
	"testing"
	"time"

	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

type mockPoolSender struct {
	closed bool
}

func (m *mockPoolSender) DeliverRequest(ctx context.Context, dispatcher inf.IRpcDispatcher, envelope inf.IEnvelope) error {
	return nil
}

func (m *mockPoolSender) DeliverResponse(ctx context.Context, dispatcher inf.IRpcDispatcher, envelope inf.IEnvelope) error {
	return nil
}

func (m *mockPoolSender) Close() {
	m.closed = true
}

func (m *mockPoolSender) IsClosed() bool {
	return m.closed
}

func newTestPoolForRuntime(t *testing.T, strategy string) *ConnectionPool {
	t.Helper()
	cfg := DefaultPoolConfig()
	cfg.LoadBalanceStrategy = strategy
	cfg.MinConnections = 1
	cfg.MaxConnections = 10
	cfg.ScaleUpCooldown = 0
	cfg.ScaleDownCooldown = 0
	cfg.ScaleUpThreshold = 0.6
	cfg.ScaleDownThreshold = 0.2
	return NewConnectionPool("127.0.0.1:9000", "grpc", func(addr string) inf.IRpcSender {
		return &mockPoolSender{}
	}, cfg, nil)
}

func addConnection(cp *ConnectionPool, id string, state ConnectionState, totalReq int64, avgResp int64, lastUsed time.Time) {
	conn := NewPoolConnection(id, &mockPoolSender{}, nil)
	conn.State = state
	conn.LastUsed = lastUsed
	conn.Metrics.TotalRequests = totalReq
	conn.Metrics.AvgResponseTime = avgResp
	cp.connections[id] = conn
}

func TestSelectConnectionRoundRobin(t *testing.T) {
	cp := newTestPoolForRuntime(t, "round_robin")
	healthy := []*PoolConnection{
		NewPoolConnection("c1", &mockPoolSender{}, nil),
		NewPoolConnection("c2", &mockPoolSender{}, nil),
	}

	first := cp.selectConnection(healthy)
	second := cp.selectConnection(healthy)
	if first == nil || second == nil {
		t.Fatalf("expected non-nil selected connection")
	}
	if first.ID == second.ID {
		t.Fatalf("expected round-robin to rotate connections, got same id %s", first.ID)
	}
}

func TestSelectConnectionLeastConnections(t *testing.T) {
	cp := newTestPoolForRuntime(t, "least_connections")
	c1 := NewPoolConnection("c1", &mockPoolSender{}, nil)
	c2 := NewPoolConnection("c2", &mockPoolSender{}, nil)
	c1.Metrics.TotalRequests = 20
	c2.Metrics.TotalRequests = 3

	selected := cp.selectConnection([]*PoolConnection{c1, c2})
	if selected == nil || selected.ID != "c2" {
		t.Fatalf("expected least-connection pick c2, got %+v", selected)
	}
}

func TestSelectConnectionFastestResponse(t *testing.T) {
	cp := newTestPoolForRuntime(t, "fastest_response")
	c1 := NewPoolConnection("c1", &mockPoolSender{}, nil)
	c2 := NewPoolConnection("c2", &mockPoolSender{}, nil)
	c1.Metrics.TotalRequests = 5
	c2.Metrics.TotalRequests = 8
	c1.Metrics.AvgResponseTime = 500
	c2.Metrics.AvgResponseTime = 100

	selected := cp.selectConnection([]*PoolConnection{c1, c2})
	if selected == nil || selected.ID != "c2" {
		t.Fatalf("expected fastest-response pick c2, got %+v", selected)
	}
}

func TestScaleDownRemovesOldestIdle(t *testing.T) {
	cp := newTestPoolForRuntime(t, "round_robin")
	now := time.Now()
	addConnection(cp, "old", StateIdle, 0, 0, now.Add(-10*time.Minute))
	addConnection(cp, "new", StateIdle, 0, 0, now.Add(-1*time.Minute))

	if err := cp.scaleDown(1); err != nil {
		t.Fatalf("scaleDown should succeed, got err=%v", err)
	}

	if _, exists := cp.connections["old"]; exists {
		t.Fatalf("expected oldest idle connection to be removed")
	}
	if _, exists := cp.connections["new"]; !exists {
		t.Fatalf("expected newer idle connection to remain")
	}
}

func TestScaleDownNoIdleReturnsError(t *testing.T) {
	cp := newTestPoolForRuntime(t, "round_robin")
	addConnection(cp, "active", StateActive, 0, 0, time.Now())

	err := cp.scaleDown(1)
	if err == nil {
		t.Fatalf("expected error when no idle connections")
	}
}

func TestCheckAndScaleUp(t *testing.T) {
	cp := newTestPoolForRuntime(t, "round_robin")
	cp.config.InitialConnections = 0
	cp.config.MaxConnections = 6
	cp.config.MinConnections = 1
	cp.config.ScaleUpThreshold = 0.5

	now := time.Now()
	addConnection(cp, "a1", StateActive, 0, 0, now)
	addConnection(cp, "a2", StateActive, 0, 0, now)
	addConnection(cp, "a3", StateActive, 0, 0, now)
	addConnection(cp, "a4", StateActive, 0, 0, now)

	before := len(cp.connections)
	cp.checkAndScale()
	after := len(cp.connections)
	if after <= before {
		t.Fatalf("expected scale up, before=%d after=%d", before, after)
	}
}

func TestCheckAndScaleDown(t *testing.T) {
	cp := newTestPoolForRuntime(t, "round_robin")
	cp.config.MinConnections = 2
	cp.config.ScaleDownThreshold = 0.9 // make scale down easier to trigger

	now := time.Now()
	addConnection(cp, "i1", StateIdle, 0, 0, now.Add(-4*time.Minute))
	addConnection(cp, "i2", StateIdle, 0, 0, now.Add(-3*time.Minute))
	addConnection(cp, "i3", StateIdle, 0, 0, now.Add(-2*time.Minute))
	addConnection(cp, "i4", StateIdle, 0, 0, now.Add(-1*time.Minute))

	before := len(cp.connections)
	cp.checkAndScale()
	after := len(cp.connections)
	if after >= before {
		t.Fatalf("expected scale down, before=%d after=%d", before, after)
	}
	if after < cp.config.MinConnections {
		t.Fatalf("scale down should not go below min connections, min=%d after=%d", cp.config.MinConnections, after)
	}
}

// ============================================================================
// P1-4.4: Pool Stop goroutine 退出验证
// ============================================================================

func TestPoolStop_Idempotent(t *testing.T) {
	cp := newTestPoolForRuntime(t, "round_robin")
	addConnection(cp, "c1", StateIdle, 0, 0, time.Now())

	if err := cp.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	cp.Stop()
	cp.Stop() // 第二次调用不应 panic
}

func TestPoolStop_GoroutineExit(t *testing.T) {
	cp := newTestPoolForRuntime(t, "round_robin")
	cp.config.HealthCheckInterval = 50 * time.Millisecond
	addConnection(cp, "c1", StateIdle, 0, 0, time.Now())

	if err := cp.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// 等一小段让后台 goroutine 执行至少一次
	time.Sleep(100 * time.Millisecond)

	// Stop 应该等 wg.Wait() 所有后台 goroutine 退出
	done := make(chan struct{})
	go func() {
		cp.Stop()
		close(done)
	}()

	select {
	case <-done:
		// 正常退出
	case <-time.After(5 * time.Second):
		t.Fatal("Pool Stop blocked for >5s, goroutines likely not exiting")
	}
}

func TestPoolStop_ClosesAllConnections(t *testing.T) {
	cp := newTestPoolForRuntime(t, "round_robin")
	s1 := &mockPoolSender{}
	s2 := &mockPoolSender{}
	cp.connections["c1"] = NewPoolConnection("c1", s1, nil)
	cp.connections["c2"] = NewPoolConnection("c2", s2, nil)

	if err := cp.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	cp.Stop()

	if !s1.closed || !s2.closed {
		t.Errorf("expected all senders closed: s1=%v s2=%v", s1.closed, s2.closed)
	}

	cp.connMutex.RLock()
	remaining := len(cp.connections)
	cp.connMutex.RUnlock()

	if remaining != 0 {
		t.Errorf("expected 0 connections after Stop, got %d", remaining)
	}
}

func TestPoolManagerClose_StopsAllPools(t *testing.T) {
	pm := NewPoolManager(nil)
	pm.RegisterCreator("grpc", func(addr string) inf.IRpcSender {
		return &mockPoolSender{}
	})
	pm.SetPoolConfig("grpc", DefaultPoolConfig())

	// GetOrCreatePool 内部已调用 Start()
	_, err := pm.GetOrCreatePool("10.0.0.1:9000", "grpc")
	if err != nil {
		t.Fatalf("create pool 1 failed: %v", err)
	}
	_, err = pm.GetOrCreatePool("10.0.0.2:9000", "grpc")
	if err != nil {
		t.Fatalf("create pool 2 failed: %v", err)
	}

	pm.Close()

	// Close 后 pools 应为空
	all := pm.GetAllPoolMetrics()
	if len(all) != 0 {
		t.Errorf("expected 0 pools after Close, got %d", len(all))
	}
}

func TestPoolManagerClose_Idempotent(t *testing.T) {
	pm := NewPoolManager(nil)
	pm.RegisterCreator("grpc", func(addr string) inf.IRpcSender {
		return &mockPoolSender{}
	})
	pm.SetPoolConfig("grpc", DefaultPoolConfig())

	_, err := pm.GetOrCreatePool("10.0.0.1:9000", "grpc")
	if err != nil {
		t.Fatalf("create pool failed: %v", err)
	}

	pm.Close()
	pm.Close() // 第二次不应 panic
}
