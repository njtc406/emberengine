package mailbox

import (
	"sync/atomic"

	"github.com/njtc406/emberengine/v2/engine/pkg/def"
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
	s := m.metrics.snapshot()
	p := m.workerPool
	if p == nil {
		return s
	}
	s.QueueFullRejectedTotal = p.queueFullRejectedTotal.Load()
	s.ReadQueueFullDiscardedTotal = p.readQueueFullDiscardedTotal.Load()
	s.QueueBounded = p.queueSpec.backend == "bounded"
	if snap := p.snap.Load(); snap != nil {
		for _, worker := range snap.workers {
			w, ok := worker.(*Worker)
			if !ok {
				continue
			}
			switch q := w.queueManager.(type) {
			case *DualQueueManager:
				s.QueueLength += int64(max(0, q.systemMailbox.Len())) + int64(max(0, q.userMailbox.Len()))
			case *PriorityQueueManager:
				for _, physical := range q.queues {
					s.QueueLength += int64(max(0, physical.Len()))
				}
			}
		}
		if s.QueueBounded {
			var capacity int64
			if p.queueSpec.dual != nil {
				capacity = int64(p.queueSpec.dual.system) + int64(p.queueSpec.dual.user)
			} else {
				for _, q := range p.queueSpec.priority.priorities {
					capacity += int64(q.Capacity)
				}
			}
			s.QueueCapacity = capacity * int64(len(snap.workers))
		}
	}
	for old := p.observedQueueHighWatermark.Load(); s.QueueLength > old; old = p.observedQueueHighWatermark.Load() {
		if p.observedQueueHighWatermark.CompareAndSwap(old, s.QueueLength) {
			break
		}
	}
	s.ObservedQueueHighWatermark = p.observedQueueHighWatermark.Load()
	return s
}
