package def

// MailboxMetrics 是所有 Mailbox 聚合后的指标快照。
// 字段为 int64 纯值，由 Mailbox 的原子计数器 snapshot 而来。
type MailboxMetrics struct {
	PostTotal           int64 // PostJob 总调用次数
	SuspendedTotal      int64 // 因 mailbox 挂起而拒绝的次数
	RejectedTotal       int64 // 因中间件 Reject 而拒绝的次数
	DispatchFailedTotal int64 // DispatchJob 失败次数
}
