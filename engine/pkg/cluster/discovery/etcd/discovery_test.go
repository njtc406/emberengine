package etcd

import (
	"context"
	"testing"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/event"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// mock provider implements IClientProvider
type mockProvider struct {
	connected bool
	watchCh   chan clientv3.WatchResponse
	getResp   *clientv3.GetResponse
}

func (m *mockProvider) IsConnected() bool { return m.connected }
func (m *mockProvider) WatchPrefix(ctx context.Context, key string) <-chan clientv3.WatchResponse {
	return m.watchCh
}
func (m *mockProvider) Watch(ctx context.Context, key string) <-chan clientv3.WatchResponse {
	return m.watchCh
}
func (m *mockProvider) GetPrefix(ctx context.Context, key string) (*clientv3.GetResponse, error) {
	return m.getResp, nil
}

func TestWatchLoopPushesEvents(t *testing.T) {
	if log.SysLogger == nil {
		log.Init(&log.LoggerConf{Stdout: true, Caller: false, Color: false, Level: "debug"}, true)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	watchCh := make(chan clientv3.WatchResponse, 1)
	kv := &mvccpb.KeyValue{Key: []byte("k"), Value: []byte("v")}
	watchCh <- clientv3.WatchResponse{Events: []*clientv3.Event{{Type: clientv3.EventTypePut, Kv: kv}}}

	mp := &mockProvider{connected: true, watchCh: watchCh, getResp: &clientv3.GetResponse{Kvs: []*mvccpb.KeyValue{kv}}}
	proc := event.NewTrigger()

	e := &EtcdDiscovery{ctx: ctx, provider: mp, proc: proc, conf: &config.DiscoveryConf{Path: "/ember/service"}}
	event.BindHandler(proc, event.SysEventServiceReg, "service_register", e.handler, e.onRegister)
	event.BindHandler(proc, event.SysEventServiceDis, "service_unregister", e.handler, e.onUnregister)

	// run watch loop briefly
	go e.watchLoop()
	go e.syncInitialState()
	// allow goroutines to process
	time.Sleep(100 * time.Millisecond)
	cancel()

	// TODO
}
