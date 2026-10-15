package metrics

import (
	"strings"
	"testing"

	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/event"
	pool "github.com/njtc406/emberengine/engine/pkg/rpc/client/pool"
	"github.com/njtc406/emberengine/engine/pkg/rpc/message/msgbus"
)

func TestSnapshotToText_Nil(t *testing.T) {
	if text := SnapshotToText(nil); text != "" {
		t.Errorf("nil snapshot should produce empty text, got: %q", text)
	}
}

func TestSnapshotToText_EmptySnapshot(t *testing.T) {
	info := &SnapshotInfo{}
	text := SnapshotToText(info)
	// 即使无 pool，Node 指标应该存在
	if !strings.Contains(text, "ember_node_uptime_seconds") {
		t.Errorf("empty snapshot should still have node metrics, got:\n%s", text)
	}
	// 不应该有 pool 指标
	if strings.Contains(text, "ember_pool_") {
		t.Errorf("empty pool should not produce pool metrics, got:\n%s", text)
	}
}

func TestSnapshotToText_NodeAndPool(t *testing.T) {
	info := &SnapshotInfo{
		Node: NodeInfo{
			NodeUID:      "test-node",
			UptimeSecs:   120,
			ServiceCount: 3,
			ClusterMode:  true,
		},
		PoolMetrics: map[string]*pool.PoolMetrics{
			"10.0.0.1:9000_grpc": {
				TotalConnections: 4,
				TotalRequests:    100,
			},
		},
	}

	text := SnapshotToText(info)

	// Node 指标
	if !strings.Contains(text, `ember_node_uptime_seconds{node="test-node"} 120`) {
		t.Errorf("missing node uptime in:\n%s", text)
	}

	// Pool 指标
	if !strings.Contains(text, `ember_pool_connections_total{pool="10.0.0.1:9000_grpc"} 4`) {
		t.Errorf("missing pool connections in:\n%s", text)
	}

	// Node 指标应该在 Pool 指标前面
	nodeIdx := strings.Index(text, "ember_node_uptime_seconds")
	poolIdx := strings.Index(text, "ember_pool_connections_total")
	if nodeIdx >= poolIdx {
		t.Errorf("node metrics should come before pool metrics")
	}
}

func TestSnapshotToSamples_Nil(t *testing.T) {
	samples := SnapshotToSamples(nil)
	if len(samples) != 0 {
		t.Errorf("nil info should produce 0 samples, got %d", len(samples))
	}
}

func TestSnapshotToSamples_Count(t *testing.T) {
	rm := &msgbus.RpcMetrics{CallTotal: 1}
	mbm := &def.MailboxMetrics{PostTotal: 1}
	em := &event.EventMetrics{TotalPublished: 1}
	info := &SnapshotInfo{
		Node: NodeInfo{NodeUID: "n"},
		PoolMetrics: map[string]*pool.PoolMetrics{
			"p1": {TotalConnections: 1},
			"p2": {TotalConnections: 2},
		},
		RpcMetrics:     rm,
		MailboxMetrics: mbm,
		EventMetrics:   em,
	}
	samples := SnapshotToSamples(info)
	// 7 node + 7 rpc + 4 mailbox + 4 event + 10*2 pool = 42
	if len(samples) != 42 {
		t.Errorf("samples count = %d, want 42", len(samples))
	}
}

func TestSnapshotToText_PoolSortedStable(t *testing.T) {
	// 只用 PoolMetrics 测试排序稳定性（避免 Go runtime 动态值干扰）
	pm := map[string]*pool.PoolMetrics{
		"z_pool": {TotalConnections: 1},
		"a_pool": {TotalConnections: 2},
		"m_pool": {TotalConnections: 3},
	}

	// PoolMetricsToText 多次调用结果应一致（无 Node 动态指标）
	text1 := PoolMetricsToText(pm)
	text2 := PoolMetricsToText(pm)
	if text1 != text2 {
		t.Errorf("pool text not stable across calls:\n--- call 1 ---\n%s\n--- call 2 ---\n%s", text1, text2)
	}

	// a_pool 应出现在 m_pool 前面，m_pool 在 z_pool 前面
	aIdx := strings.Index(text1, `pool="a_pool"`)
	mIdx := strings.Index(text1, `pool="m_pool"`)
	zIdx := strings.Index(text1, `pool="z_pool"`)
	if aIdx < 0 || mIdx < 0 || zIdx < 0 {
		t.Fatalf("missing pool labels in:\n%s", text1)
	}
	if aIdx >= mIdx || mIdx >= zIdx {
		t.Errorf("pools should be sorted alphabetically (a=%d, m=%d, z=%d)", aIdx, mIdx, zIdx)
	}
}

func TestSnapshotToText_NilPoolEntrySkipped(t *testing.T) {
	info := &SnapshotInfo{
		PoolMetrics: map[string]*pool.PoolMetrics{
			"good": {TotalConnections: 1},
			"nil":  nil,
		},
	}
	text := SnapshotToText(info)
	if strings.Contains(text, `pool="nil"`) {
		t.Errorf("nil pool entry should be skipped, got:\n%s", text)
	}
	if !strings.Contains(text, `pool="good"`) {
		t.Errorf("good pool should be present, got:\n%s", text)
	}
}
