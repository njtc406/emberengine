package mailbox

import (
	"context"
	"fmt"

	"github.com/njtc406/emberengine/engine/pkg/utils/boundedmpsc"
	"sync/atomic"
	"testing"
	"time"

	mbjob "github.com/njtc406/emberengine/engine/pkg/actor/mailbox/job"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

// ============================================================================
// P0-1.3: PostJob 成功、拒绝、discard 三条路径释放测试
//
// 验证 PostJob 的所有权转移契约：
// - 成功路径：Worker 执行后 Release
// - Suspended 路径：Mailbox.discardJob → Release
// - Middleware Reject 路径：ExecuteOnComplete → Release
// - Dispatch 失败路径：ExecuteOnComplete → Release
// ============================================================================

// --- 测试辅助 ---

// releaseTrackingInvoker 追踪 OnJobDiscarded 调用次数
type releaseTrackingInvoker struct {
	mockInvoker
	discardCount atomic.Int64
	executeCount atomic.Int64
}

func (m *releaseTrackingInvoker) ExecuteJob(_ context.Context, job inf.IMailboxJob) error {
	m.executeCount.Add(1)
	return nil
}

func (m *releaseTrackingInvoker) OnJobDiscarded(_ inf.IMailboxJob, _ error) {
	m.discardCount.Add(1)
}

func newSimpleConf(workerNum int32) *config.MailboxConf {
	return &config.MailboxConf{
		QueueMode:   "dual",
		StopTimeout: 5 * time.Second,
		SchedulePolicy: &config.WorkerSchedulePolicy{
			InitialWorkerNum:  workerNum,
			VirtualWorkerRate: 24,
		},
	}
}

func newWorkerForSafeExecTest(t *testing.T, invoker inf.IMessageInvoker) *Worker {
	t.Helper()
	return &Worker{
		workerId: 1,
		env: &WorkerEnv{
			logger:          &testLogger{t: t},
			invoker:         invoker,
			middlewareChain: NewMiddlewareChain(nil),
			rw:              &RWController{},
		},
	}
}

// waitUntil 等待条件满足或超时
func waitUntil(t *testing.T, cond func() bool, timeout time.Duration, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for: %s", msg)
		}
		time.Sleep(time.Millisecond)
	}
}

// --- 测试用例 ---

func TestPostJob_Success_WorkerReleasesJob(t *testing.T) {
	logger := &testLogger{t: t}
	invoker := &releaseTrackingInvoker{mockInvoker: mockInvoker{name: "test-svc"}}

	mb, err := NewMailbox(newSimpleConf(1), logger, invoker, nil)
	if err != nil {
		t.Fatalf("NewMailbox: %v", err)
	}
	mb.Start()

	job := mbjob.NewEventBusJob()
	job.SetContext(context.Background())
	job.SetPriority(def.PriorityNormal)

	if err := mb.PostJob(job); err != nil {
		t.Fatalf("PostJob: %v", err)
	}

	// 等待 Worker 执行
	waitUntil(t, func() bool { return invoker.executeCount.Load() >= 1 }, 2*time.Second, "job executed")

	mb.Stop()

	// Job 应已被 Worker Release（DataRef unref）
	if job.IsRef() {
		t.Error("job should be unref'd after Worker execution")
	}
	// 不应触发 discard
	if invoker.discardCount.Load() != 0 {
		t.Errorf("discardCount = %d, want 0", invoker.discardCount.Load())
	}
}

func TestWorkerSafeExec_CancelableJobCanceledBeforeExecute_Discards(t *testing.T) {
	invoker := &releaseTrackingInvoker{mockInvoker: mockInvoker{name: "test-svc"}}
	w := newWorkerForSafeExecTest(t, invoker)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	job := mbjob.NewRpcJob()
	job.SetContext(ctx)
	job.SetPriority(def.PriorityNormal)

	w.safeExec(job)

	if invoker.executeCount.Load() != 0 {
		t.Errorf("executeCount = %d, want 0", invoker.executeCount.Load())
	}
	if invoker.discardCount.Load() != 1 {
		t.Errorf("discardCount = %d, want 1", invoker.discardCount.Load())
	}
	if job.IsRef() {
		t.Error("job should be unref'd after canceled discard")
	}
}

