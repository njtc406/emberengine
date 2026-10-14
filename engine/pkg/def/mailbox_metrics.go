package def

// MailboxMetrics 是所有 Mailbox 聚合后的指标快照。
// 快照包含累计计数、近似主队列占用和后端信息；按存活服务汇总的计数可随服务移除下降。
type MailboxMetrics struct {
	QueueFullRejectedTotal      int64
	ReadQueueFullDiscardedTotal int64
	QueueLength                 int64 // Nonnegative approximate main-queue length; excludes readCh and executing jobs.
	QueueCapacity               int64 // Zero unless QueueBounded.
	QueueBounded                bool
	QueueBackend                string // Aggregation: bounded, unbounded, mixed; empty when no mailbox is observable.
	ObservedQueueHighWatermark  int64  // Scrape-observed maximum, not an exact lifetime peak.
	PostTotal                   int64  // PostJob 总调用次数
	SuspendedTotal              int64  // 因 mailbox 挂起而拒绝的次数
	RejectedTotal               int64  // 因中间件 Reject 而拒绝的次数
	DispatchFailedTotal         int64  // DispatchJob 失败次数
}
