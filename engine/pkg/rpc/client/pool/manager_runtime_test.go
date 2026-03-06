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
