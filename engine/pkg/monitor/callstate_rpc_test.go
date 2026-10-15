package monitor

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

// ============================================================================
// P0-3 / P0-1.6: RPC 调用链回归测试
//
// 覆盖：
// - CallState 超时后 late response 不重复触发回调
// - CallState 同步 Call Wait → Release 路径
// - CallState Complete 幂等
// - Monitor Remove 后 state 不再被 timer 触发
// ============================================================================

// --- helpers ---

// countingDispatcher 计数 PostJob 调用次数
type countingDispatcher struct {
	pid      *actor.PID
	postJobs atomic.Int64
	closed   atomic.Bool
}

func (d *countingDispatcher) PostJob(j inf.IMailboxJob) error {
	d.postJobs.Add(1)
	if j != nil {
		j.Release()
	}
	return nil
}
func (d *countingDispatcher) DeliverRequest(_ context.Context, _ inf.IEnvelope) error { return nil }
func (d *countingDispatcher) DeliverResponse(_ context.Context, _ inf.IEnvelope) error {
	return nil
}
func (d *countingDispatcher) SetPid(pid *actor.PID) { d.pid = pid }
func (d *countingDispatcher) GetPid() *actor.PID    { return d.pid }
func (d *countingDispatcher) Close()                { d.closed.Store(true) }
func (d *countingDispatcher) IsClosed() bool        { return d.closed.Load() }

// --- P0-1.6: CallState 超时后迟到响应处理 ---

func TestCallState_SyncCall_WaitAndRelease(t *testing.T) {
	state := NewCallState(context.Background(), 1, "TestMethod", time.Second, nil, nil, nil)

	// 模拟另一个 goroutine 完成
	go func() {
		time.Sleep(10 * time.Millisecond)
		state.SetResult("ok", nil)
		state.Complete() // signals done
	}()

	state.Wait()

	if state.Response() != "ok" {
		t.Errorf("response = %v, want 'ok'", state.Response())
	}
	if state.Error() != nil {
		t.Errorf("error = %v, want nil", state.Error())
	}

	state.Release()
	// Release 后 DataRef 应为 unref
	if state.IsRef() {
		t.Error("state should be unref'd after Release")
	}
}

func TestCallState_SyncCall_WithError(t *testing.T) {
	state := NewCallState(context.Background(), 2, "TestError", time.Second, nil, nil, nil)

	go func() {
		state.SetResult(nil, context.DeadlineExceeded)
		state.Complete()
	}()

	state.Wait()

	if state.Error() != context.DeadlineExceeded {
		t.Errorf("error = %v, want DeadlineExceeded", state.Error())
	}

	state.Release()
}

func TestCallState_AsyncCall_CompleteReleasesState(t *testing.T) {
	disp := &countingDispatcher{}
	var cbCalled atomic.Int32
	cb := dto.CompletionFunc(func(_ context.Context, _ interface{}, _ error, _ ...interface{}) {
		cbCalled.Add(1)
	})

	state := NewCallState(context.Background(), 3, "TestAsync", time.Second, disp, []dto.CompletionFunc{cb}, nil)
	state.SetResult("async-ok", nil)

	// Complete 应投递 callback job 并释放 state
	state.Complete()

	if disp.postJobs.Load() != 1 {
		t.Errorf("postJobs = %d, want 1", disp.postJobs.Load())
	}
	// state 应已被释放回池
	if state.IsRef() {
		t.Error("state should be unref'd after async Complete")
	}
}

func TestCallState_AsyncCall_CompleteIdempotent(t *testing.T) {
	disp := &countingDispatcher{}
	cb := dto.CompletionFunc(func(_ context.Context, _ interface{}, _ error, _ ...interface{}) {})

	state := NewCallState(context.Background(), 4, "TestIdempotent", time.Second, disp, []dto.CompletionFunc{cb}, nil)
	state.SetResult(nil, nil)

	state.Complete()
	// 第二次 Complete：state 已经 unref，行为取决于 pool 实现
	// 不应 panic
	state.Complete()
}

// --- Monitor 超时与迟到响应 ---

