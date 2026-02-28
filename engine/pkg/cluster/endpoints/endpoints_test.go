package endpoints

import (
	"context"
	"testing"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/cluster/endpoints/repository"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/rpc/remote"
	"go.etcd.io/etcd/api/v3/mvccpb"
	"google.golang.org/protobuf/encoding/protojson"
)

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
	if err == nil {
		t.Fatalf("expected ignore local service error")
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
