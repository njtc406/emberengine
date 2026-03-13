package etcd

import (
	"context"
	"fmt"
	"path"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	disc "github.com/njtc406/emberengine/engine/pkg/cluster/discovery"
	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/protobuf/encoding/protojson"
)

type etcdServiceRegistry struct{ d *EtcdDiscovery }

func (r *etcdServiceRegistry) ServiceKey(pid *actor.PID) string {
	return path.Join(r.d.conf.Path, pid.GetServiceUid())
}

func (r *etcdServiceRegistry) MasterKey(group string) string {
	return path.Join(r.d.conf.MasterPath, group)
}

func (r *etcdServiceRegistry) RegisterService(ctx context.Context, pid *actor.PID, leaseRef disc.LeaseRef) error {
	id, ok := leaseRef.(clientv3.LeaseID)
	if !ok || !isEtcdClientConnected(r.d.client) {
		return fmt.Errorf("etcd client not connected or invalid leaseRef")
	}
	// 序列化前把运行时 MasterFlag 投影到 IsMaster bool（proto 传输字段）
	pid.PrepareForMarshal()
	pidData, err := protojson.Marshal(pid)
	if err != nil {
		return fmt.Errorf("marshal pid failed: %w", err)
	}
	_, err = r.d.client.Put(ctx, r.ServiceKey(pid), string(pidData), clientv3.WithLease(id))
	return err
}

func (r *etcdServiceRegistry) TryAcquireMaster(ctx context.Context, masterKey, group string, leaseRef disc.LeaseRef) (bool, error) {
	id, ok := leaseRef.(clientv3.LeaseID)
	if !ok || !isEtcdClientConnected(r.d.client) {
		return false, fmt.Errorf("etcd client not connected or invalid leaseRef")
	}
	txnResp, err := r.d.client.Txn(ctx).
		If(clientv3.Compare(clientv3.CreateRevision(masterKey), "=", 0)).
		Then(clientv3.OpPut(masterKey, group, clientv3.WithLease(id))).
		Commit()
	if err != nil {
		return false, err
	}
	return txnResp.Succeeded, nil
}
