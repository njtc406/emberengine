package metrics

import (
	"github.com/njtc406/emberengine/engine/pkg/rpc/message/msgbus"
)

// --- RPC Metrics 转换 ---

// rpcMetricDescs 定义 RPC 指标描述
var rpcMetricDescs = struct {
	CallTotal       MetricDesc
	CallErrors      MetricDesc
	CallInFlight    MetricDesc
	AsyncCallTotal  MetricDesc
	AsyncCallErrors MetricDesc
	SendTotal       MetricDesc
	SendErrors      MetricDesc
}{
	CallTotal:       MetricDesc{Name: "ember_rpc_call_total", Help: "Total number of synchronous RPC calls", Type: Counter},
	CallErrors:      MetricDesc{Name: "ember_rpc_call_errors_total", Help: "Total number of failed synchronous RPC calls", Type: Counter},
	CallInFlight:    MetricDesc{Name: "ember_rpc_call_in_flight", Help: "Number of synchronous RPC calls currently in progress", Type: Gauge},
	AsyncCallTotal:  MetricDesc{Name: "ember_rpc_async_call_total", Help: "Total number of asynchronous RPC calls", Type: Counter},
	AsyncCallErrors: MetricDesc{Name: "ember_rpc_async_call_errors_total", Help: "Total number of failed asynchronous RPC calls", Type: Counter},
	SendTotal:       MetricDesc{Name: "ember_rpc_send_total", Help: "Total number of fire-and-forget RPC sends", Type: Counter},
	SendErrors:      MetricDesc{Name: "ember_rpc_send_errors_total", Help: "Total number of failed fire-and-forget RPC sends", Type: Counter},
}

// RpcMetricsToSamples 将 RpcMetrics 转换为 Prometheus 样本列表。
func RpcMetricsToSamples(m *msgbus.RpcMetrics) []MetricSample {
	if m == nil {
		return nil
	}

	return []MetricSample{
		{Desc: rpcMetricDescs.CallTotal, Value: float64(m.CallTotal)},
		{Desc: rpcMetricDescs.CallErrors, Value: float64(m.CallErrors)},
		{Desc: rpcMetricDescs.CallInFlight, Value: float64(m.CallInFlight)},
		{Desc: rpcMetricDescs.AsyncCallTotal, Value: float64(m.AsyncCallTotal)},
		{Desc: rpcMetricDescs.AsyncCallErrors, Value: float64(m.AsyncCallErrors)},
		{Desc: rpcMetricDescs.SendTotal, Value: float64(m.SendTotal)},
		{Desc: rpcMetricDescs.SendErrors, Value: float64(m.SendErrors)},
	}
}

// RpcMetricsToText 将 RpcMetrics 转换为 Prometheus exposition text。
func RpcMetricsToText(m *msgbus.RpcMetrics) string {
	return SamplesToText(RpcMetricsToSamples(m))
}
