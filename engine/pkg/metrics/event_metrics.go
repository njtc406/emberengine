package metrics

import (
	"github.com/njtc406/emberengine/engine/pkg/event"
)

// --- Event Metrics 转换 ---

// eventMetricDescs 定义 Event 指标描述
var eventMetricDescs = struct {
	Published MetricDesc
	Delivered MetricDesc
	Throttled MetricDesc
	Batched   MetricDesc
}{
	Published: MetricDesc{Name: "ember_event_published_total", Help: "Total number of events published to the event bus", Type: Counter},
	Delivered: MetricDesc{Name: "ember_event_delivered_total", Help: "Total number of events delivered to subscribers", Type: Counter},
	Throttled: MetricDesc{Name: "ember_event_throttled_total", Help: "Total number of events rejected by throttling", Type: Counter},
	Batched:   MetricDesc{Name: "ember_event_batched_total", Help: "Total number of events processed through batching", Type: Counter},
}

// EventMetricsToSamples 将 EventMetrics 转换为 Prometheus 样本列表。
func EventMetricsToSamples(m *event.EventMetrics) []MetricSample {
	if m == nil {
		return nil
	}

	return []MetricSample{
		{Desc: eventMetricDescs.Published, Value: float64(m.TotalPublished)},
		{Desc: eventMetricDescs.Delivered, Value: float64(m.TotalDelivered)},
		{Desc: eventMetricDescs.Throttled, Value: float64(m.TotalThrottled)},
		{Desc: eventMetricDescs.Batched, Value: float64(m.TotalBatched)},
	}
}

// EventMetricsToText 将 EventMetrics 转换为 Prometheus exposition text。
func EventMetricsToText(m *event.EventMetrics) string {
	return SamplesToText(EventMetricsToSamples(m))
}
