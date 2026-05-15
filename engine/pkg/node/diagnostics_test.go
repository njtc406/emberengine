package node

import (
	"strings"
	"testing"

	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/event"
	"github.com/njtc406/emberengine/engine/pkg/metrics"
	"github.com/njtc406/emberengine/engine/pkg/rpc/client/pool"
)

func TestGetRuntimeSnapshotNilNode(t *testing.T) {
	var n *Node
	s := n.GetRuntimeSnapshot()
	if s.NodeUID != "" || s.ClusterMode || s.UptimeSecs != 0 {
		t.Fatalf("unexpected snapshot for nil node: %#v", s)
	}
}

func TestGetRuntimeSnapshotZeroNode(t *testing.T) {
	n := &Node{}
	s := n.GetRuntimeSnapshot()
	if s.NodeUID != "" {
		t.Fatalf("expected empty node uid for zero node, got %q", s.NodeUID)
	}
	if s.ClusterMode {
		t.Fatalf("expected cluster mode false for zero node")
	}
	if s.Service.ServiceCount != 0 {
		t.Fatalf("expected service count 0 for zero node, got %d", s.Service.ServiceCount)
	}
}

// TestRuntimeSnapshotPoolMetricsToText 验证 Node 快照中的 PoolMetrics
// 可以直接传给 metrics.PoolMetricsToText 输出 Prometheus 文本。
func TestRuntimeSnapshotPoolMetricsToText(t *testing.T) {
	// 构造带 PoolMetrics 的快照（不需要真实 Node）
	snapshot := RuntimeSnapshot{
		PoolMetrics: map[string]*pool.PoolMetrics{
			"10.0.0.1:9000_grpc": {
				TotalConnections:   4,
				ActiveConnections:  2,
				TotalRequests:      100,
				SuccessfulRequests: 95,
				FailedRequests:     5,
				SuccessRate:        0.95,
			},
		},
		PoolKeys: []string{"10.0.0.1:9000_grpc"},
	}

	text := metrics.PoolMetricsToText(snapshot.PoolMetrics)
	if text == "" {
		t.Fatal("PoolMetricsToText should produce non-empty output")
	}
	if !strings.Contains(text, "ember_pool_connections_total") {
		t.Error("output should contain ember_pool_connections_total")
	}
	if !strings.Contains(text, `pool="10.0.0.1:9000_grpc"`) {
		t.Error("output should contain pool label")
	}
}

// TestRuntimeSnapshotNilPoolMetrics 验证 PoolMetrics 为 nil 时不 panic。
func TestRuntimeSnapshotNilPoolMetrics(t *testing.T) {
	snapshot := RuntimeSnapshot{}
	text := metrics.PoolMetricsToText(snapshot.PoolMetrics)
	if text != "" {
		t.Errorf("nil PoolMetrics should produce empty text, got: %s", text)
	}
}

// --- P2-1: ToSnapshotInfo / SnapshotToText 集成测试 ---

func TestToSnapshotInfo_Basic(t *testing.T) {
	snapshot := RuntimeSnapshot{
		NodeUID:     "node-abc",
		ClusterMode: true,
		UptimeSecs:  600,
		PoolMetrics: map[string]*pool.PoolMetrics{
			"10.0.0.1:9000_grpc": {TotalConnections: 4, TotalRequests: 100},
		},
	}
	snapshot.Service.ServiceCount = 3

	info := snapshot.ToSnapshotInfo()
	if info.Node.NodeUID != "node-abc" {
		t.Errorf("NodeUID = %q, want %q", info.Node.NodeUID, "node-abc")
	}
	if info.Node.UptimeSecs != 600 {
		t.Errorf("UptimeSecs = %d, want 600", info.Node.UptimeSecs)
	}
	if info.Node.ServiceCount != 3 {
		t.Errorf("ServiceCount = %d, want 3", info.Node.ServiceCount)
	}
	if !info.Node.ClusterMode {
		t.Error("ClusterMode should be true")
	}
	if len(info.PoolMetrics) != 1 {
		t.Errorf("PoolMetrics len = %d, want 1", len(info.PoolMetrics))
	}
}

func TestToSnapshotInfo_FullText(t *testing.T) {
	snapshot := RuntimeSnapshot{
		NodeUID:    "node-x",
		UptimeSecs: 42,
		PoolMetrics: map[string]*pool.PoolMetrics{
			"pool1": {TotalConnections: 2},
		},
	}
	snapshot.Service.ServiceCount = 1

	text := metrics.SnapshotToText(snapshot.ToSnapshotInfo())
	if !strings.Contains(text, `ember_node_uptime_seconds{node="node-x"} 42`) {
		t.Errorf("missing uptime in:\n%s", text)
	}
	if !strings.Contains(text, `ember_pool_connections_total{pool="pool1"} 2`) {
		t.Errorf("missing pool metric in:\n%s", text)
	}
}

func TestToSnapshotInfo_Empty(t *testing.T) {
	snapshot := RuntimeSnapshot{}
	info := snapshot.ToSnapshotInfo()
	text := metrics.SnapshotToText(info)
	// 应该有 node 指标，无 pool 指标
	if !strings.Contains(text, "ember_node_uptime_seconds") {
		t.Errorf("empty snapshot should have node metrics, got:\n%s", text)
	}
	if strings.Contains(text, "ember_pool_") {
		t.Errorf("empty snapshot should not have pool metrics, got:\n%s", text)
	}
}

// --- P2-4: Mailbox/Event 指标集成到 Snapshot ---

func TestToSnapshotInfo_MailboxAndEvent(t *testing.T) {
	mbm := &def.MailboxMetrics{PostTotal: 50, SuspendedTotal: 3}
	em := &event.EventMetrics{TotalPublished: 200, TotalDelivered: 180}
	snapshot := RuntimeSnapshot{
		NodeUID:        "node-p2-4",
		UptimeSecs:     10,
		MailboxMetrics: mbm,
		EventMetrics:   em,
	}
	snapshot.Service.ServiceCount = 1

	info := snapshot.ToSnapshotInfo()
	if info.MailboxMetrics == nil {
		t.Fatal("MailboxMetrics should not be nil")
	}
	if info.MailboxMetrics.PostTotal != 50 {
		t.Errorf("MailboxMetrics.PostTotal = %d, want 50", info.MailboxMetrics.PostTotal)
	}
	if info.EventMetrics == nil {
		t.Fatal("EventMetrics should not be nil")
	}
	if info.EventMetrics.TotalPublished != 200 {
		t.Errorf("EventMetrics.TotalPublished = %d, want 200", info.EventMetrics.TotalPublished)
	}

	text := metrics.SnapshotToText(info)
	if !strings.Contains(text, "ember_mailbox_post_total") {
		t.Errorf("missing mailbox metrics in:\n%s", text)
	}
	if !strings.Contains(text, "ember_event_published_total") {
		t.Errorf("missing event metrics in:\n%s", text)
	}
}
