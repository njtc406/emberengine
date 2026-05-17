// Package core
// @Title  Service Profiler 桥接
// @Description  封装 Profiler 的注册/注销逻辑，消除重复的 registry 适配器构造。
// @Author  yr  2026/5/17
package core

import (
	"github.com/njtc406/emberengine/engine/pkg/profiler"

	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
)

// profilerBridge 封装 Profiler 的注册/注销逻辑。
// 优先使用直属 profilerRegistry，fallback 到 nodeCtx 的 registry。
type profilerBridge struct {
	profilerRegistry *profiler.Registry
	nodeCtx          inf.INodeContext
}

func (b *profilerBridge) resolveRegistry() inf.INodeProfilerRegistry {
	if b.profilerRegistry != nil {
		return &profilerRegistryAdapter{registry: b.profilerRegistry}
	}
	if b.nodeCtx != nil {
		return b.nodeCtx.GetProfilerRegistry()
	}
	return nil
}

// profilerRegistryAdapter 将 *profiler.Registry 适配为 inf.INodeProfilerRegistry。
type profilerRegistryAdapter struct {
	registry *profiler.Registry
}

func (a *profilerRegistryAdapter) RegProfiler(name string, logger log.ILoggerX) inf.IProfiler {
	if a == nil || a.registry == nil {
		return nil
	}
	p := a.registry.RegProfiler(name, logger)
	if p == nil {
		return nil
	}
	return profiler.NewAdapter(p)
}

func (a *profilerRegistryAdapter) UnRegProfiler(name string) {
	if a == nil || a.registry == nil {
		return
	}
	a.registry.UnRegProfiler(name)
}

func (s *Service) OpenProfiler() {
	bridge := profilerBridge{
		profilerRegistry: s.deps.profilerRegistry,
		nodeCtx:          s.deps.nodeCtx,
	}
	reg := bridge.resolveRegistry()
	if reg == nil {
		s.Error("profiler registry is nil")
		return
	}
	s.profiler = reg.RegProfiler(s.pid.GetServiceUid(), s.ILoggerX)
	if s.profiler == nil {
		s.Error("profiler reg fail")
		return
	}
}

func (s *Service) GetProfiler() inf.IProfiler {
	return s.profiler
}

func (s *Service) closeProfiler() {
	if s.profiler != nil {
		bridge := profilerBridge{
			profilerRegistry: s.deps.profilerRegistry,
			nodeCtx:          s.deps.nodeCtx,
		}
		if reg := bridge.resolveRegistry(); reg != nil {
			reg.UnRegProfiler(s.pid.GetServiceUid())
		}
		s.profiler = nil
	}
}
