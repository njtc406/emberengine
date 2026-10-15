package metrics

import (
	"strings"
	"testing"

	pool "github.com/njtc406/emberengine/engine/pkg/rpc/client/pool"
)

func TestPoolMetricsToText_Empty(t *testing.T) {
	text := PoolMetricsToText(nil)
	if text != "" {
		t.Errorf("nil input should produce empty text, got: %s", text)
	}
	text = PoolMetricsToText(map[string]*pool.PoolMetrics{})
	if text != "" {
		t.Errorf("empty map should produce empty text, got: %s", text)
	}
}

func TestPoolMetricsToText_SinglePool(t *testing.T) {
	m := map[string]*pool.PoolMetrics{
		"10.0.0.1:9000_grpc": {
			TotalConnections:   4,
			ActiveConnections:  2,
			IdleConnections:    1,
			UnhealthyConns:     1,
			TotalRequests:      100,
			SuccessfulRequests: 95,
			FailedRequests:     5,
			AvgResponseTime:    1500000, // 1.5ms
			SuccessRate:        0.95,
			ScaleOperations:    3,
		},
	}

	text := PoolMetricsToText(m)

	// 验证 HELP/TYPE 存在
	if !strings.Contains(text, "# HELP ember_pool_connections_total") {
		t.Error("missing HELP for ember_pool_connections_total")
	}
	if !strings.Contains(text, "# TYPE ember_pool_connections_total gauge") {
		t.Error("missing TYPE for ember_pool_connections_total")
	}
	if !strings.Contains(text, "# TYPE ember_pool_requests_total counter") {
		t.Error("missing TYPE counter for ember_pool_requests_total")
	}

	// 验证值
	if !strings.Contains(text, `ember_pool_connections_total{pool="10.0.0.1:9000_grpc"} 4`) {
		t.Errorf("wrong total connections value in:\n%s", text)
	}
	if !strings.Contains(text, `ember_pool_requests_total{pool="10.0.0.1:9000_grpc"} 100`) {
		t.Errorf("wrong total requests value in:\n%s", text)
	}
	if !strings.Contains(text, `ember_pool_success_rate{pool="10.0.0.1:9000_grpc"} 0.95`) {
		t.Errorf("wrong success rate value in:\n%s", text)
	}
}

func TestPoolMetricsToText_MultiPool(t *testing.T) {
	m := map[string]*pool.PoolMetrics{
		"10.0.0.1:9000_grpc": {TotalConnections: 4, TotalRequests: 100},
		"10.0.0.2:9000_grpc": {TotalConnections: 2, TotalRequests: 50},
	}

	text := PoolMetricsToText(m)

	// 两个 pool 的指标应该都存在
	if !strings.Contains(text, `pool="10.0.0.1:9000_grpc"`) {
		t.Error("missing pool 10.0.0.1")
	}
	if !strings.Contains(text, `pool="10.0.0.2:9000_grpc"`) {
		t.Error("missing pool 10.0.0.2")
	}
}

func TestPoolMetricsToText_NilPoolInMap(t *testing.T) {
	m := map[string]*pool.PoolMetrics{
		"10.0.0.1:9000_grpc": nil,
		"10.0.0.2:9000_grpc": {TotalConnections: 2},
	}

	text := PoolMetricsToText(m)
	if strings.Contains(text, "10.0.0.1") {
		t.Error("nil pool metrics should be skipped")
	}
	if !strings.Contains(text, "10.0.0.2") {
		t.Error("non-nil pool should be present")
	}
}

func TestPoolMetricsToSamples_Count(t *testing.T) {
	m := map[string]*pool.PoolMetrics{
		"pool1": {TotalConnections: 1},
		"pool2": {TotalConnections: 2},
	}

	samples := PoolMetricsToSamples(m)
	// 10 指标 * 2 pool = 20 样本
	if len(samples) != 20 {
		t.Errorf("samples count = %d, want 20", len(samples))
	}
}

func TestEscapeLabelValue(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`simple`, `simple`},
		{`has"quote`, `has\"quote`},
		{`has\back`, `has\\back`},
		{"has\nnewline", `has\nnewline`},
	}
	for _, c := range cases {
		got := escapeLabelValue(c.in)
		if got != c.want {
			t.Errorf("escapeLabelValue(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPoolMetricsToText_ZeroMetrics(t *testing.T) {
	m := map[string]*pool.PoolMetrics{
		"empty_pool": {},
	}
	text := PoolMetricsToText(m)
	if !strings.Contains(text, `ember_pool_connections_total{pool="empty_pool"} 0`) {
		t.Errorf("zero values should output 0:\n%s", text)
	}
}
