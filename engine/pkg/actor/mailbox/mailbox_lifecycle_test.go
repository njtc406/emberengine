package mailbox

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mbjob "github.com/njtc406/emberengine/engine/pkg/actor/mailbox/job"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

// ============================================================================
// P0-2: Mailbox 生命周期闭环测试
//
// 覆盖：
// - P0-2.1: BeginStop/Wait/Stop 幂等
// - P0-2.2/P0-2.3: DrainPolicy（已在 postjob_ownership_test.go 部分覆盖）
// - P0-2.4: Suspend/Resume
// - P0-2.7: handler panic 后资源释放
// ============================================================================

// --- 辅助 ---

// countingInvoker 带执行计数、丢弃计数、panic 控制的 invoker
type countingInvoker struct {
	mockInvoker
	executeCount atomic.Int64
	discardCount atomic.Int64
	panicOnExec  atomic.Bool
	panicMsg     string
}

func (m *countingInvoker) ExecuteJob(_ context.Context, _ inf.IMailboxJob) error {
	if m.panicOnExec.Load() {
		panic(m.panicMsg)
	}
	m.executeCount.Add(1)
	return nil
}

func (m *countingInvoker) OnJobDiscarded(_ inf.IMailboxJob, _ error) {
	m.discardCount.Add(1)
}

func newJob() inf.IMailboxJob {
	j := mbjob.NewEventBusJob()
	j.SetContext(context.Background())
	j.SetPriority(def.PriorityNormal)
	return j
}

// ============================================================================
// P0-2.1: BeginStop/Wait/Stop 幂等测试
// ============================================================================

func TestMailbox_Stop_Idempotent(t *testing.T) {
	logger := &testLogger{t: t}
	invoker := &countingInvoker{mockInvoker: mockInvoker{name: "test-svc"}}

	mb, err := NewMailbox(newSimpleConf(1), logger, invoker, nil)
	if err != nil {
		t.Fatalf("NewMailbox: %v", err)
	}
	mb.Start()

	// 投递几个 Job 确保 mailbox 正常运行
	for i := 0; i < 5; i++ {
		if err := mb.PostJob(newJob()); err != nil {
			t.Fatalf("PostJob: %v", err)
		}
	}

	// 多次 Stop 不应 panic 或死锁
	mb.Stop()
	mb.Stop()
	mb.Stop()

	if invoker.executeCount.Load() < 5 {
		t.Errorf("executeCount = %d, want >= 5", invoker.executeCount.Load())
	}
}

func TestMailbox_BeginStopWait_Idempotent(t *testing.T) {
	logger := &testLogger{t: t}
	invoker := &countingInvoker{mockInvoker: mockInvoker{name: "test-svc"}}

	mb, err := NewMailbox(newSimpleConf(1), logger, invoker, nil)
	if err != nil {
		t.Fatalf("NewMailbox: %v", err)
	}
	mb.Start()

	// 多次 BeginStop 不应 panic
	mb.BeginStop()
	mb.BeginStop()
	mb.BeginStop()

	// 多次 Wait 不应 panic 或死锁
	mb.Wait()
	mb.Wait()
	mb.Wait()
}

func TestMailbox_StopWithoutStart(t *testing.T) {
	logger := &testLogger{t: t}
	invoker := &countingInvoker{mockInvoker: mockInvoker{name: "test-svc"}}

	mb, err := NewMailbox(newSimpleConf(1), logger, invoker, nil)
	if err != nil {
		t.Fatalf("NewMailbox: %v", err)
	}

	// 未 Start 直接 Stop 不应 panic 或死锁
	mb.Stop()
}

// ============================================================================
// P0-2.4: Suspend/Resume 测试
// ============================================================================

