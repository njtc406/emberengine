package etcd

import (
	"context"
	"fmt"
	"path"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	disc "github.com/njtc406/emberengine/engine/pkg/cluster/discovery"
	"github.com/njtc406/emberengine/engine/pkg/def"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type etcdServiceRegistry struct{ d *EtcdDiscovery }

func (r *etcdServiceRegistry) ServiceKey(pid *actor.PID) string {
	return path.Join(r.d.conf.Path, pid.GetServiceUid())
}

func (r *etcdServiceRegistry) MasterKey(group string) string {
	return path.Join(r.d.conf.MasterPath, group)
}

func (r *etcdServiceRegistry) RegisterService(ctx context.Context, pid *actor.PID, status int32, visibility def.ServiceVisibility, leaseRef disc.LeaseRef) error {
	id, ok := leaseRef.(clientv3.LeaseID)
	if !ok || !isEtcdClientConnected(r.d.client) {
		return fmt.Errorf("etcd client not connected or invalid leaseRef")
	}
	// 走 MarshalServiceEntry 唯一出口：内部通过 MarshalPIDJSON Clone + PrepareForMarshal，
	// 与并发 RPC 序列化、运行时 SetMaster 完全不竞争 IsMaster 字段。
	entryData, err := disc.MarshalServiceEntry(pid, status, visibility)
	if err != nil {
		return err
	}
	_, err = r.d.client.Put(ctx, r.ServiceKey(pid), string(entryData), clientv3.WithLease(id))
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
