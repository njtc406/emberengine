package etcd

import (
	"context"
	"fmt"
	disc "github.com/njtc406/emberengine/engine/pkg/cluster/discovery"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// etcdMasterElection implements IMasterElection
type etcdMasterElection struct{ d *EtcdDiscovery }

func (e *etcdMasterElection) TryAcquireMaster(ctx context.Context, masterKey, group string, leaseRef disc.LeaseRef) (bool, error) {
	id, ok := leaseRef.(clientv3.LeaseID)
	if !ok || !isEtcdClientConnected(e.d.client) {
		return false, fmt.Errorf("etcd client not connected or invalid leaseRef")
	}
	txnResp, err := e.d.client.Txn(ctx).
		If(clientv3.Compare(clientv3.CreateRevision(masterKey), "=", 0)).
		Then(clientv3.OpPut(masterKey, group, clientv3.WithLease(id))).
		Commit()
	if err != nil {
		return false, err
	}
	return txnResp.Succeeded, nil
}
