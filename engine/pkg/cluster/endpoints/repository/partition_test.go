package repository

import (
	"github.com/njtc406/emberengine/v2/engine/pkg/core/rpc"
	inf "github.com/njtc406/emberengine/v2/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/v2/engine/pkg/rpc/message/msgbus"
	"testing"
)

func TestPartitionCanonicalSelectionAndWildcard(t *testing.T) {
	r := NewRepository(nil)
	sender := newRepoDispatcher("sender", "1", "system", "Sender", true)
	r.Add("", sender)
	for _, p := range []string{"1", "001", "0"} {
		r.Add("", newRepoDispatcher("node", p, "logic", "Target", true))
	}
	for _, option := range []inf.SelectParamBuilder{rpc.WithPartition(1), rpc.WithPartition(int64(1)), rpc.WithPartition("1"), rpc.WithPartition("001"), rpc.WithPartition(0)} {
		bus := r.Select(sender.GetPid(), rpc.WithName("Target"), option)
		if got := len(bus.(msgbus.MultiBus)); got != 1 {
			t.Fatalf("exact selection = %d, want 1", got)
		}
		bus.Release()
	}
	bus := r.Select(sender.GetPid(), rpc.WithName("Target"))
	if got := len(bus.(msgbus.MultiBus)); got != 3 {
		t.Fatalf("nil partition wildcard = %d, want 3", got)
	}
	bus.Release()
	bus = r.SelectByServiceType(sender.GetPid(), "", "logic", "")
	if got := len(bus.(msgbus.MultiBus)); got != 3 {
		t.Fatalf("type wildcard = %d, want 3", got)
	}
	bus.Release()
}

func TestSelectByServiceTypeAnyPartition(t *testing.T) {
	r := NewRepository(nil)
	sender := newRepoDispatcher("sender", "1", "system", "Sender", true)
	r.Add("", sender)
	for _, p := range []string{"1", "true", "{2}", ""} {
		r.Add("", newRepoDispatcher("node", p, "logic", "Target", true))
	}
	for _, tt := range []struct {
		value any
		count int
	}{
		{1, 1}, {int64(1), 1}, {"1", 1}, {true, 1}, {struct{ X int }{2}, 1}, {nil, 4}, {(*int)(nil), 4}, {"", 4},
	} {
		bus := r.SelectByServiceType(sender.GetPid(), tt.value, "logic", "Target")
		if got := len(bus.(msgbus.MultiBus)); got != tt.count {
			t.Fatalf("%T matched %d, want %d", tt.value, got, tt.count)
		}
		bus.Release()
	}
	for _, tt := range []struct {
		value any
		count int
	}{{nil, 4}, {(*int)(nil), 4}, {"", 1}, {true, 1}} {
		bus := r.Select(sender.GetPid(), rpc.WithName("Target"), rpc.WithPartition(tt.value))
		if got := len(bus.(msgbus.MultiBus)); got != tt.count {
			t.Fatalf("option %T matched %d, want %d", tt.value, got, tt.count)
		}
		bus.Release()
	}
}
