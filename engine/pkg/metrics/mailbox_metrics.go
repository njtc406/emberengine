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

	return []MetricSample{
		{Desc: mailboxMetricDescs.PostTotal, Value: float64(m.PostTotal)},
		{Desc: mailboxMetricDescs.SuspendedTotal, Value: float64(m.SuspendedTotal)},
		{Desc: mailboxMetricDescs.RejectedTotal, Value: float64(m.RejectedTotal)},
		{Desc: mailboxMetricDescs.DispatchFailedTotal, Value: float64(m.DispatchFailedTotal)},
	}
}

// MailboxMetricsToText 将 MailboxMetrics 转换为 Prometheus exposition text。
func MailboxMetricsToText(m *def.MailboxMetrics) string {
	return SamplesToText(MailboxMetricsToSamples(m))
}
