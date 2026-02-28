package etcd

import (
	"context"
	"testing"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type captureEventChannel struct {
	ch chan inf.IEvent
}

func (c *captureEventChannel) PushEvent(evt inf.IEvent) error {
	c.ch <- evt
	return nil
}

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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	watchCh := make(chan clientv3.WatchResponse, 1)
	kv := &mvccpb.KeyValue{Key: []byte("k"), Value: []byte("v")}
	watchCh <- clientv3.WatchResponse{Events: []*clientv3.Event{{Type: clientv3.EventTypePut, Kv: kv}}}

	mp := &mockProvider{connected: true, watchCh: watchCh, getResp: &clientv3.GetResponse{Kvs: []*mvccpb.KeyValue{kv}}}

	capture := &captureEventChannel{ch: make(chan inf.IEvent, 8)}

	// 不调用 Init：Init 会创建真实 etcd client。
	// 这里直接注入 provider/evtCh/context/conf，验证 watchLoop/syncInitialState 是否按约定推送事件。
	e := &EtcdDiscovery{}
	e.ctx = ctx
	e.cancel = cancel
	e.conf = &config.DiscoveryConf{Path: "/ember/service"}
	e.provider = mp
	e.evtCh = capture

	// run watch loop briefly
	go e.watchLoop()
	go e.syncInitialState()

	// 预期：syncInitialState 推 1 个 SysEventETCDPut，watchLoop 再推 1 个 SysEventETCDPut
	var got []inf.IEvent
	deadline := time.After(1 * time.Second)
	for len(got) < 2 {
		select {
		case ev := <-capture.ch:
			got = append(got, ev)
		case <-deadline:
			t.Fatalf("timeout waiting events, got=%d", len(got))
		}
	}
	cancel()

	for i, ev := range got {
		if ev.GetEventType() != event.SysEventETCDPut {
			t.Fatalf("event[%d] type mismatch: got=%v", i, ev.GetEventType())
		}
		data, ok := ev.GetData().(*mvccpb.KeyValue)
		if !ok || data == nil {
			t.Fatalf("event[%d] data type mismatch: %T", i, ev.GetData())
		}
		if string(data.Key) != "k" || string(data.Value) != "v" {
			t.Fatalf("event[%d] kv mismatch: key=%s value=%s", i, string(data.Key), string(data.Value))
		}
	}
}
