package repository

import (
	"context"
	"testing"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/rpc/message/msgbus"
	"github.com/njtc406/emberengine/engine/pkg/utils/timelib"
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

func TestRepositoryRunningServiceNotSelectableUntilReady(t *testing.T) {
	r := NewRepository(nil)
	sender := newRepoDispatcher("n0", 1, "sender", "system", "SenderService", true)
	target := newRepoDispatcher("n1", 1, "svc1", "logic", "Gate", true)

	r.Add("", sender)
	r.AddWithMeta("", target, def.SvcStatusRunning, def.ServiceVisibilityCluster)

	if got := r.SelectByServiceUid(target.GetPid().GetServiceUid()); got == nil {
		t.Fatalf("expected running service to be available by exact uid")
	}

	bus := r.SelectByServiceType(sender.GetPid(), 0, "logic", "")
	multi, ok := bus.(msgbus.MultiBus)
	if !ok {
		t.Fatalf("expected MultiBus result, got %T", bus)
	}
	if len(multi) != 0 {
		t.Fatalf("expected running service to be skipped by normal selection, got %d", len(multi))
	}

	r.UpdateStatus(target.GetPid().GetServiceUid(), def.SvcStatusReady)
	bus = r.SelectByServiceType(sender.GetPid(), 0, "logic", "")
	multi, ok = bus.(msgbus.MultiBus)
	if !ok {
		t.Fatalf("expected MultiBus result after ready, got %T", bus)
	}
	if len(multi) != 1 {
		t.Fatalf("expected ready service to be selectable, got %d", len(multi))
	}
}

func TestRepositoryAddWithMetaUpdateKeepsExistingDispatcher(t *testing.T) {
	r := NewRepository(nil)
	oldDispatcher := newRepoDispatcher("n1", 1, "svc1", "logic", "Gate", false)
	newDispatcher := newRepoDispatcher("n1", 1, "svc1", "logic", "Gate", true)
	uid := oldDispatcher.GetPid().GetServiceUid()

	r.AddWithMeta("", oldDispatcher, def.SvcStatusRunning, def.ServiceVisibilityCluster)
	r.AddWithMeta("", newDispatcher, def.SvcStatusReady, def.ServiceVisibilityCluster)

	got := r.SelectByServiceUid(uid)
	if got != oldDispatcher {
		t.Fatalf("expected existing dispatcher to be reused")
	}
	if oldDispatcher.closed {
		t.Fatalf("existing dispatcher should not be closed by metadata update")
	}
	if !newDispatcher.closed {
		t.Fatalf("replacement dispatcher should be closed when existing dispatcher is reused")
	}
	if !oldDispatcher.GetPid().IsMasterNode() {
		t.Fatalf("expected existing dispatcher pid master flag to be synchronized")
	}
	if !r.IsSelectable(uid) {
		t.Fatalf("expected metadata update to mark service ready")
	}
}

func TestRepositoryAddTmpReusesExistingDispatcher(t *testing.T) {
	r := NewRepository(nil)
	oldDispatcher := newRepoDispatcher("n1", 1, "svc1", "logic", "Gate", true)
	newDispatcher := newRepoDispatcher("n1", 1, "svc1", "logic", "Gate", true)
	uid := oldDispatcher.GetPid().GetServiceUid()

	if got := r.AddTmp(oldDispatcher); got != oldDispatcher {
		t.Fatalf("expected first tmp dispatcher to be stored")
	}
	oldValue, ok := r.tmpMapPid.Load(uid)
	if !ok {
		t.Fatalf("expected tmp dispatcher to be cached")
	}
	oldTmp := oldValue.(*tmpInfo)
	oldTmp.latest.Store(timelib.Now().Add(-time.Minute).UnixNano())

	if got := r.AddTmp(newDispatcher); got != oldDispatcher {
		t.Fatalf("expected existing tmp dispatcher to be reused")
	}
	if oldDispatcher.closed {
		t.Fatalf("existing tmp dispatcher should not be closed")
	}
	if !newDispatcher.closed {
		t.Fatalf("replacement tmp dispatcher should be closed")
	}
	if !oldTmp.lastActive().After(timelib.Now().Add(-time.Second)) {
		t.Fatalf("expected reused tmp dispatcher to refresh last active time")
	}
}

func TestRepositorySelectTmpTouchesLastActive(t *testing.T) {
	r := NewRepository(nil)
	dispatcher := newRepoDispatcher("n1", 1, "svc1", "logic", "Gate", true)
	uid := dispatcher.GetPid().GetServiceUid()
	r.AddTmp(dispatcher)

	value, ok := r.tmpMapPid.Load(uid)
	if !ok {
		t.Fatalf("expected tmp dispatcher to be cached")
	}
	tmp := value.(*tmpInfo)
	tmp.latest.Store(timelib.Now().Add(-10 * time.Minute).UnixNano())
	if !r.defaultStrategy(tmp) {
		t.Fatalf("expected tmp dispatcher to be expired before touch")
	}

	if got := r.SelectByServiceUid(uid); got != dispatcher {
		t.Fatalf("expected tmp dispatcher to be selected")
	}
	if r.defaultStrategy(tmp) {
		t.Fatalf("expected tmp dispatcher select to refresh last active time")
	}
}

func TestRepositoryStopClosesTmpDispatchers(t *testing.T) {
	r := NewRepository(nil)
	dispatcher := newRepoDispatcher("n1", 1, "svc1", "logic", "Gate", true)
	uid := dispatcher.GetPid().GetServiceUid()
	r.AddTmp(dispatcher)

	r.Stop()

	if !dispatcher.closed {
		t.Fatalf("expected Stop to close tmp dispatcher")
	}
	if _, ok := r.tmpMapPid.Load(uid); ok {
		t.Fatalf("expected Stop to remove tmp dispatcher from cache")
	}
}

func TestRepositoryStartStopIdempotent(t *testing.T) {
	r := NewRepository(nil)
	r.Start()
	r.Start()
	r.Stop()
	r.Stop()
}
