package endpoints

import (
	"context"
	"testing"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	disc "github.com/njtc406/emberengine/engine/pkg/cluster/discovery"
	"github.com/njtc406/emberengine/engine/pkg/cluster/endpoints/repository"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/rpc/client"
	"github.com/njtc406/emberengine/engine/pkg/rpc/remote"
	"go.etcd.io/etcd/api/v3/mvccpb"
	"google.golang.org/protobuf/encoding/protojson"
)

type endpointTestService struct {
	inf.IService
	pid                    *actor.PID
	mailbox                inf.IMailbox
	visibility             def.ServiceVisibility
	status                 int32
	isPrimarySecondaryMode bool
}

func (s *endpointTestService) GetPid() *actor.PID                   { return s.pid }
func (s *endpointTestService) GetMailbox() inf.IMailbox             { return s.mailbox }
func (s *endpointTestService) GetVisibility() def.ServiceVisibility { return s.visibility }
func (s *endpointTestService) GetStatus() int32                     { return s.status }
func (s *endpointTestService) GetName() string                      { return s.pid.GetName() }
func (s *endpointTestService) IsPrimarySecondaryMode() bool         { return s.isPrimarySecondaryMode }

func newTestEndpointManager(t *testing.T) *EndpointManager {
	t.Helper()
	base, err := log.NewLogger(&log.LoggerConf{Stdout: false, Caller: false, Color: false, Level: "error"}, true)
	if err != nil {
		t.Fatalf("new logger failed: %v", err)
	}
	t.Cleanup(func() { _ = base.Close() })

	return &EndpointManager{
		ILoggerX:      log.NewLoggerX(base, log.Fields{"pkg": "endpoint_test"}),
		nodeUid:       "local-node",
		repository:    repository.NewRepository(nil),
		isClusterMode: false,
	}
}

func TestUpdateServiceInfoNilKey(t *testing.T) {
	em := newTestEndpointManager(t)
	err := em.updateServiceInfo(context.Background(), &mvccpb.KeyValue{})
	if err == nil {
		t.Fatalf("expected error when kv key is nil")
	}
}

func TestUpdateServiceInfoIgnoreLocalNode(t *testing.T) {
	em := newTestEndpointManager(t)
	pid := actor.NewPID("", em.nodeUid, 1, "svc1", "logic", "Gate", 1, def.RpcTypeLocal)
	b, err := protojson.Marshal(pid)
	if err != nil {
		t.Fatalf("marshal pid failed: %v", err)
	}
	kv := &mvccpb.KeyValue{Key: []byte("k-local"), Value: b}

	err = em.updateServiceInfo(context.Background(), kv)
	if err != nil {
		t.Fatalf("ignore local service should not be treated as error: %v", err)
	}
	if got := em.repository.SelectByServiceUid(pid.GetServiceUid()); got != nil {
		t.Fatalf("local service should not be added as remote")
	}
}

func TestUpdateAndRemoveRemoteServiceInfo(t *testing.T) {
	em := newTestEndpointManager(t)
	pid := actor.NewPID("", "remote-node", 1, "svc1", "logic", "Gate", 1, def.RpcTypeLocal)
	b, err := protojson.Marshal(pid)
	if err != nil {
		t.Fatalf("marshal pid failed: %v", err)
	}
	key := "k-remote"
	kv := &mvccpb.KeyValue{Key: []byte(key), Value: b}

	if err := em.updateServiceInfo(context.Background(), kv); err != nil {
		t.Fatalf("update remote service failed: %v", err)
	}
	if got := em.repository.SelectByServiceUid(pid.GetServiceUid()); got == nil {
		t.Fatalf("expected remote service to be added")
	}

	if err := em.removeServiceInfo(context.Background(), &mvccpb.KeyValue{Key: []byte(key)}); err != nil {
		t.Fatalf("remove remote service failed: %v", err)
	}
	if got := em.repository.SelectByServiceUid(pid.GetServiceUid()); got != nil {
		t.Fatalf("expected remote service removed")
	}
}

func TestUpdateRemoteServiceInfoWithServiceEntryStatus(t *testing.T) {
	em := newTestEndpointManager(t)
	pid := actor.NewPID("", "remote-node", 1, "svc1", "logic", "Gate", 1, def.RpcTypeLocal)
	b, err := disc.MarshalServiceEntry(pid, def.SvcStatusRunning, def.ServiceVisibilityCluster)
	if err != nil {
		t.Fatalf("marshal service entry failed: %v", err)
	}
	key := "k-remote-entry"
	if err := em.updateServiceInfo(context.Background(), &mvccpb.KeyValue{Key: []byte(key), Value: b}); err != nil {
		t.Fatalf("update remote service failed: %v", err)
	}
	if got := em.repository.SelectByServiceUid(pid.GetServiceUid()); got == nil {
		t.Fatalf("expected remote service to be added")
	}
	if em.repository.IsSelectable(pid.GetServiceUid()) {
		t.Fatalf("running service entry should not be selectable")
	}

	b, err = disc.MarshalServiceEntry(pid, def.SvcStatusReady, def.ServiceVisibilityCluster)
	if err != nil {
		t.Fatalf("marshal ready service entry failed: %v", err)
	}
	if err := em.updateServiceInfo(context.Background(), &mvccpb.KeyValue{Key: []byte(key), Value: b}); err != nil {
		t.Fatalf("update ready service failed: %v", err)
	}
	if !em.repository.IsSelectable(pid.GetServiceUid()) {
		t.Fatalf("ready service entry should be selectable")
	}
}