func TestMailbox_SuspendResume_Basic(t *testing.T) {
	logger := &testLogger{t: t}
	invoker := &countingInvoker{mockInvoker: mockInvoker{name: "test-svc"}}

	mb, err := NewMailbox(newSimpleConf(1), logger, invoker, nil)
	if err != nil {
		t.Fatalf("NewMailbox: %v", err)
	}
	mb.Start()

	// 正常投递
	if err := mb.PostJob(newJob()); err != nil {
		t.Fatalf("PostJob: %v", err)
	}
	waitUntil(t, func() bool { return invoker.executeCount.Load() >= 1 }, 2*time.Second, "first job")

	// 挂起
	if !mb.Suspend() {
		t.Fatal("first Suspend should return true")
	}

	// 重复挂起返回 false
	if mb.Suspend() {
		t.Fatal("second Suspend should return false (already suspended)")
	}

	// 挂起后普通消息被拒绝
	if err := mb.PostJob(newJob()); err != def.ErrMailboxSuspended {
		t.Fatalf("PostJob during suspend should return ErrMailboxSuspended, got %v", err)
	}

	// 恢复
	if !mb.Resume() {
		t.Fatal("first Resume should return true")
	}

	// 重复恢复返回 false
	if mb.Resume() {
		t.Fatal("second Resume should return false (already resumed)")
	}

	// 恢复后正常投递
	if err := mb.PostJob(newJob()); err != nil {
		t.Fatalf("PostJob after resume: %v", err)
	}
	waitUntil(t, func() bool { return invoker.executeCount.Load() >= 2 }, 2*time.Second, "post-resume job")

	mb.Stop()
}

func TestMailbox_SuspendAllowsUrgentMessages(t *testing.T) {
	logger := &testLogger{t: t}
	invoker := &countingInvoker{mockInvoker: mockInvoker{name: "test-svc"}}

	mb, err := NewMailbox(newSimpleConf(1), logger, invoker, nil)
	if err != nil {
		t.Fatalf("NewMailbox: %v", err)
	}
	mb.Start()
	mb.Suspend()

	// 紧急消息应被放行（DefaultSuspendPolicy 允许 PriorityUrgent 及以上）
	urgentJob := mbjob.NewEventBusJob()
	urgentJob.SetContext(context.Background())
	urgentJob.SetPriority(def.PriorityUrgent)

	if err := mb.PostJob(urgentJob); err != nil {
		t.Fatalf("urgent PostJob during suspend should succeed, got %v", err)
	}
	waitUntil(t, func() bool { return invoker.executeCount.Load() >= 1 }, 2*time.Second, "urgent job executed")

	mb.Resume()
	mb.Stop()
}

// ============================================================================
// P0-2.7: handler panic 后资源释放测试
// ============================================================================

func TestMailbox_HandlerPanic_ReleasesJob(t *testing.T) {
	logger := &testLogger{t: t}
	invoker := &countingInvoker{
		mockInvoker: mockInvoker{name: "test-svc"},
		panicMsg:    "test handler panic",
	}
	invoker.panicOnExec.Store(true)

	mb, err := NewMailbox(newSimpleConf(1), logger, invoker, nil)
	if err != nil {
		t.Fatalf("NewMailbox: %v", err)
	}
	mb.Start()

	// 投递一个会导致 panic 的 Job
	panicJob := mbjob.NewEventBusJob()
	panicJob.SetContext(context.Background())
	panicJob.SetPriority(def.PriorityNormal)
	if err := mb.PostJob(panicJob); err != nil {
		t.Fatalf("PostJob: %v", err)
	}

	// 等待处理
	time.Sleep(100 * time.Millisecond)

	// Job 应被释放（safeExecInternal 的 defer 保证）
	if panicJob.IsRef() {
		t.Error("job should be unref'd even after handler panic")
	}

	// panic 后 Worker 应继续运行
	invoker.panicOnExec.Store(false)
	normalJob := newJob()
	if err := mb.PostJob(normalJob); err != nil {
		t.Fatalf("PostJob after panic: %v", err)
	}
	waitUntil(t, func() bool { return invoker.executeCount.Load() >= 1 }, 2*time.Second, "post-panic job executed")

	mb.Stop()
}

