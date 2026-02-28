package profiler

import "sync"

type Adapter struct {
	profiler *Profiler
	mu       sync.Mutex
	stack    []*Analyzer
}

func NewAdapter(p *Profiler) *Adapter {
	if p == nil {
		return nil
	}
	return &Adapter{profiler: p}
}

func (a *Adapter) Push(tag string) {
	if a == nil || a.profiler == nil {
		return
	}
	analyzer := a.profiler.Push(tag)
	if analyzer == nil {
		return
	}
	a.mu.Lock()
	a.stack = append(a.stack, analyzer)
	a.mu.Unlock()
}

func (a *Adapter) Pop() {
	if a == nil || a.profiler == nil {
		return
	}
	a.mu.Lock()
	n := len(a.stack)
	if n == 0 {
		a.mu.Unlock()
		return
	}
	analyzer := a.stack[n-1]
	a.stack = a.stack[:n-1]
	a.mu.Unlock()
	if analyzer != nil {
		analyzer.Pop()
	}
}

func (a *Adapter) Reset() {
	for {
		a.mu.Lock()
		n := len(a.stack)
		if n == 0 {
			a.mu.Unlock()
			return
		}
		analyzer := a.stack[n-1]
		a.stack = a.stack[:n-1]
		a.mu.Unlock()
		if analyzer != nil {
			analyzer.Pop()
		}
	}
}

func (a *Adapter) IsEnabled() bool {
	return a != nil && a.profiler != nil
}