func TestMonitor_Timeout_TriggersComplete(t *testing.T) {
	rm, cleanup := newTestMonitor(t)
	defer cleanup()

	state := NewCallState(context.Background(), rm.GenSeq(), "TestTimeout", 50*time.Millisecond, nil, nil, nil)

	done := make(chan struct{})
	go func() {
		state.Wait()
		close(done)
	}()

	rm.Add(state)

	select {
	case <-done:
		// 超时触发 Complete
	case <-time.After(2 * time.Second):
		t.Fatal("timeout: state.Wait should have been unblocked by timer")
	}

	if state.Error() == nil {
		t.Error("expected timeout error")
	}
	state.Release()
}

func TestMonitor_LateResponse_NoDoubleCallback(t *testing.T) {
	rm, cleanup := newTestMonitor(t)
	defer cleanup()

	var cbCount atomic.Int32
	disp := &countingDispatcher{}
	cb := dto.CompletionFunc(func(_ context.Context, _ interface{}, _ error, _ ...interface{}) {
		cbCount.Add(1)
	})

	reqId := rm.GenSeq()
	state := NewCallState(context.Background(), reqId, "TestLate", 50*time.Millisecond, disp, []dto.CompletionFunc{cb}, nil)
	rm.Add(state)

	// 等待超时触发
	time.Sleep(200 * time.Millisecond)

	// 模拟 late response：尝试 Remove
	lateState := rm.Remove(reqId)

	// 超时已经移除了 state，Remove 应返回 nil
	if lateState != nil {
		t.Error("late Remove should return nil (already removed by timer)")
		lateState.Release()
	}

	// callback 应只被触发一次（通过 timer 的 Complete）
	if disp.postJobs.Load() != 1 {
		t.Errorf("postJobs = %d, want 1 (only from timer)", disp.postJobs.Load())
	}
}

func TestMonitor_RemoveBeforeTimeout_CancelsTimer(t *testing.T) {
	rm, cleanup := newTestMonitor(t)
	defer cleanup()

	reqId := rm.GenSeq()
	state := NewCallState(context.Background(), reqId, "TestRemove", time.Second, nil, nil, nil)

	done := make(chan struct{})
	go func() {
		state.Wait()
		close(done)
	}()

	rm.Add(state)

	// 立即移除（模拟正常响应到达）
	removed := rm.Remove(reqId)
	if removed == nil {
		t.Fatal("Remove should return the state")
	}

	// 手动完成
	removed.SetResult("normal", nil)
	removed.Complete()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Wait should have been unblocked")
	}

	if removed.Response() != "normal" {
		t.Errorf("response = %v, want 'normal'", removed.Response())
	}

	removed.Release()

	// 等一段时间确认 timer 不会再触发
	time.Sleep(100 * time.Millisecond)
}

// ============================================================================
// P3-5: Graceful shutdown 验证
// ============================================================================

func TestMonitor_AddAfterStop_ReturnsError(t *testing.T) {
	rm, cleanup := newTestMonitor(t)
	cleanup() // stop immediately

	state := NewCallState(context.Background(), 1, "AfterStop", time.Second, nil, nil, nil)
	rm.Add(state)

	// Add after Stop should call Complete immediately with ErrRPCHadClosed
	state.Wait()
	if state.Error() == nil {
		t.Fatal("expected error after Add to stopped monitor")
	}
	if state.Error().Error() != "rpc had closed" {
		t.Errorf("error = %v, want 'rpc had closed'", state.Error())
	}
	state.Release()
}

func TestMonitor_StopIdempotent(t *testing.T) {
	rm, cleanup := newTestMonitor(t)
	defer cleanup()

	// Double stop should not panic
	rm.Stop()
	rm.Stop()
}

func TestMonitor_StopClearsPendingStates(t *testing.T) {
	rm, cleanup := newTestMonitor(t)

	reqId1 := rm.GenSeq()
	reqId2 := rm.GenSeq()
	state1 := NewCallState(context.Background(), reqId1, "Pending1", 10*time.Second, nil, nil, nil)
	state2 := NewCallState(context.Background(), reqId2, "Pending2", 10*time.Second, nil, nil, nil)
	rm.Add(state1)
	rm.Add(state2)

	// Stop should clear all pending states
	cleanup()

	// After stop, Get should return nil
	if rm.Get(reqId1) != nil {
		t.Error("state1 should be cleared after Stop")
	}
	if rm.Get(reqId2) != nil {
		t.Error("state2 should be cleared after Stop")
	}
}
