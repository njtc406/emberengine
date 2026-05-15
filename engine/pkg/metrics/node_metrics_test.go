package metrics

import (
	"strings"
	"testing"
)

func TestNodeSamples_Basic(t *testing.T) {
	info := NodeInfo{
		NodeUID:      "node-1",
		UptimeSecs:   300,
		ServiceCount: 5,
		ClusterMode:  true,
	}
	samples := NodeSamples(info)

	// 应该有 7 个样本（uptime, services, cluster_mode, goroutines, maxprocs, gc_pause, alloc）
	if len(samples) != 7 {
		t.Fatalf("NodeSamples count = %d, want 7", len(samples))
	}

	text := SamplesToText(samples)

	// 验证 node label
	if !strings.Contains(text, `node="node-1"`) {
		t.Errorf("missing node label in:\n%s", text)
	}

	// 验证 uptime
	if !strings.Contains(text, "ember_node_uptime_seconds") {
		t.Errorf("missing uptime metric in:\n%s", text)
	}
	if !strings.Contains(text, `ember_node_uptime_seconds{node="node-1"} 300`) {
		t.Errorf("wrong uptime value in:\n%s", text)
	}

	// 验证 services
	if !strings.Contains(text, `ember_node_services_total{node="node-1"} 5`) {
		t.Errorf("wrong services value in:\n%s", text)
	}

	// 验证 cluster_mode = 1
	if !strings.Contains(text, `ember_node_cluster_mode{node="node-1"} 1`) {
		t.Errorf("cluster mode should be 1 for true, got:\n%s", text)
	}

	// 验证 Go 运行时指标存在
	for _, name := range []string{
		"ember_go_goroutines",
		"ember_go_maxprocs",
		"ember_go_gc_pause_ns",
		"ember_go_alloc_bytes",
	} {
		if !strings.Contains(text, name) {
			t.Errorf("missing Go runtime metric %s in:\n%s", name, text)
		}
	}
}

func TestNodeSamples_ClusterModeFalse(t *testing.T) {
	info := NodeInfo{
		NodeUID:     "node-2",
		ClusterMode: false,
	}
	samples := NodeSamples(info)
	text := SamplesToText(samples)
	if !strings.Contains(text, `ember_node_cluster_mode{node="node-2"} 0`) {
		t.Errorf("cluster mode should be 0 for false, got:\n%s", text)
	}
}

func TestNodeSamples_EmptyUID(t *testing.T) {
	info := NodeInfo{}
	samples := NodeSamples(info)
	if len(samples) != 7 {
		t.Fatalf("NodeSamples count = %d, want 7 even with empty UID", len(samples))
	}
	text := SamplesToText(samples)
	// 空 UID 时不应有 label braces 中的 node=""
	if strings.Contains(text, `node=""`) {
		t.Errorf("empty UID should produce no node label, got:\n%s", text)
	}
	// 指标名应该存在且无 label
	if !strings.Contains(text, "ember_node_uptime_seconds 0") {
		t.Errorf("bare metric with zero uptime expected, got:\n%s", text)
	}
}

func TestNodeSamples_GoRoutinesPositive(t *testing.T) {
	samples := NodeSamples(NodeInfo{NodeUID: "test"})
	for _, s := range samples {
		if s.Desc.Name == "ember_go_goroutines" {
			if s.Value <= 0 {
				t.Errorf("goroutines should be positive, got %g", s.Value)
			}
			return
		}
	}
	t.Error("ember_go_goroutines not found in samples")
}
