package monitor

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	mbjob "github.com/njtc406/emberengine/engine/pkg/actor/mailbox/job"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
)

type errScheduler struct{}

func (s *errScheduler) AfterFunc(_ time.Duration, _ string, _ timingwheel.TimerCallback, _ ...interface{}) (uint64, error) {
	return 0, errors.New("boom")
}
func (s *errScheduler) AfterAsyncFunc(_ time.Duration, _ string, _ func(...interface{}), _ ...interface{}) (uint64, error) {
	return 0, errors.New("boom")
}
func (s *errScheduler) TickerFunc(_ time.Duration, _ string, _ timingwheel.TimerCallback, _ ...interface{}) (uint64, error) {
	return 0, errors.New("boom")
}
func (s *errScheduler) TickerAsyncFunc(_ time.Duration, _ string, _ func(...interface{}), _ ...interface{}) (uint64, error) {
	return 0, errors.New("boom")
}
func (s *errScheduler) CronFunc(_ string, _ string, _ timingwheel.TimerCallback, _ ...interface{}) (uint64, error) {
	return 0, errors.New("boom")
}
func (s *errScheduler) CronAsyncFunc(_ string, _ string, _ func(...interface{}), _ ...interface{}) (uint64, error) {
	return 0, errors.New("boom")
}
func (s *errScheduler) CancelTimer(_ uint64) {}
func (s *errScheduler) Stop()                {}
func (s *errScheduler) GetTimerCbChannel() chan timingwheel.ITimer {
	return make(chan timingwheel.ITimer)
}

type inlineDispatcher struct {
	pid    *actor.PID
	closed atomic.Bool
	cb     func(ctx context.Context, job inf.IMailboxJob)
}

func (d *inlineDispatcher) SetPid(pid *actor.PID) {
	d.pid = pid
}
func (d *inlineDispatcher) GetPid() *actor.PID { return d.pid }
func (d *inlineDispatcher) Close()             { d.closed.Store(true) }
func (d *inlineDispatcher) IsClosed() bool     { return d.closed.Load() }

func (d *inlineDispatcher) Deliver(ctx context.Context, _ inf.IEnvelope) error { return nil }

func (d *inlineDispatcher) PostJob(j inf.IMailboxJob) error {
	if j == nil {
		return nil
	}
	ctx := j.GetContext()
	defer j.Release()

	if d.cb != nil {
		d.cb(ctx, j)
		return nil
	}

	// 默认行为：只处理并发回调 job（与 Service.handleConcurrentCallbackJob 对齐）。
	if j.GetType() != def.MailboxJobTypeConcurrentCallback {
		return nil
	}
	cbj, ok := j.(*mbjob.ConcurrentCallbackJob)
	if !ok {
		return nil
	}
	cb := cbj.GetPayload()
	if cb == nil {
		return nil
	}
	cb.DoCallback(ctx)
	// 当前框架里 callback payload（CallState）需要自行归还池。
	if st, ok := cb.(*CallState); ok {
		st.Release()
	}
	return nil
}

func newTestRpcMonitor(sd timingwheel.ITimerScheduler) *RpcMonitor {
	rm := &RpcMonitor{sd: sd}
	rm.initBuckets(defaultWaitBucketCount, 16)
	return rm
}

func TestRpcMonitorAdd_WhenSchedulerFails_CallDoesNotHang(t *testing.T) {
	rm := newTestRpcMonitor(&errScheduler{})
	st := NewCallState(context.Background(), 1, "m", time.Second, nil, nil, nil)

	done := make(chan struct{})
	go func() {
		st.Wait()
		close(done)
	}()

	rm.Add(st)

	select {
	case <-done:
		// ok
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Wait should not hang when monitor.Add fails")
	}
	if st.Error() == nil {
		t.Fatal("expected error when monitor.Add fails")
	}
	st.Release()
}

func TestRpcMonitorAdd_WhenSchedulerFails_AsyncCallbackFires(t *testing.T) {
	var called atomic.Int32
	disp := &inlineDispatcher{cb: func(ctx context.Context, j inf.IMailboxJob) {
		if cbj, ok := j.(*mbjob.ConcurrentCallbackJob); ok {
			cb := cbj.GetPayload()
			if cb != nil {
				cb.DoCallback(ctx)
				called.Add(1)
				if st, ok := cb.(*CallState); ok {
					st.Release()
				}
			}
		}
	}}

	rm := newTestRpcMonitor(&errScheduler{})
	st := NewCallState(context.Background(), 2, "m", time.Second, disp, []dto.CompletionFunc{
		func(ctx context.Context, _ interface{}, _ error, _ ...interface{}) {},
	}, nil)

	rm.Add(st)

	if called.Load() != 1 {
		t.Fatalf("expected callback to be fired once, got %d", called.Load())
	}
}
