package pool

import (
	"context"
	"testing"
	"time"

	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

type mockFactorySender struct {
	closed bool
}

func (m *mockFactorySender) DeliverRequest(ctx context.Context, dispatcher inf.IRpcDispatcher, envelope inf.IEnvelope) error {
	return nil
}

func (m *mockFactorySender) DeliverResponse(ctx context.Context, dispatcher inf.IRpcDispatcher, envelope inf.IEnvelope) error {
	return nil
}

func (m *mockFactorySender) Close() {
	m.closed = true
}

func (m *mockFactorySender) IsClosed() bool {
	return m.closed
}

func TestPoolManagerGetOrCreatePoolNoCreator(t *testing.T) {
	pm := NewPoolManager(nil)
	t.Cleanup(func() { pm.Close() })

	_, err := pm.GetOrCreatePool("127.0.0.1:9000", "grpc")
	if err == nil {
		t.Fatalf("expected error when creator is not registered")
	}
}

func TestPoolManagerGetOrCreatePoolReuse(t *testing.T) {
	pm := NewPoolManager(nil)
	t.Cleanup(func() { pm.Close() })

	pm.RegisterCreator("grpc", func(addr string) inf.IRpcSender {
		return &mockFactorySender{}
	})
	pm.SetPoolConfig("grpc", &PoolConfig{
		MinConnections:      1,
		MaxConnections:      3,
		InitialConnections:  1,
		ScaleUpThreshold:    0.8,
		ScaleDownThreshold:  0.3,
		ScaleUpCooldown:     0,
		ScaleDownCooldown:   0,
		HealthCheckInterval: time.Hour,
		HealthCheckTimeout:  time.Second,
		MaxConsecutiveFails: 3,
		MaxIdleTime:         time.Minute,
		MaxConnectionAge:    5 * time.Minute,
		ConnectionTimeout:   10 * time.Second,
		LoadBalanceStrategy: "round_robin",
	})

	p1, err := pm.GetOrCreatePool("127.0.0.1:9000", "grpc")
	if err != nil {
		t.Fatalf("create pool failed: %v", err)
	}
	p2, err := pm.GetOrCreatePool("127.0.0.1:9000", "grpc")
	if err != nil {
		t.Fatalf("get existing pool failed: %v", err)
	}
	if p1 != p2 {
		t.Fatalf("expected same pool instance for same address/type")
	}
}

func TestPoolManagerGetPoolMetricsAndRemove(t *testing.T) {
	pm := NewPoolManager(nil)
	t.Cleanup(func() { pm.Close() })

	pm.RegisterCreator("grpc", func(addr string) inf.IRpcSender {
		return &mockFactorySender{}
	})

	if _, err := pm.GetOrCreatePool("127.0.0.1:9001", "grpc"); err != nil {
		t.Fatalf("create pool failed: %v", err)
	}

	metrics, err := pm.GetPoolMetrics("127.0.0.1:9001", "grpc")
	if err != nil {
		t.Fatalf("get pool metrics failed: %v", err)
	}
	if metrics == nil {
		t.Fatalf("expected non-nil metrics")
	}

	all := pm.GetAllPoolMetrics()
	if len(all) == 0 {
		t.Fatalf("expected all metrics map to include created pool")
	}

	pm.RemovePool("127.0.0.1:9001", "grpc")
	if _, err := pm.GetPoolMetrics("127.0.0.1:9001", "grpc"); err == nil {
		t.Fatalf("expected not found error after remove")
	}
}
