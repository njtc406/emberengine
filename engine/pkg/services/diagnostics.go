package services

import "sort"

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
