package metrics

import (
	"github.com/njtc406/emberengine/engine/pkg/def"
)

// --- Mailbox Metrics 转换 ---

// mailboxMetricDescs 定义 Mailbox 指标描述
var mailboxMetricDescs = struct {
	PostTotal           MetricDesc
	SuspendedTotal      MetricDesc
	RejectedTotal       MetricDesc
	DispatchFailedTotal MetricDesc
}{
	PostTotal:           MetricDesc{Name: "ember_mailbox_post_total", Help: "Total number of PostJob calls across all mailboxes", Type: Counter},
	SuspendedTotal:      MetricDesc{Name: "ember_mailbox_suspended_total", Help: "Total PostJob calls rejected due to mailbox suspension", Type: Counter},
	RejectedTotal:       MetricDesc{Name: "ember_mailbox_rejected_total", Help: "Total PostJob calls rejected by middleware", Type: Counter},
	DispatchFailedTotal: MetricDesc{Name: "ember_mailbox_dispatch_failed_total", Help: "Total PostJob calls where DispatchJob failed", Type: Counter},
}

// MailboxMetricsToSamples 将 MailboxMetrics 转换为 Prometheus 样本列表。
func MailboxMetricsToSamples(m *def.MailboxMetrics) []MetricSample {
	if m == nil {
		return nil
	}

	samples := []MetricSample{
		{Desc: mailboxMetricDescs.PostTotal, Value: float64(m.PostTotal)},
		{Desc: mailboxMetricDescs.SuspendedTotal, Value: float64(m.SuspendedTotal)},
		{Desc: mailboxMetricDescs.RejectedTotal, Value: float64(m.RejectedTotal)},
		{Desc: mailboxMetricDescs.DispatchFailedTotal, Value: float64(m.DispatchFailedTotal)},
		{Desc: MetricDesc{Name: "ember_mailbox_queue_full_rejected_total", Help: "Main queue full rejections across live services; may decrease on service removal", Type: Counter}, Value: float64(m.QueueFullRejectedTotal)},
		{Desc: MetricDesc{Name: "ember_mailbox_read_queue_full_discarded_total", Help: "RW secondary queue full discards across live services; not synchronous rejections and may decrease on service removal", Type: Counter}, Value: float64(m.ReadQueueFullDiscardedTotal)},
		{Desc: MetricDesc{Name: "ember_mailbox_queue_length", Help: "Approximate nonnegative main queue length excluding readCh, pending and executing jobs", Type: Gauge}, Value: float64(m.QueueLength)},
		{Desc: MetricDesc{Name: "ember_mailbox_queue_observed_high_watermark", Help: "Maximum global main queue length observed at collection, not an exact lifetime peak", Type: Gauge}, Value: float64(m.ObservedQueueHighWatermark)},
	}
	switch m.QueueBackend {
	case "bounded", "unbounded", "mixed":
		samples = append(samples, MetricSample{Desc: MetricDesc{Name: "ember_mailbox_queue_backend_info", Help: "Backend composition of observable mailboxes", Type: Gauge}, Labels: map[string]string{"backend": m.QueueBackend}, Value: 1})
	}
	if m.QueueBounded && m.QueueBackend == "bounded" {
		samples = append(samples, MetricSample{Desc: MetricDesc{Name: "ember_mailbox_queue_capacity", Help: "Main queue capacity; present only for a nonempty all-bounded mailbox set", Type: Gauge}, Value: float64(m.QueueCapacity)})
	}
	return samples
}

// MailboxMetricsToText 将 MailboxMetrics 转换为 Prometheus exposition text。
func MailboxMetricsToText(m *def.MailboxMetrics) string {
	return SamplesToText(MailboxMetricsToSamples(m))
}