func TestWorkerSafeExec_NonCancelableJobCanceledContext_StillExecutes(t *testing.T) {
	invoker := &releaseTrackingInvoker{mockInvoker: mockInvoker{name: "test-svc"}}
	w := newWorkerForSafeExecTest(t, invoker)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	job := mbjob.NewEventBusJob()
	job.SetContext(ctx)
	job.SetPriority(def.PriorityNormal)

	w.safeExec(job)

	if invoker.executeCount.Load() != 1 {
		t.Errorf("executeCount = %d, want 1", invoker.executeCount.Load())
	}
	if invoker.discardCount.Load() != 0 {
		t.Errorf("discardCount = %d, want 0", invoker.discardCount.Load())
	}
	if job.IsRef() {
		t.Error("job should be unref'd after execution")
	}
}

func TestPostJob_Suspended_DiscardsAndReleasesJob(t *testing.T) {
	logger := &testLogger{t: t}
	invoker := &releaseTrackingInvoker{mockInvoker: mockInvoker{name: "test-svc"}}

	mb, err := NewMailbox(newSimpleConf(1), logger, invoker, nil)
	if err != nil {
		t.Fatalf("NewMailbox: %v", err)
	}
	mb.Start()

	// 挂起 Mailbox
	if !mb.Suspend() {
		t.Fatal("Suspend should return true")
	}

	job := mbjob.NewEventBusJob()
	job.SetContext(context.Background())
	job.SetPriority(def.PriorityNormal)

	err = mb.PostJob(job)
	if err != def.ErrMailboxSuspended {
		t.Fatalf("PostJob should return ErrMailboxSuspended, got %v", err)
	}

	// Job 应已被释放
	if job.IsRef() {
		t.Error("job should be unref'd after suspend discard")
	}
	// OnJobDiscarded 应被调用
	if invoker.discardCount.Load() != 1 {
		t.Errorf("discardCount = %d, want 1", invoker.discardCount.Load())
	}
	// 不应执行
	if invoker.executeCount.Load() != 0 {
		t.Errorf("executeCount = %d, want 0", invoker.executeCount.Load())
	}

	mb.Resume()
	mb.Stop()
}

func TestPostJob_AfterStop_ReturnsError(t *testing.T) {
	logger := &testLogger{t: t}
	invoker := &releaseTrackingInvoker{mockInvoker: mockInvoker{name: "test-svc"}}

	mb, err := NewMailbox(newSimpleConf(1), logger, invoker, nil)
	if err != nil {
		t.Fatalf("NewMailbox: %v", err)
	}
	mb.Start()
	mb.Stop()

	job := mbjob.NewEventBusJob()
	job.SetContext(context.Background())
	job.SetPriority(def.PriorityNormal)

	// 停止后投递应失败
	err = mb.PostJob(job)
	if err == nil {
		t.Fatal("PostJob after Stop should return error")
	}

	// Job 应已被释放（PostJob 无论成功/失败都接管所有权）
	if job.IsRef() {
		t.Error("job should be unref'd after PostJob failure")
	}
}

