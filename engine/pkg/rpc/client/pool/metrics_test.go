package pool

import (
	"sync"
	"testing"
	"time"

	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

// ============================================================================
// P1-2.2: PoolManager 聚合指标测试
//
// 覆盖场景：
// - 空 PoolManager：GetAllPoolMetrics 返回空 map
// - 多 pool 聚合：各 pool metrics 独立
// - 并发读取 metrics 不 panic / 不 race
// - Stop 后读取 metrics 不 panic
// - 单连接 metrics 更新后反映在 pool 级别
// ============================================================================

// --- 辅助构造 ---

func newTestPoolManager(t *testing.T) *PoolManager {
	t.Helper()
	pm := NewPoolManager(nil)
	pm.RegisterCreator("grpc", func(addr string) inf.IRpcSender {
		return &mockFactorySender{}
	})
	pm.SetPoolConfig("grpc", &PoolConfig{
		MinConnections:      2,
		MaxConnections:      5,
		InitialConnections:  2,
		ScaleUpThreshold:    0.8,
		ScaleDownThreshold:  0.3,
		ScaleUpCooldown:     time.Hour,
		ScaleDownCooldown:   time.Hour,
		HealthCheckInterval: time.Hour,
		HealthCheckTimeout:  time.Second,
		MaxConsecutiveFails: 3,
		MaxIdleTime:         time.Hour,
		MaxConnectionAge:    time.Hour,
		ConnectionTimeout:   10 * time.Second,
		LoadBalanceStrategy: "round_robin",
	})
	t.Cleanup(func() { pm.Close() })
	return pm
}

// --- 空 PoolManager ---

func TestGetAllPoolMetrics_Empty(t *testing.T) {
	pm := NewPoolManager(nil)
	t.Cleanup(func() { pm.Close() })

	all := pm.GetAllPoolMetrics()
	if len(all) != 0 {
		t.Errorf("empty PoolManager should return empty map, got %d entries", len(all))
	}
}

// --- 多 pool 聚合 ---

func TestGetAllPoolMetrics_MultiPool(t *testing.T) {
	pm := newTestPoolManager(t)

	addrs := []string{"10.0.0.1:9000", "10.0.0.2:9000", "10.0.0.3:9000"}
	for _, addr := range addrs {
		if _, err := pm.GetOrCreatePool(addr, "grpc"); err != nil {
			t.Fatalf("create pool %s failed: %v", addr, err)
		}
	}

	all := pm.GetAllPoolMetrics()
	if len(all) != 3 {
		t.Fatalf("expected 3 pool metrics, got %d", len(all))
	}

	for key, m := range all {
		if m == nil {
			t.Errorf("metrics for %s is nil", key)
			continue
		}
		// 新建 pool 应该有 InitialConnections 个连接
		if m.TotalConnections < 1 {
			t.Errorf("pool %s: TotalConnections = %d, want >= 1", key, m.TotalConnections)
		}
	}
}

// --- 单个 pool metrics 独立不互扰 ---

func TestGetPoolMetrics_SinglePool(t *testing.T) {
	pm := newTestPoolManager(t)

	pool, err := pm.GetOrCreatePool("10.0.0.1:9000", "grpc")
	if err != nil {
		t.Fatalf("create pool failed: %v", err)
	}

	m := pool.GetMetrics()
	if m.TotalRequests != 0 {
		t.Errorf("new pool TotalRequests = %d, want 0", m.TotalRequests)
	}
	if m.SuccessRate != 0 {
		t.Errorf("new pool SuccessRate = %f, want 0", m.SuccessRate)
	}
}

// --- 连接指标更新后在 pool 级别可见 ---

func TestPoolMetrics_AfterConnectionUpdate(t *testing.T) {
	pm := newTestPoolManager(t)

	pool, err := pm.GetOrCreatePool("10.0.0.1:9000", "grpc")
	if err != nil {
		t.Fatalf("create pool failed: %v", err)
	}

	// 获取一个连接并模拟请求
	conn, err := pool.GetConnection()
	if err != nil {
		t.Fatalf("get connection failed: %v", err)
	}

	conn.UpdateMetrics(true, 5*time.Millisecond)
	conn.UpdateMetrics(true, 10*time.Millisecond)
	conn.UpdateMetrics(false, 20*time.Millisecond)

	m := pool.GetMetrics()
	// pool 有 2 个连接，只有 1 个被更新了 3 次请求
	if m.TotalRequests != 3 {
		t.Errorf("TotalRequests = %d, want 3", m.TotalRequests)
	}
	if m.SuccessfulRequests != 2 {
		t.Errorf("SuccessfulRequests = %d, want 2", m.SuccessfulRequests)
	}
	if m.FailedRequests != 1 {
		t.Errorf("FailedRequests = %d, want 1", m.FailedRequests)
	}
	if m.SuccessRate < 0.6 || m.SuccessRate > 0.7 {
		t.Errorf("SuccessRate = %f, want ~0.667", m.SuccessRate)
	}
	if m.AvgResponseTime <= 0 {
		t.Errorf("AvgResponseTime = %d, want > 0", m.AvgResponseTime)
	}
}

// --- 并发读取 metrics ---

func TestGetAllPoolMetrics_ConcurrentRead(t *testing.T) {
	pm := newTestPoolManager(t)

	for i := 0; i < 5; i++ {
		addr := "10.0.0." + string(rune('1'+i)) + ":9000"
		if _, err := pm.GetOrCreatePool(addr, "grpc"); err != nil {
			t.Fatalf("create pool failed: %v", err)
		}
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				all := pm.GetAllPoolMetrics()
				if all == nil {
					t.Error("GetAllPoolMetrics returned nil")
					return
				}
			}
		}()
	}
	wg.Wait()
}