func TestGetDispatcherCreatesTmpWhenMissing(t *testing.T) {
	em := newTestEndpointManager(t)
	pid := actor.NewPID("", "remote-node", 1, "svc-tmp", "logic", "TmpSvc", 1, def.RpcTypeLocal)

	d := em.GetDispatcher(pid)
	if d == nil {
		t.Fatalf("expected dispatcher created for missing service")
	}
	if got := em.repository.SelectByServiceUid(pid.GetServiceUid()); got == nil {
		t.Fatalf("expected temp dispatcher stored in repository tmp map")
	}
}

func TestEndpointManagerRouteByPid_UsesTemporaryDispatcherForUnknownRemoteReceiver(t *testing.T) {
	em := newTestEndpointManager(t)
	sender := actor.NewPID("127.0.0.1:6670", em.nodeUid, 1, "sender", "system", "SenderService", 1, def.RpcTypeGrpc)
	receiver := actor.NewPID("127.0.0.1:6671", "remote-node", 1, "scene-1", "scene", "SceneService", 1, def.RpcTypeGrpc)

	em.repository.AddWithMeta("", client.NewDispatcher(nil, sender, nil), def.SvcStatusReady, def.ServiceVisibilityCluster)

	if got := em.repository.SelectByServiceUid(receiver.GetServiceUid()); got != nil {
		t.Fatalf("receiver should not be preloaded in repository, got %T", got)
	}

	bus := em.RouteByPid(sender, receiver)
	if bus == nil {
		t.Fatalf("expected non-nil bus")
	}

	if got := em.repository.SelectByServiceUid(receiver.GetServiceUid()); got == nil {
		t.Fatalf("expected receiver to be cached as temporary dispatcher")
	}
}

func TestToNodeServiceUpdatesLocalVisibility(t *testing.T) {
	em := newTestEndpointManager(t)
	pid := actor.NewPID("", em.nodeUid, 1, "svc1", "logic", "Gate", 1, def.RpcTypeLocal)
	svc := &endpointTestService{
		pid:        pid,
		visibility: def.ServiceVisibilityCluster,
		status:     def.SvcStatusReady,
	}

	em.repository.AddWithMeta("", client.NewDispatcher(nil, pid, nil), def.SvcStatusReady, def.ServiceVisibilityCluster)
	if !em.repository.IsRemoteCallable(pid.GetServiceUid()) {
		t.Fatalf("expected cluster service to be remote callable")
	}

	em.ToNodeService(svc)
	if !em.repository.IsRemoteCallable(pid.GetServiceUid()) {
		t.Fatalf("expected node service to remain remote callable by pid")
	}
}

func TestToNodeServicePrimarySecondaryKeepsElectionWatcher(t *testing.T) {
	em := newTestEndpointManager(t)
	em.isClusterMode = true
	pid := actor.NewPID("", em.nodeUid, 1, "svc1", "logic", "Gate", 1, def.RpcTypeLocal)
	svc := &endpointTestService{
		pid:                    pid,
		visibility:             def.ServiceVisibilityCluster,
		status:                 def.SvcStatusReady,
		isPrimarySecondaryMode: true,
	}
	em.repository.AddWithMeta("", client.NewDispatcher(nil, pid, nil), def.SvcStatusReady, def.ServiceVisibilityCluster)

	defer func() {
		if err := recover(); err != nil {
			t.Fatalf("ToNodeService should not trigger unregister path for primary-secondary service: %v", err)
		}
	}()
	em.ToNodeService(svc)

	if !em.repository.IsRemoteCallable(pid.GetServiceUid()) {
		t.Fatalf("expected primary-secondary node service to remain remote callable by pid")
	}
}

func TestRemoveServiceWithoutEventProcessorDoesNotPanic(t *testing.T) {
	em := newTestEndpointManager(t)
	pid := actor.NewPID("", em.nodeUid, 1, "svc1", "logic", "Gate", 1, def.RpcTypeLocal)
	svc := &endpointTestService{
		pid:        pid,
		visibility: def.ServiceVisibilityCluster,
		status:     def.SvcStatusReady,
	}
	em.repository.AddWithMeta("", client.NewDispatcher(nil, pid, nil), def.SvcStatusReady, def.ServiceVisibilityCluster)

	defer func() {
		if err := recover(); err != nil {
			t.Fatalf("RemoveService should not panic without eventProcessor: %v", err)
		}
	}()
	em.RemoveService(svc)
}

func TestGetDispatcherLocalMissingReturnsNilAndNodeReturnsDispatcher(t *testing.T) {
	em := newTestEndpointManager(t)
	pid := actor.NewPID("", em.nodeUid, 1, "svc1", "logic", "Gate", 1, def.RpcTypeLocal)
	if got := em.GetDispatcher(pid); got != nil {
		t.Fatalf("expected missing local service dispatcher to be nil")
	}

	em.repository.AddWithMeta("", client.NewDispatcher(nil, pid, nil), def.SvcStatusReady, def.ServiceVisibilityNode)
	if got := em.GetDispatcher(pid); got == nil {
		t.Fatalf("expected node local service dispatcher")
	}
}

func TestCreatePidFallbackWithoutRemote(t *testing.T) {
	em := newTestEndpointManager(t)
	em.remotes = map[string]*remote.Remote{}

	pid := em.CreatePid(3, "svcX", "logic", "Gate", 1, "grpc")
	if pid.GetAddress() != "" {
		t.Fatalf("expected empty address when rpcType remote not found, got %q", pid.GetAddress())
	}
	if pid.GetRpcType() != "" {
		t.Fatalf("expected empty rpc type fallback, got %q", pid.GetRpcType())
	}
	if pid.GetNodeUid() != em.nodeUid {
		t.Fatalf("expected node uid %q, got %q", em.nodeUid, pid.GetNodeUid())
	}
}
