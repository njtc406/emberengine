package etcd

import (
	"context"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// internal client provider for etcd backend
type etcdClientProvider struct{ d *EtcdDiscovery }

func (p *etcdClientProvider) IsConnected() bool { return isEtcdClientConnected(p.d.client) }

func (p *etcdClientProvider) WatchPrefix(ctx context.Context, key string) <-chan clientv3.WatchResponse {
	return p.d.client.Watch(ctx, key, clientv3.WithPrefix())
}

func (p *etcdClientProvider) Watch(ctx context.Context, key string) <-chan clientv3.WatchResponse {
	return p.d.client.Watch(ctx, key)
}

func (p *etcdClientProvider) GetPrefix(ctx context.Context, key string) (*clientv3.GetResponse, error) {
	return p.d.client.Get(ctx, key, clientv3.WithPrefix())
}
