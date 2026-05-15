package mailbox

import (
	"sync/atomic"

	"github.com/njtc406/emberengine/engine/pkg/def"
)

// mailboxMetricsCollector 使用原子计数器采集 Mailbox 投递路径的指标。
// 每个 Mailbox 实例内嵌一个 collector；如需跨 Service 聚合，
// 由上层（ServiceManager）遍历各 Service 的 Mailbox 汇总。
type mailboxMetricsCollector struct {
	postTotal           atomic.Int64
	suspendedTotal      atomic.Int64
	rejectedTotal       atomic.Int64
	dispatchFailedTotal atomic.Int64
}

// snapshot 返回当前计数器的不可变副本。
func (c *mailboxMetricsCollector) snapshot() def.MailboxMetrics {
	return def.MailboxMetrics{
		PostTotal:           c.postTotal.Load(),
		SuspendedTotal:      c.suspendedTotal.Load(),
		RejectedTotal:       c.rejectedTotal.Load(),
		DispatchFailedTotal: c.dispatchFailedTotal.Load(),
	}
}

// GetMailboxMetrics 返回该 Mailbox 的指标快照。
func (m *Mailbox) GetMailboxMetrics() def.MailboxMetrics {
	return m.metrics.snapshot()
}
