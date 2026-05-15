package metrics

import (
	"sort"

	pool "github.com/njtc406/emberengine/engine/pkg/rpc/client/pool"
)

// --- Pool Metrics 转换 ---

// poolMetricDescs 定义 PoolMetrics 对应的 Prometheus 指标描述
var poolMetricDescs = []MetricDesc{
	{Name: "ember_pool_connections_total", Help: "Total number of connections in the pool", Type: Gauge},
	{Name: "ember_pool_connections_active", Help: "Number of active connections", Type: Gauge},
	{Name: "ember_pool_connections_idle", Help: "Number of idle connections", Type: Gauge},
	{Name: "ember_pool_connections_unhealthy", Help: "Number of unhealthy connections", Type: Gauge},
	{Name: "ember_pool_requests_total", Help: "Total number of requests", Type: Counter},
	{Name: "ember_pool_requests_successful", Help: "Total number of successful requests", Type: Counter},
	{Name: "ember_pool_requests_failed", Help: "Total number of failed requests", Type: Counter},
	{Name: "ember_pool_response_time_avg_ns", Help: "Average response time in nanoseconds", Type: Gauge},
	{Name: "ember_pool_success_rate", Help: "Request success rate (0.0 to 1.0)", Type: Gauge},
	{Name: "ember_pool_scale_operations_total", Help: "Total number of scale operations", Type: Counter},
}

// PoolMetricsToSamples 将 PoolMetrics map 转换为指标样本列表。
// poolKey 格式通常为 "addr_rpcType"，作为 pool label 值。
// 输出按 poolKey 字母序排列，保证 Prometheus text 稳定。
func PoolMetricsToSamples(allMetrics map[string]*pool.PoolMetrics) []MetricSample {
	if len(allMetrics) == 0 {
		return nil
	}

	// 排序 pool keys 保证输出稳定
	keys := make([]string, 0, len(allMetrics))
	for k := range allMetrics {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	samples := make([]MetricSample, 0, len(allMetrics)*len(poolMetricDescs))
	for _, poolKey := range keys {
		m := allMetrics[poolKey]
		if m == nil {
			continue
		}
		labels := map[string]string{"pool": poolKey}
		samples = append(samples,
			MetricSample{Desc: poolMetricDescs[0], Labels: labels, Value: float64(m.TotalConnections)},
			MetricSample{Desc: poolMetricDescs[1], Labels: labels, Value: float64(m.ActiveConnections)},
			MetricSample{Desc: poolMetricDescs[2], Labels: labels, Value: float64(m.IdleConnections)},
			MetricSample{Desc: poolMetricDescs[3], Labels: labels, Value: float64(m.UnhealthyConns)},
			MetricSample{Desc: poolMetricDescs[4], Labels: labels, Value: float64(m.TotalRequests)},
			MetricSample{Desc: poolMetricDescs[5], Labels: labels, Value: float64(m.SuccessfulRequests)},
			MetricSample{Desc: poolMetricDescs[6], Labels: labels, Value: float64(m.FailedRequests)},
			MetricSample{Desc: poolMetricDescs[7], Labels: labels, Value: float64(m.AvgResponseTime)},
			MetricSample{Desc: poolMetricDescs[8], Labels: labels, Value: m.SuccessRate},
			MetricSample{Desc: poolMetricDescs[9], Labels: labels, Value: float64(m.ScaleOperations)},
		)
	}
	return samples
}

// PoolMetricsToText 将 PoolMetrics map 转换为 Prometheus exposition text 格式。
// 输出可直接作为 /metrics HTTP 响应体，Content-Type 为
// "text/plain; version=0.0.4; charset=utf-8"。
func PoolMetricsToText(allMetrics map[string]*pool.PoolMetrics) string {
	return SamplesToText(PoolMetricsToSamples(allMetrics))
}