func TestPostJob_DrainDiscard_ReleasesResidualJobs(t *testing.T) {
	logger := &testLogger{t: t}
	// 使用慢速 invoker，让 Stop 时队列中有残留
	invoker := &releaseTrackingInvoker{mockInvoker: mockInvoker{
		name:       "test-svc",
		writeDelay: 50 * time.Millisecond,
	}}

	mb, err := NewMailbox(newSimpleConf(1), logger, invoker, nil, WithDrainPolicy(DrainDiscard))
	if err != nil {
		t.Fatalf("NewMailbox: %v", err)
	}
	mb.Start()

	// 快速投递多个 Job，让队列积压
	const N = 20
	jobs := make([]*mbjob.EventBusJob, N)
	for i := 0; i < N; i++ {
		jobs[i] = mbjob.NewEventBusJob()
		jobs[i].SetContext(context.Background())
		jobs[i].SetPriority(def.PriorityNormal)
		if err := mb.PostJob(jobs[i]); err != nil {
			t.Fatalf("PostJob[%d]: %v", i, err)
		}
	}

	// 给一些时间让第一个 Job 开始执行
	time.Sleep(10 * time.Millisecond)

	// 停止（DrainDiscard 策略）
	mb.Stop()

	// 所有 Job 都应已被释放（执行的和丢弃的）
	for i, j := range jobs {
		if j.IsRef() {
			t.Errorf("job[%d] should be unref'd after DrainDiscard", i)
		}
	}

	executed := invoker.executeCount.Load()
	discarded := invoker.discardCount.Load()
	t.Logf("executed=%d, discarded=%d, total=%d", executed, discarded, N)

	// 执行数 + 丢弃数 应该 >= N（理论上 == N）
	if executed+discarded < int64(N) {
		t.Errorf("executed(%d) + discarded(%d) < %d", executed, discarded, N)
	}
}

func TestPostJob_DrainExecute_ExecutesResidualJobs(t *testing.T) {
	logger := &testLogger{t: t}
	invoker := &releaseTrackingInvoker{mockInvoker: mockInvoker{name: "test-svc"}}

	mb, err := NewMailbox(newSimpleConf(1), logger, invoker, nil, WithDrainPolicy(DrainExecute))
	if err != nil {
		t.Fatalf("NewMailbox: %v", err)
	}
	mb.Start()

	const N = 10
	for i := 0; i < N; i++ {
		job := mbjob.NewEventBusJob()
		job.SetContext(context.Background())
		job.SetPriority(def.PriorityNormal)
		if err := mb.PostJob(job); err != nil {
			t.Fatalf("PostJob[%d]: %v", i, err)
		}
	}

	mb.Stop()

	// DrainExecute 策略下所有 Job 都应被执行
	if invoker.executeCount.Load() != int64(N) {
		t.Errorf("executeCount = %d, want %d", invoker.executeCount.Load(), N)
	}
	if invoker.discardCount.Load() != 0 {
		t.Errorf("discardCount = %d, want 0", invoker.discardCount.Load())
	}
}

// rejectAllMiddleware 拒绝所有消息的中间件
type rejectAllMiddleware struct{}

func (m *rejectAllMiddleware) Name() string { return "reject-all" }
func (m *rejectAllMiddleware) OnReceive(ctx inf.IMiddlewareContext) dto.MiddlewareResult {
	return dto.Reject(def.ErrMailboxMiddlewareRejected)
}
func (m *rejectAllMiddleware) OnComplete(ctx inf.IMiddlewareContext, err error, result interface{}) {}
func (m *rejectAllMiddleware) OnStart()                                                             {}
func (m *rejectAllMiddleware) OnStop()                                                              {}

func TestPostJob_MiddlewareReject_DiscardsAndReleasesJob(t *testing.T) {
	logger := &testLogger{t: t}
	invoker := &releaseTrackingInvoker{mockInvoker: mockInvoker{name: "test-svc"}}

	mb, err := NewMailbox(newSimpleConf(1), logger, invoker,
		[]inf.IMailboxMiddleware{&rejectAllMiddleware{}})
	if err != nil {
		t.Fatalf("NewMailbox: %v", err)
	}
	mb.Start()

	job := mbjob.NewEventBusJob()
	job.SetContext(context.Background())
	job.SetPriority(def.PriorityNormal)

	err = mb.PostJob(job)
	if err != def.ErrMailboxMiddlewareRejected {
		t.Fatalf("PostJob should return ErrMailboxMiddlewareRejected, got %v", err)
	}

	// Job 应已被释放
	if job.IsRef() {
		t.Error("job should be unref'd after middleware reject")
	}
	// OnJobDiscarded 应被调用
	if invoker.discardCount.Load() != 1 {
		t.Errorf("discardCount = %d, want 1", invoker.discardCount.Load())
	}

	mb.Stop()
}

