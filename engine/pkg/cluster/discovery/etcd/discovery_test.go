package etcd

import (
	"context"
	"testing"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	mvccpb "go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/protobuf/proto"
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

// mock processor captures pushed events
type mockProcessor struct{ got []inf.IEvent }

func (p *mockProcessor) Init(_ inf.IListener)                                                   {}
func (p *mockProcessor) EventHandler(_ inf.IEvent)                                              {}
func (p *mockProcessor) RegEventReceiverFunc(int32, inf.IEventHandler, inf.EventCallBack)       {}
func (p *mockProcessor) UnRegEventReceiverFun(int32, inf.IEventHandler)                         {}
func (p *mockProcessor) RegGlobalEventReceiverFunc(int32, inf.IEventHandler, inf.EventCallBack) {}
func (p *mockProcessor) UnRegGlobalEventReceiverFun(int32, inf.IEventHandler)                   {}
func (p *mockProcessor) PublishGlobal(context.Context, int32, proto.Message) error              { return nil }
func (p *mockProcessor) RegServerEventReceiverFunc(int32, inf.IEventHandler, inf.EventCallBack) {}
func (p *mockProcessor) UnRegServerEventReceiverFun(int32, inf.IEventHandler)                   {}
func (p *mockProcessor) PublishServer(context.Context, int32, proto.Message) error              { return nil }
func (p *mockProcessor) RegSpecificEventReceiverFunc(int32, string, inf.IEventHandler, inf.EventCallBack) {
}
func (p *mockProcessor) UnRegSpecificEventReceiverFun(int32, string, inf.IEventHandler) {}
func (p *mockProcessor) PublishSpecific(context.Context, int32, string, proto.Message) error {
	return nil
}
func (p *mockProcessor) CastEvent(inf.IEvent)                                     {}
func (p *mockProcessor) AddBindEvent(int32, inf.IEventHandler, inf.EventCallBack) {}
func (p *mockProcessor) AddListen(int32, inf.IEventHandler)                       {}
func (p *mockProcessor) RemoveBindEvent(int32, inf.IEventHandler)                 {}
func (p *mockProcessor) RemoveListen(int32, inf.IEventHandler)                    {}
func (p *mockProcessor) PushEvent(ev inf.IEvent) error                            { p.got = append(p.got, ev); return nil }

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
	proc := &mockProcessor{}

	e := &EtcdDiscovery{ctx: ctx, provider: mp, proc: proc, conf: &config.DiscoveryConf{Path: "/ember/service"}}

	// run watch loop briefly
	go e.watchLoop()
	go e.syncInitialState()
	// allow goroutines to process
	time.Sleep(100 * time.Millisecond)
	cancel()
	// check that events were pushed
	if len(proc.got) == 0 {
		t.Fatalf("expected events pushed, got 0")
	}
	// types should be either put or del
	for _, ev := range proc.got {
		if ev.GetType() != event.SysEventETCDPut && ev.GetType() != event.SysEventETCDDel {
			t.Fatalf("unexpected event type: %d", ev.GetType())
		}
	}
}
