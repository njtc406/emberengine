package services

import (
	"sort"

	"github.com/njtc406/emberengine/engine/pkg/def"
)

// RuntimeSummary 提供 ServiceManager 的运行态摘要，用于诊断与可观测性。
type RuntimeSummary struct {
	ServiceCount int      `json:"service_count"`
	ServiceNames []string `json:"service_names"`
}

// GetRuntimeSummary 返回当前已初始化服务的摘要信息。
func (sm *ServiceManager) GetRuntimeSummary() RuntimeSummary {
	if sm == nil {
		return RuntimeSummary{}
	}

	names := make([]string, 0, len(sm.runServices))
	for _, svc := range sm.runServices {
		if svc == nil {
			continue
		}
		names = append(names, svc.GetName())
	}
	sort.Strings(names)

	return RuntimeSummary{
		ServiceCount: len(names),
		ServiceNames: names,
	}
}

// mailboxMetricsProvider 鸭子类型接口，由 *mailbox.Mailbox 隐式满足。
// 避免 services 包直接依赖 mailbox 包。
type mailboxMetricsProvider interface {
	GetMailboxMetrics() def.MailboxMetrics
}

// GetAggregatedMailboxMetrics 遍历所有 Service 的 Mailbox，汇总指标快照。
func (sm *ServiceManager) GetAggregatedMailboxMetrics() def.MailboxMetrics {
	if sm == nil {
		return def.MailboxMetrics{}
	}
	var agg def.MailboxMetrics
	for _, svc := range sm.runServices {
		if svc == nil {
			continue
		}
		mb := svc.GetMailbox()
		if mb == nil {
			continue
		}
		if provider, ok := mb.(mailboxMetricsProvider); ok {
			m := provider.GetMailboxMetrics()
			agg.PostTotal += m.PostTotal
			agg.SuspendedTotal += m.SuspendedTotal
			agg.RejectedTotal += m.RejectedTotal
			agg.DispatchFailedTotal += m.DispatchFailedTotal
		}
	}
	return agg
}
