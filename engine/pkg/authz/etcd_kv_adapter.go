package authz

import (
	"context"
	"fmt"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// EtcdKVAdapter 将 clientv3.Client 适配为 authz.KVClient 接口。
type EtcdKVAdapter struct {
	client    *clientv3.Client
	ownClient bool // 是否拥有 client 生命周期
}

// NewEtcdKVAdapter 创建 etcd KV 适配器。
// ownClient=true 时 Close() 会关闭底层 client；否则 Close() 为无操作。
func NewEtcdKVAdapter(client *clientv3.Client, ownClient bool) *EtcdKVAdapter {
	return &EtcdKVAdapter{client: client, ownClient: ownClient}
}

func (a *EtcdKVAdapter) Get(ctx context.Context, key string) ([]byte, int64, error) {
	resp, err := a.client.Get(ctx, key)
	if err != nil {
		return nil, 0, err
	}
	if len(resp.Kvs) == 0 {
		return nil, resp.Header.Revision, nil
	}
	return resp.Kvs[0].Value, resp.Kvs[0].ModRevision, nil
}

func (a *EtcdKVAdapter) Watch(ctx context.Context, prefix string) (<-chan WatchEvent, error) {
	wch := a.client.Watch(ctx, prefix, clientv3.WithPrefix())
	out := make(chan WatchEvent, 16)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case resp, ok := <-wch:
				if !ok {
					return
				}
				if resp.Err() != nil {
					return
				}
				for _, ev := range resp.Events {
					var wt WatchEventType
					switch ev.Type {
					case clientv3.EventTypePut:
						wt = WatchEventPut
					case clientv3.EventTypeDelete:
						wt = WatchEventDelete
					default:
						continue
					}
					select {
					case out <- WatchEvent{Type: wt, Key: string(ev.Kv.Key), Value: ev.Kv.Value}:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()
	return out, nil
}

func (a *EtcdKVAdapter) Close() error {
	if !a.ownClient || a.client == nil {
		return nil
	}
	return a.client.Close()
}

// NewEtcdClientForAuthz 根据 etcd 配置创建独立的 etcd 客户端（用于 authz 策略加载）。
func NewEtcdClientForAuthz(endpoints []string, dialTimeout time.Duration, username, password string) (*clientv3.Client, error) {
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("authz: etcd endpoints is empty")
	}
	if dialTimeout <= 0 {
		dialTimeout = 3 * time.Second
	}
	cfg := clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: dialTimeout,
		Username:    username,
		Password:    password,
	}
	return clientv3.New(cfg)
}
