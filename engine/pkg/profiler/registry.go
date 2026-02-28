package profiler

import (
	"container/list"
	"sync"

	"github.com/njtc406/emberengine/engine/pkg/log"
)

type Registry struct {
	lock      sync.RWMutex
	profilers map[string]*Profiler
	report    ReportFunType
}

func NewRegistry() *Registry {
	return &Registry{
		profilers: make(map[string]*Profiler),
		report:    DefaultReportFunction,
	}
}

func (r *Registry) RegProfiler(profilerName string, logger log.ILoggerX) *Profiler {
	r.lock.Lock()
	defer r.lock.Unlock()
	if _, ok := r.profilers[profilerName]; ok {
		return nil
	}
	p := NewProfiler(logger)
	r.profilers[profilerName] = p
	return p
}

func (r *Registry) UnRegProfiler(profilerName string) {
	r.lock.Lock()
	defer r.lock.Unlock()
	delete(r.profilers, profilerName)
}

func (r *Registry) SetReportFunction(reportFun ReportFunType) {
	if reportFun == nil {
		return
	}
	r.lock.Lock()
	r.report = reportFun
	r.lock.Unlock()
}

func (r *Registry) Report() {
	r.lock.RLock()
	report := r.report
	profiles := make(map[string]*Profiler, len(r.profilers))
	for name, prof := range r.profilers {
		profiles[name] = prof
	}
	r.lock.RUnlock()

	for name, prof := range profiles {
		prof.stackLocker.RLock()
		pElem := prof.stack.Back()
		for pElem != nil {
			pElement := pElem.Value.(*Element)
			pExceptionElem, _ := prof.check(pElement)
			if pExceptionElem != nil {
				prof.pushRecordLog(pExceptionElem)
			}
			pElem = pElem.Prev()
		}

		if prof.record.Len() == 0 {
			prof.stackLocker.RUnlock()
			continue
		}

		record := prof.record
		prof.record = list.New()
		callNum := prof.callNum
		totalCostTime := prof.totalCostTime
		prof.stackLocker.RUnlock()

		report(name, callNum, totalCostTime, record)
	}
}
