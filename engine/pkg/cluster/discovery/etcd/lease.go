package etcd

import (
	"context"
	"fmt"
	disc "github.com/njtc406/emberengine/engine/pkg/cluster/discovery"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type etcdLeaseManager struct{ d *EtcdDiscovery }

func (m *etcdLeaseManager) Grant(ttlSeconds int64) (disc.LeaseRef, error) {
	if !isEtcdClientConnected(m.d.client) {
		return nil, fmt.Errorf("etcd client is not connected")
	}
	resp, err := m.d.client.Grant(context.Background(), ttlSeconds)
	if err != nil {
		return nil, err
	}
	return resp.ID, nil
}

func (m *etcdLeaseManager) Revoke(ref disc.LeaseRef) {
	id, ok := ref.(clientv3.LeaseID)
	if !ok || id == 0 || !isEtcdClientConnected(m.d.client) {
		return
	}
	_, _ = m.d.client.Revoke(context.Background(), id)
}

func (m *etcdLeaseManager) KeepAliveLoop(ctx context.Context, ref disc.LeaseRef) error {
	id, ok := ref.(clientv3.LeaseID)
	if !ok || !isEtcdClientConnected(m.d.client) {
		return fmt.Errorf("etcd client is not connected or invalid leaseRef")
	}
	kaRespCh, err := m.d.client.KeepAlive(ctx, id)
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case kaResp, ok := <-kaRespCh:
			if !ok || kaResp == nil {
				return fmt.Errorf("keepalive channel closed")
			}
		}
	}
}
