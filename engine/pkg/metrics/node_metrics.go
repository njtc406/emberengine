package metrics

import "runtime"

// --- Node-level Metrics ---

// nodeMetricDescs 定义 Node 级指标描述
var nodeMetricDescs = struct {
	Uptime       MetricDesc
	Services     MetricDesc
	ClusterMode  MetricDesc
	GoRoutines   MetricDesc
	GoMaxProcs   MetricDesc
	GoGCPauseNs  MetricDesc
	GoAllocBytes MetricDesc
}{
	Uptime:       MetricDesc{Name: "ember_node_uptime_seconds", Help: "Node uptime in seconds", Type: Gauge},
	Services:     MetricDesc{Name: "ember_node_services_total", Help: "Total number of running services", Type: Gauge},
	ClusterMode:  MetricDesc{Name: "ember_node_cluster_mode", Help: "Whether node is in cluster mode (1=true, 0=false)", Type: Gauge},
	GoRoutines:   MetricDesc{Name: "ember_go_goroutines", Help: "Number of goroutines", Type: Gauge},
	GoMaxProcs:   MetricDesc{Name: "ember_go_maxprocs", Help: "Value of GOMAXPROCS", Type: Gauge},
	GoGCPauseNs:  MetricDesc{Name: "ember_go_gc_pause_ns", Help: "Most recent GC pause duration in nanoseconds", Type: Gauge},
	GoAllocBytes: MetricDesc{Name: "ember_go_alloc_bytes", Help: "Current heap allocation in bytes", Type: Gauge},
}

// NodeInfo 携带 Node 级指标所需的基本信息。
// 由调用方（通常是 node 包）填充，避免 metrics 包反向依赖 node 包。
type NodeInfo struct {
	NodeUID      string
	UptimeSecs   int64
	ServiceCount int
	ClusterMode  bool
}

// NodeSamples 返回 Node 级指标样本。
// 同时采集 Go 运行时指标（goroutine 数、GC、内存）。
func NodeSamples(info NodeInfo) []MetricSample {
	labels := map[string]string{"node": info.NodeUID}
	if info.NodeUID == "" {
		labels = nil // 无 node label 时不输出空 label
	}

	clusterVal := float64(0)
	if info.ClusterMode {
		clusterVal = 1
	}

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	return []MetricSample{
		{Desc: nodeMetricDescs.Uptime, Labels: labels, Value: float64(info.UptimeSecs)},
		{Desc: nodeMetricDescs.Services, Labels: labels, Value: float64(info.ServiceCount)},
		{Desc: nodeMetricDescs.ClusterMode, Labels: labels, Value: clusterVal},
		{Desc: nodeMetricDescs.GoRoutines, Labels: labels, Value: float64(runtime.NumGoroutine())},
		{Desc: nodeMetricDescs.GoMaxProcs, Labels: labels, Value: float64(runtime.GOMAXPROCS(0))},
		{Desc: nodeMetricDescs.GoGCPauseNs, Labels: labels, Value: float64(lastGCPauseNs(&memStats))},
		{Desc: nodeMetricDescs.GoAllocBytes, Labels: labels, Value: float64(memStats.Alloc)},
	}
}

// lastGCPauseNs 返回最近一次 GC 暂停时间（纳秒）。
func lastGCPauseNs(m *runtime.MemStats) uint64 {
	if m.NumGC == 0 {
		return 0
	}
	// PauseNs 是环形缓冲，最近一次在 (NumGC+255)%256
	return m.PauseNs[(m.NumGC+255)%256]
}
