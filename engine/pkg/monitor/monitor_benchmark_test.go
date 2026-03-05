package monitor

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	mbjob "github.com/njtc406/emberengine/engine/pkg/actor/mailbox/job"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/asynclib"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
)

var benchOnce sync.Once
var benchMonitor *RpcMonitor
var benchTW *timingwheel.TimingWheel
var benchPool *asynclib.Pool

func newBenchLogger() log.ILoggerX {
	l, err := log.NewLogger(&log.LoggerConf{Stdout: false, Caller: false, Color: false, Level: "error"}, true)
	if err != nil {
		panic(err)
	}
	return l
}

func benchInitMonitor() {
	benchOnce.Do(func() {
		logger := newBenchLogger()
		benchTW = timingwheel.NewTimingWheel(time.Millisecond, 64, logger)
		benchTW.Start()

		var err error
		benchPool, err = asynclib.NewPool(128)
		if err != nil {
			panic(err)
		}

		conf := &config.RpcMonitorConf{MonitorTimerSize: 10000, MonitorBucketSize: 20}
		benchMonitor = NewRpcMonitor().Init(conf, logger, benchTW, benchPool)
		if err = benchMonitor.Start(); err != nil {
			panic(err)
		}
	})
}

type benchMailbox struct {
	handler func(ctx context.Context, j inf.IMailboxJob)
}

func (m *benchMailbox) PostJob(j inf.IMailboxJob) error {
	if j == nil {
		return nil
	}
	ctx := j.GetContext()
	defer j.Release()
	if m.handler != nil {
		m.handler(ctx, j)
	}
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
	mb.handler = func(ctx context.Context, j inf.IMailboxJob) {
		// 当前框架中：CallState.Complete()（NeedCallback=true）会 Post 一个 RpcJob。
		// 这里模拟 service.handleRpcJob 最终回收 envelope。
		rpcJob, ok := j.(*mbjob.RpcJob)
		if !ok {
			return
		}
		env := rpcJob.GetPayload()
		if env != nil {
			env.Release()
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

	rm := benchMonitor
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

	rm := benchMonitor
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

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		st := NewCallState(context.Background(), uint64(i+1), "m", time.Second, nil, nil, nil)
		st.Complete()
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
	mb.handler = func(ctx context.Context, j inf.IMailboxJob) {
		cbJob, ok := j.(*mbjob.ConcurrentCallbackJob)
		if !ok {
			return
		}
		cb := cbJob.GetPayload()
		if cb == nil {
			return
		}
		cb.DoCallback(ctx)
		if st, ok := cb.(*CallState); ok {
			st.Release()
		}
	}
	disp := &fakeDispatcher{mailbox: mb}
	callbacks := []dto.CompletionFunc{func(ctx context.Context, resp interface{}, err error, params ...interface{}) {}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		st := NewCallState(context.Background(), uint64(i+1), "m", time.Second, disp, callbacks, nil)
		st.Complete()
	}
}

// fakeDispatcher implements IRpcDispatcher enough for CallState.dispatchCallbackEvent (PostJob/IsClosed).
// Other methods are unused in these benchmarks.

type fakeDispatcher struct {
	mailbox inf.IMailboxChannel
	pid     *actor.PID
}

func (d *fakeDispatcher) PostJob(j inf.IMailboxJob) error {
	return d.mailbox.PostJob(j)
}

func (d *fakeDispatcher) Deliver(ctx context.Context, _ inf.IEnvelope) error { return nil }

func (d *fakeDispatcher) DeliverRequest(ctx context.Context, env inf.IEnvelope) error {
	return d.Deliver(ctx, env)
}

func (d *fakeDispatcher) DeliverResponse(ctx context.Context, env inf.IEnvelope) error {
	return d.Deliver(ctx, env)
}

func (d *fakeDispatcher) Close()         {}
func (d *fakeDispatcher) IsClosed() bool { return false }
func (d *fakeDispatcher) SetPid(pid *actor.PID) {
	d.pid = pid
}
func (d *fakeDispatcher) GetPid() *actor.PID { return d.pid }