func TestMailbox_HandlerPanic_WorkerContinuesProcessing(t *testing.T) {
	logger := &testLogger{t: t}
	invoker := &countingInvoker{
		mockInvoker: mockInvoker{name: "test-svc"},
		panicMsg:    "boom",
	}

	mb, err := NewMailbox(newSimpleConf(1), logger, invoker, nil)
	if err != nil {
		t.Fatalf("NewMailbox: %v", err)
	}
	mb.Start()

	// 第一个 Job panic
	invoker.panicOnExec.Store(true)
	if err := mb.PostJob(newJob()); err != nil {
		t.Fatalf("PostJob panic job: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// 恢复正常
	invoker.panicOnExec.Store(false)

	// 后续 N 个 Job 都应正常执行
	const N = 10
	for i := 0; i < N; i++ {
		if err := mb.PostJob(newJob()); err != nil {
			t.Fatalf("PostJob[%d]: %v", i, err)
		}
	}

	waitUntil(t, func() bool { return invoker.executeCount.Load() >= int64(N) }, 2*time.Second, "all post-panic jobs")
	mb.Stop()

	if invoker.executeCount.Load() < int64(N) {
		t.Errorf("executeCount = %d, want >= %d", invoker.executeCount.Load(), N)
	}
}

// ============================================================================
// Mailbox 多 Worker 并发测试
// ============================================================================

func TestMailbox_MultiWorker_AllJobsExecuted(t *testing.T) {
	logger := &testLogger{t: t}
	invoker := &countingInvoker{mockInvoker: mockInvoker{name: "test-svc"}}

	mb, err := NewMailbox(newSimpleConf(4), logger, invoker, nil)
	if err != nil {
		t.Fatalf("NewMailbox: %v", err)
	}
	mb.Start()

	const N = 1000
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			j := mbjob.NewEventBusJob()
			j.SetContext(context.Background())
			j.SetPriority(def.PriorityNormal)
			j.SetDispatcherKey(fmt.Sprintf("key-%d", idx%10))
			if err := mb.PostJob(j); err != nil {
				t.Errorf("PostJob[%d]: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()
	mb.Stop()

	if invoker.executeCount.Load() != int64(N) {
		t.Errorf("executeCount = %d, want %d", invoker.executeCount.Load(), N)
	}
}

func TestMailbox_MultiWorker_DrainDiscard_AllReleased(t *testing.T) {
	logger := &testLogger{t: t}
	invoker := &countingInvoker{mockInvoker: mockInvoker{
		name:       "test-svc",
		writeDelay: 20 * time.Millisecond,
	}}

	mb, err := NewMailbox(newSimpleConf(2), logger, invoker, nil, WithDrainPolicy(DrainDiscard))
	if err != nil {
		t.Fatalf("NewMailbox: %v", err)
	}
	mb.Start()

	const N = 50
	for i := 0; i < N; i++ {
		j := mbjob.NewEventBusJob()
		j.SetContext(context.Background())
		j.SetPriority(def.PriorityNormal)
		j.SetDispatcherKey(fmt.Sprintf("k-%d", i%5))
		if err := mb.PostJob(j); err != nil {
			t.Fatalf("PostJob[%d]: %v", i, err)
		}
	}

	time.Sleep(10 * time.Millisecond)
	mb.Stop()

	total := invoker.executeCount.Load() + invoker.discardCount.Load()
	t.Logf("executed=%d, discarded=%d, total=%d", invoker.executeCount.Load(), invoker.discardCount.Load(), total)
	if total < int64(N) {
		t.Errorf("executed(%d) + discarded(%d) = %d < %d", invoker.executeCount.Load(), invoker.discardCount.Load(), total, N)
	}
}
