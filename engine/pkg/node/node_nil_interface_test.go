package node

import (
	"testing"

	"github.com/njtc406/emberengine/v2/engine/pkg/cluster"
	"github.com/njtc406/emberengine/v2/engine/pkg/config"
	inf "github.com/njtc406/emberengine/v2/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/v2/engine/pkg/log"
	"github.com/njtc406/emberengine/v2/engine/pkg/utils/asynclib"
	"github.com/njtc406/emberengine/v2/engine/pkg/utils/timingwheel"
)

func TestNodeNilInterfaceGetters(t *testing.T) {
	n := New()
	var ctx inf.INodeContext = n
	getters := []struct {
		name string
		get  func() any
	}{
		{"config", func() any { return ctx.GetConfig() }},
		{"logger", func() any { return ctx.GetLogger() }},
		{"pool", func() any { return ctx.GetAntsPool() }},
		{"timing wheel", func() any { return ctx.GetTimingWheel() }},
		{"endpoints", func() any { return ctx.GetEndpointManager() }},
		{"event bus", func() any { return ctx.GetEventBus() }},
		{"router", func() any { return ctx.GetRouter() }},
		{"profiler", func() any { return ctx.GetProfilerRegistry() }},
		{"method index", func() any { return ctx.GetMethodIndex() }},
	}
	for _, tt := range getters {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.get(); got != nil {
				t.Fatalf("uninitialized component = %T, want nil interface", got)
			}
		})
	}
	n.Cluster = cluster.NewCluster()
	if got := ctx.GetEndpointManager(); got != nil {
		t.Fatalf("uninitialized cluster endpoints = %T, want nil interface", got)
	}
}

func TestNodeInterfaceGettersPreserveComponents(t *testing.T) {
	n := &Node{
		Config: &config.Config{}, Logger: &log.Logger{},
		AntsPool: &asynclib.Pool{}, TimingWheel: &timingwheel.TimingWheel{},
	}
	if n.GetConfig() != n.Config || n.GetLogger() != n.Logger ||
		n.GetAntsPool() != n.AntsPool || n.GetTimingWheel() != n.TimingWheel {
		t.Fatal("getters must return the existing non-nil components")
	}
}
