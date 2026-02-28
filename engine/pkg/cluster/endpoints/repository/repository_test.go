package repository

import (
	"context"
	"testing"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/rpc/message/msgbus"
)

type mockRepoDispatcher struct {
	pid    *actor.PID
	closed bool
}

func (m *mockRepoDispatcher) PostJob(job inf.IMailboxJob) error { return nil }

func (m *mockRepoDispatcher) DeliverRequest(ctx context.Context, envelope inf.IEnvelope) error {
	return nil
}

func (m *mockRepoDispatcher) DeliverResponse(ctx context.Context, envelope inf.IEnvelope) error {
	return nil
}

func (m *mockRepoDispatcher) SetPid(pid *actor.PID) { m.pid = pid }

func (m *mockRepoDispatcher) GetPid() *actor.PID { return m.pid }

func (m *mockRepoDispatcher) Close() { m.closed = true }

func (m *mockRepoDispatcher) IsClosed() bool { return m.closed }

func newRepoDispatcher(nodeUID string, partition int32, serviceID, serviceType, serviceName string, isMaster bool) *mockRepoDispatcher {
	pid := actor.NewPID("", nodeUID, partition, serviceID, serviceType, serviceName, 1, def.RpcTypeLocal)
	pid.SetMaster(isMaster)
	return &mockRepoDispatcher{pid: pid}
}

func TestRepositoryAddSelectRemove(t *testing.T) {
	r := NewRepository(nil)
	d := newRepoDispatcher("n1", 1, "svc1", "logic", "Gate", true)
	uid := d.GetPid().GetServiceUid()

	r.Add("", d)
	selected := r.SelectByServiceUid(uid)
	if selected == nil {
		t.Fatalf("expected dispatcher to be selectable after add")
	}

	if _, ok := r.mapSvcBySNameAndSUid[d.GetPid().GetName()]; !ok {
		t.Fatalf("expected service name index to exist after add")
	}

	r.Remove(uid)
	if got := r.SelectByServiceUid(uid); got != nil {
		t.Fatalf("expected nil after remove")
	}
	if !d.closed {
		t.Fatalf("expected dispatcher closed on remove")
	}
	if _, ok := r.mapSvcBySNameAndSUid[d.GetPid().GetName()]; ok {
		t.Fatalf("expected service name index removed after remove")
	}
}

func TestRepositorySelectByNameAndPartition(t *testing.T) {
	r := NewRepository(nil)
	sender := newRepoDispatcher("n0", 1, "sender", "logic", "SenderService", true)
	targetP1 := newRepoDispatcher("n1", 1, "svc1", "logic", "Gate", true)
	targetP2 := newRepoDispatcher("n2", 2, "svc2", "logic", "Gate", true)
	notMaster := newRepoDispatcher("n3", 1, "svc3", "logic", "Gate", false)

	r.Add("", sender)
	r.Add("", targetP1)
	r.Add("", targetP2)
	r.Add("", notMaster)

	name := "Gate"
	partition := int32(1)
	bus := r.Select(sender.GetPid(), func(p *inf.SelectParam) {
		p.ServiceName = &name
		p.Partition = &partition
	})

	multi, ok := bus.(msgbus.MultiBus)
	if !ok {
		t.Fatalf("expected MultiBus result, got %T", bus)
	}
	if len(multi) != 1 {
		t.Fatalf("expected one target for partition=1 and master=true, got %d", len(multi))
	}
}

func TestRepositorySelectByServiceType(t *testing.T) {
	r := NewRepository(nil)
	sender := newRepoDispatcher("n0", 1, "sender", "system", "SenderService", true)
	target1 := newRepoDispatcher("n1", 1, "svc1", "logic", "Gate", true)
	target2 := newRepoDispatcher("n2", 2, "svc2", "logic", "Match", true)
	otherType := newRepoDispatcher("n3", 1, "svc3", "chat", "Chat", true)

	r.Add("", sender)
	r.Add("", target1)
	r.Add("", target2)
	r.Add("", otherType)

	bus := r.SelectByServiceType(sender.GetPid(), 0, "logic", "")
	multi, ok := bus.(msgbus.MultiBus)
	if !ok {
		t.Fatalf("expected MultiBus result, got %T", bus)
	}
	if len(multi) != 2 {
		t.Fatalf("expected 2 logic services, got %d", len(multi))
	}
}
