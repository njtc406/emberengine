package monitor

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
)

var benchOnce sync.Once

func benchInitMonitor() {
	benchOnce.Do(func() {
		if log.SysLogger == nil {
			log.Init(&log.LoggerConf{Stdout: false, Caller: false, Color: false, Level: "error"}, true)
		}
		if config.Conf.NodeConf == nil {
			config.Conf.NodeConf = &config.NodeConf{}
		}
		if config.Conf.NodeConf.RpcMonitorConf == nil {
			config.Conf.NodeConf.RpcMonitorConf = &config.RpcMonitorConf{MonitorTimerSize: 10000, MonitorBucketSize: 20}
		}
		timingwheel.Start(time.Millisecond, 64, log.SysLogger)

		rm := GetRpcMonitor()
		rm.Init()
		rm.Start()
	})
}

type benchMailbox struct {
	handler func(ctx context.Context, ev inf.IEvent)
}

func (m *benchMailbox) PostMessage(ctx context.Context, ev inf.IEvent) error {
	if ev == nil {
		return nil
	}
	if !ev.IsRef() {
		return nil
	}
	defer ev.Release()
	m.handler(ctx, ev)
	return nil
}

// Benchmark: CallState pool get/put + sync completion.
func BenchmarkCallState_Complete_Sync(b *testing.B) {
	benchInitMonitor()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		st := NewCallState(context.Background(), uint64(i+1), "m", time.Second, nil, nil, nil)
		st.SetResult(1, nil)
		st.Complete()
		st.Wait()
		st.Release()
	}
}

// Benchmark: Async completion path (dispatch into mailbox, execute callback, and Release via mailbox defer).
func BenchmarkCallState_Complete_Async_Dispatch(b *testing.B) {
	benchInitMonitor()
	b.ReportAllocs()

	mb := &benchMailbox{}
	mb.handler = func(ctx context.Context, ev inf.IEvent) {
		if ev.GetType() != event.ServiceConcurrentCallback {
			return
		}
		if env, ok := ev.(*event.CallbackEnvelope); ok {
			env.Payload.DoCallback(ctx)
		}
	}
	disp := &fakeDispatcher{mailbox: mb}
	callbacks := []dto.CompletionFunc{func(ctx context.Context, resp interface{}, err error, params ...interface{}) {}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		st := NewCallState(context.Background(), uint64(i+1), "m", time.Second, disp, callbacks, nil)
		st.SetResult(1, nil)
		st.Complete()
	}
}

// Benchmark: RpcMonitor Add + Remove (covers map + timer scheduling + cancel).
func BenchmarkRpcMonitor_AddRemove(b *testing.B) {
	benchInitMonitor()
	b.ReportAllocs()

	rm := GetRpcMonitor()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		seq := rm.GenSeq()
		st := NewCallState(context.Background(), seq, "m", time.Second, nil, nil, nil)
		rm.Add(st)
		got := rm.Remove(seq)
		if got != nil {
			got.Release()
		}
	}
}

// Benchmark: Cancel path (NewCancel + invoke) without waiting for real timer.
func BenchmarkRpcMonitor_Cancel(b *testing.B) {
	benchInitMonitor()
	b.ReportAllocs()

	rm := GetRpcMonitor()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		seq := rm.GenSeq()
		st := NewCallState(context.Background(), seq, "m", time.Second, nil, nil, nil)
		rm.Add(st)
		cancel := rm.NewCancel(seq)
		cancel()
	}
}

// Benchmark: Timeout handling logic (without sleeping) for sync wait.
func BenchmarkRpcMonitor_CallTimeout_Sync(b *testing.B) {
	benchInitMonitor()
	b.ReportAllocs()

	rm := GetRpcMonitor()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		st := NewCallState(context.Background(), uint64(i+1), "m", time.Second, nil, nil, nil)
		rm.callTimeout(st)
		st.Wait()
		if st.Error() != def.ErrRPCCallTimeout {
			b.Fatal("unexpected error")
		}
		st.Release()
	}
}

// Benchmark: Timeout handling logic for async callback path.
func BenchmarkRpcMonitor_CallTimeout_Async(b *testing.B) {
	benchInitMonitor()
	b.ReportAllocs()

	mb := &benchMailbox{}
	mb.handler = func(ctx context.Context, ev inf.IEvent) {
		if ev.GetType() != event.ServiceConcurrentCallback {
			return
		}
		if env, ok := ev.(*event.CallbackEnvelope); ok {
			env.Payload.DoCallback(ctx)
		}
	}
	disp := &fakeDispatcher{mailbox: mb}
	callbacks := []dto.CompletionFunc{func(ctx context.Context, resp interface{}, err error, params ...interface{}) {}}

	rm := GetRpcMonitor()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		st := NewCallState(context.Background(), uint64(i+1), "m", time.Second, disp, callbacks, nil)
		rm.callTimeout(st)
	}
}

// fakeDispatcher implements IRpcDispatcher enough for CallState.dispatchCallbackEvent (PostMessage/IsClosed).
// Other methods are unused in these benchmarks.

type fakeDispatcher struct {
	mailbox inf.IMailboxChannel
}

func (d *fakeDispatcher) PostMessage(ctx context.Context, evt inf.IEvent) error {
	return d.mailbox.PostMessage(ctx, evt)
}

func (d *fakeDispatcher) Deliver(ctx context.Context, _ inf.IEnvelope) error { return nil }

func (d *fakeDispatcher) Close()              {}
func (d *fakeDispatcher) IsClosed() bool      { return false }
func (d *fakeDispatcher) SetPid(_ *actor.PID) {}
func (d *fakeDispatcher) GetPid() *actor.PID  { return nil }
