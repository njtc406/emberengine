package monitor

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
	"golang.org/x/net/context"
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
	cb     func(ctx context.Context, evt inf.IEvent)
}

func (d *inlineDispatcher) SetPid(pid *actor.PID) {
	d.pid = pid
}
func (d *inlineDispatcher) GetPid() *actor.PID { return d.pid }
func (d *inlineDispatcher) Close()             { d.closed.Store(true) }
func (d *inlineDispatcher) IsClosed() bool     { return d.closed.Load() }

func (d *inlineDispatcher) Deliver(ctx context.Context, _ inf.IEnvelope) error { return nil }

func (d *inlineDispatcher) PostMessage(ctx context.Context, evt inf.IEvent) error {
	if d.cb != nil {
		d.cb(ctx, evt)
	} else {
		// 默认模拟 ServiceConcurrentCallback 的处理：执行回调
		if env, ok := evt.(*event.CallbackEnvelope); ok {
			env.Payload.DoCallback(ctx)
		}
	}
	// 模拟 Service.InvokeMessage 的 defer Release
	evt.Release()
	return nil
}

func TestRpcMonitorAdd_WhenSchedulerFails_CallDoesNotHang(t *testing.T) {
	rm := &RpcMonitor{sd: &errScheduler{}, waitMap: make(map[uint64]*CallState)}
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
	disp := &inlineDispatcher{cb: func(ctx context.Context, evt inf.IEvent) {
		// evt 是 *CallbackEnvelope，模拟 ServiceConcurrentCallback 触发回调
		if env, ok := evt.(*event.CallbackEnvelope); ok {
			env.Payload.DoCallback(ctx)
			called.Add(1)
		}
	}}

	rm := &RpcMonitor{sd: &errScheduler{}, waitMap: make(map[uint64]*CallState)}
	st := NewCallState(context.Background(), 2, "m", time.Second, disp, []dto.CompletionFunc{
		func(ctx context.Context, _ interface{}, _ error, _ ...interface{}) {},
	}, nil)

	rm.Add(st)

	if called.Load() != 1 {
		t.Fatalf("expected callback to be fired once, got %d", called.Load())
	}
}