// Keep the wrapper alive after the pooled underlying Job is released.
type admissionOwnershipJob struct {
	inf.IMailboxJob
	releases atomic.Int64
}

func (j *admissionOwnershipJob) Release() {
	if j.releases.Add(1) == 1 {
		j.IMailboxJob.Release()
	}
}

func TestWorkerAdmissionOutsideGateOwnership(t *testing.T) {
	inv := &releaseTrackingInvoker{}
	env := newWorkerForSafeExecTest(t, inv).env
	w := newWorker(1, newSimpleConf(1), env, DrainExecute).(*Worker)
	w.Start()
	j := &admissionOwnershipJob{IMailboxJob: newJob()}
	paused, resume := make(chan struct{}), make(chan struct{})
	result := make(chan error, 1)
	go func() { close(paused); <-resume; result <- w.SubmitJob(j) }()
	awaitAdmission(t, paused)
	w.Stop()
	close(resume)
	if err := <-result; err != def.ErrMailboxWorkerClosed {
		t.Fatalf("got %v, want closed", err)
	}
	if j.releases.Load() != 0 {
		t.Fatal("rejected Job ownership was consumed")
	}
	j.Release()
	if j.releases.Load() != 1 || inv.executeCount.Load() != 0 || inv.discardCount.Load() != 0 {
		t.Fatal("rejected Job must terminate only in caller")
	}
}

// This test adapter exercises Worker with the real bounded queue without
// changing production queue-manager configuration or Push(false) semantics.
type admissionBoundedManager struct {
	q *boundedmpsc.Queue[inf.IMailboxJob]
}

func (m *admissionBoundedManager) Submit(j inf.IMailboxJob) error {
	if !m.q.Push(j) {
		return def.ErrMailboxWorkerIsFull
	}
	return nil
}
func (m *admissionBoundedManager) NextJob() (inf.IMailboxJob, bool) { return nil, false }
func (m *admissionBoundedManager) GetJobLen() int                   { return m.q.Len() }
func (m *admissionBoundedManager) IsEmpty() bool                    { return m.q.Empty() }
func (m *admissionBoundedManager) DrainAll(fn func(inf.IMailboxJob)) {
	for {
		j, ok := m.q.Pop()
		if !ok {
			return
		}
		fn(j)
	}
}

func TestWorkerAdmissionFullQueueOwnership(t *testing.T) {
	for _, policy := range []DrainPolicy{DrainExecute, DrainDiscard} {
		t.Run(fmt.Sprint(policy), func(t *testing.T) {
			inv := &releaseTrackingInvoker{}
			w := newWorkerForSafeExecTest(t, inv)
			q, err := boundedmpsc.New[inf.IMailboxJob](2)
			if err != nil {
				t.Fatal(err)
			}
			w.queueManager = &admissionBoundedManager{q: q}
			w.drainPolicy = policy
			jobs := []*admissionOwnershipJob{{IMailboxJob: newJob()}, {IMailboxJob: newJob()}, {IMailboxJob: newJob()}}
			for _, j := range jobs[:2] {
				if err := w.SubmitJob(j); err != nil {
					t.Fatal(err)
				}
			}
			if err := w.SubmitJob(jobs[2]); err != def.ErrMailboxWorkerIsFull {
				t.Fatalf("got %v, want full", err)
			}
			for _, j := range jobs {
				if j.releases.Load() != 0 {
					t.Fatal("submission released Job")
				}
			}
			jobs[2].Release()
			w.BeginStop()
			w.Start() // closed worker runs only final drain
			w.Wait()
			for i, j := range jobs {
				if j.releases.Load() != 1 {
					t.Errorf("job %d releases=%d", i, j.releases.Load())
				}
			}
			if !q.Empty() {
				t.Fatal("residual publication")
			}
			if policy == DrainExecute && (inv.executeCount.Load() != 2 || inv.discardCount.Load() != 0) {
				t.Fatal("execute ownership mismatch")
			}
			if policy == DrainDiscard && (inv.executeCount.Load() != 0 || inv.discardCount.Load() != 2) {
				t.Fatal("discard ownership mismatch")
			}
		})
	}
}