// --- Stop 后读取 metrics 不 panic ---

func TestPoolMetrics_AfterStop(t *testing.T) {
	pm := NewPoolManager(nil)
	pm.RegisterCreator("grpc", func(addr string) inf.IRpcSender {
		return &mockFactorySender{}
	})
	pm.SetPoolConfig("grpc", &PoolConfig{
		MinConnections:      1,
		MaxConnections:      3,
		InitialConnections:  1,
		ScaleUpThreshold:    0.8,
		ScaleDownThreshold:  0.3,
		ScaleUpCooldown:     time.Hour,
		ScaleDownCooldown:   time.Hour,
		HealthCheckInterval: time.Hour,
		HealthCheckTimeout:  time.Second,
		MaxConsecutiveFails: 3,
		MaxIdleTime:         time.Hour,
		MaxConnectionAge:    time.Hour,
		ConnectionTimeout:   10 * time.Second,
		LoadBalanceStrategy: "round_robin",
	})

	pool, err := pm.GetOrCreatePool("10.0.0.1:9000", "grpc")
	if err != nil {
		t.Fatalf("create pool failed: %v", err)
	}

	// 停止单个 pool
	pool.Stop()

	// 停止后 GetMetrics 不应 panic
	m := pool.GetMetrics()
	if m == nil {
		t.Fatal("GetMetrics after Stop should not be nil")
	}
	// 停止后连接被清除
	if m.TotalConnections != 0 {
		t.Errorf("TotalConnections after Stop = %d, want 0", m.TotalConnections)
	}

	// Close 整个 manager 后 GetAllPoolMetrics
	pm.Close()
	all := pm.GetAllPoolMetrics()
	if len(all) != 0 {
		t.Errorf("GetAllPoolMetrics after Close = %d entries, want 0", len(all))
	}
}

// --- GetPoolMetrics 不存在的 pool ---

func TestGetPoolMetrics_NotFound(t *testing.T) {
	pm := NewPoolManager(nil)
	t.Cleanup(func() { pm.Close() })

	_, err := pm.GetPoolMetrics("nonexistent", "grpc")
	if err == nil {
		t.Error("expected error for nonexistent pool")
	}
}

// --- 零连接池的 metrics ---

func TestPoolMetrics_ZeroConnections(t *testing.T) {
	// 直接构造空 ConnectionPool（不通过 factory 初始化连接）
	pool := NewConnectionPool("10.0.0.1:9000", "grpc", func(addr string) inf.IRpcSender {
		return &mockFactorySender{}
	}, DefaultPoolConfig(), nil)
	defer pool.Stop()

	m := pool.GetMetrics()
	if m.TotalConnections != 0 {
		t.Errorf("TotalConnections = %d, want 0", m.TotalConnections)
	}
	if m.AvgResponseTime != 0 {
		t.Errorf("AvgResponseTime = %d, want 0 (no connections)", m.AvgResponseTime)
	}
	if m.SuccessRate != 0 {
		t.Errorf("SuccessRate = %f, want 0 (no requests)", m.SuccessRate)
	}
}
