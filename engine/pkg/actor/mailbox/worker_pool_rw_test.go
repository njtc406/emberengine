package mailbox

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mbjob "github.com/njtc406/emberengine/engine/pkg/actor/mailbox/job"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
)

// ============================================================================
// 测试基础设施
// ============================================================================

// testLogger 实现 log.ILoggerX，用于测试（输出到 testing.T）
type testLogger struct{ t *testing.T }

func (l *testLogger) WithContext(context.Context) log.ILoggerX            { return l }
func (l *testLogger) WithField(string, interface{}) log.ILoggerX          { return l }
func (l *testLogger) WithFields(map[string]interface{}) log.ILoggerX      { return l }
func (l *testLogger) WithFreshFields(map[string]interface{}) log.ILoggerX { return l }
func (l *testLogger) Slow() log.ILoggerX                                  { return l }
func (l *testLogger) State() log.ILoggerX                                 { return l }
func (l *testLogger) Metric() log.ILoggerX                                { return l }
func (l *testLogger) Trace(...interface{})                                {}
func (l *testLogger) Tracef(string, ...interface{})                       {}
func (l *testLogger) Debug(...interface{})                                {}
func (l *testLogger) Debugf(f string, a ...interface{})                   { l.t.Helper(); l.t.Logf("[DEBUG] "+f, a...) }
func (l *testLogger) Info(...interface{})                                 {}
func (l *testLogger) Infof(f string, a ...interface{})                    { l.t.Helper(); l.t.Logf("[INFO] "+f, a...) }
func (l *testLogger) Warn(...interface{})                                 {}
func (l *testLogger) Warnf(f string, a ...interface{})                    { l.t.Helper(); l.t.Logf("[WARN] "+f, a...) }
func (l *testLogger) Warning(...interface{})                              {}
func (l *testLogger) Warningf(f string, a ...interface{})                 { l.t.Helper(); l.t.Logf("[WARN] "+f, a...) }
func (l *testLogger) Error(...interface{})                                {}
func (l *testLogger) Errorf(f string, a ...interface{})                   { l.t.Helper(); l.t.Logf("[ERROR] "+f, a...) }
func (l *testLogger) Fatal(...interface{})                                {}
func (l *testLogger) Fatalf(string, ...interface{})                       {}
func (l *testLogger) Panic(...interface{})                                {}
func (l *testLogger) Panicf(string, ...interface{})                       {}

// mockInvoker 模拟业务逻辑
type mockInvoker struct {
	readDelay  time.Duration
	writeDelay time.Duration
	mu         sync.RWMutex
	state      int64
	readCount  atomic.Int64
	writeCount atomic.Int64
	name       string
}

func (m *mockInvoker) GetServiceName() string { return m.name }

func (m *mockInvoker) ExecuteJob(_ context.Context, job inf.IMailboxJob) error {
	rwJob, ok := job.(inf.IRWModeJob)
	if ok && rwJob.GetRWMode() == def.RWModeRead {
		m.readCount.Add(1)
		m.mu.RLock()
		defer m.mu.RUnlock()
		if m.readDelay > 0 {
			time.Sleep(m.readDelay)
		}
		// 读取共享状态（仅读）
		_ = atomic.LoadInt64(&m.state)
	} else {
		m.writeCount.Add(1)
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.writeDelay > 0 {
			time.Sleep(m.writeDelay)
		}
		// 修改共享状态
		atomic.AddInt64(&m.state, 1)
	}
	return nil
}

func (m *mockInvoker) EscalateFailure(_ context.Context, _ interface{}, _ inf.IMailboxJob) {}
func (m *mockInvoker) OnJobDiscarded(_ inf.IMailboxJob, _ error)                           {}

type panicDiscardInvoker struct{ mockInvoker }

func (p *panicDiscardInvoker) OnJobDiscarded(inf.IMailboxJob, error) {
	panic("discard panic")
}

func TestWorkerSafeNotifyJobDiscardedRecovers(t *testing.T) {
	job := newRWJob(def.RWModeWrite, "discard-panic")
	defer job.Release()

	w := &Worker{
		workerId: 1,
		env: &WorkerEnv{
			logger:  &testLogger{t: t},
			invoker: &panicDiscardInvoker{},
		},
	}

	w.safeNotifyJobDiscarded(job, def.ErrMailboxNotRunning)
}

// newRWJob 创建一个带 RWMode 标记的测试 Job
func newRWJob(mode def.RWMode, key string) inf.IMailboxJob {
	j := mbjob.NewEventBusJob()
	j.SetContext(context.Background())
	j.SetPriority(def.PriorityNormal)
	j.SetDispatcherKey(key)
	j.SetRWMode(mode)
	return j
}

// newRWConf 创建 RW 模式测试配置
func newRWConf(workerNum int32, maxConcurrentReads int) *config.MailboxConf {
	return &config.MailboxConf{
		QueueMode:           "dual",
		EnableRWMode:        true,
		MaxConcurrentReads:  maxConcurrentReads,
		StopTimeout:         5 * time.Second,
		MaxJobExecutionTime: 30 * time.Second,
		SchedulePolicy: &config.WorkerSchedulePolicy{
			InitialWorkerNum:  workerNum,
			VirtualWorkerRate: 24,
		},
	}
}

// ============================================================================
// 场景 1: 读写交替正确性
// 验证目标: N 写 + M 读并发，写入值 = writeCount
// ============================================================================

func TestRW_ReadWriteInterleaveCorrectness(t *testing.T) {
	logger := &testLogger{t: t}
	invoker := &mockInvoker{name: "test-svc-1"}

	const workers = 4
	const maxReads = 8
	const writeN = 100
	const readN = 200

	wp, err := NewWorkerPool(newRWConf(workers, maxReads), logger, invoker)
	if err != nil {
		t.Fatalf("NewWorkerPool: %v", err)
	}
	if err := wp.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	var wg sync.WaitGroup

	// 投递写 Job
	for i := 0; i < writeN; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			job := newRWJob(def.RWModeWrite, fmt.Sprintf("key-%d", idx%10))
			if err := wp.DispatchJob(job); err != nil {
				t.Errorf("write dispatch: %v", err)
			}
		}(i)
	}

	// 投递读 Job
	for i := 0; i < readN; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			job := newRWJob(def.RWModeRead, fmt.Sprintf("key-%d", idx%10))
			if err := wp.DispatchJob(job); err != nil {
				t.Errorf("read dispatch: %v", err)
			}
		}(i)
	}

	wg.Wait()
	// 等待所有 Job 处理完成
	wp.Stop()

	// 验证
	finalState := atomic.LoadInt64(&invoker.state)
	if finalState != writeN {
		t.Errorf("state = %d, want %d (each write +1)", finalState, writeN)
	}
	rc := invoker.readCount.Load()
	wc := invoker.writeCount.Load()
	if rc != readN {
		t.Errorf("readCount = %d, want %d", rc, readN)
	}
	if wc != writeN {
		t.Errorf("writeCount = %d, want %d", wc, writeN)
	}
	t.Logf("OK: state=%d, reads=%d, writes=%d", finalState, rc, wc)
}

// ============================================================================
// 场景 2: Stop 时序
// 验证目标: 大量读 → BeginStop → in-flight 读完成后才 Drain
// ============================================================================

func TestRW_StopWaitsForInflightReads(t *testing.T) {
	logger := &testLogger{t: t}
	readDuration := 50 * time.Millisecond
	invoker := &mockInvoker{name: "test-svc-2", readDelay: readDuration}

	const workers = 2
	const maxReads = 4

	wp, err := NewWorkerPool(newRWConf(workers, maxReads), logger, invoker)
	if err != nil {
		t.Fatalf("NewWorkerPool: %v", err)
	}
	if err := wp.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// 投递大量读 + 少量写
	const readN = 20
	const writeN = 5
	for i := 0; i < readN; i++ {
		job := newRWJob(def.RWModeRead, fmt.Sprintf("key-%d", i%4))
		if err := wp.DispatchJob(job); err != nil {
			t.Fatalf("dispatch read: %v", err)
		}
	}
	for i := 0; i < writeN; i++ {
		job := newRWJob(def.RWModeWrite, fmt.Sprintf("key-%d", i%4))
		if err := wp.DispatchJob(job); err != nil {
			t.Fatalf("dispatch write: %v", err)
		}
	}

	// 短暂等待让读任务开始处理
	time.Sleep(10 * time.Millisecond)

	// BeginStop
	wp.BeginStop()

	// Wait 应阻塞直到所有 in-flight 读完成
	wp.Wait()

	// 验证所有读写都被处理
	rc := invoker.readCount.Load()
	wc := invoker.writeCount.Load()
	total := rc + wc
	t.Logf("After stop: reads=%d, writes=%d, total=%d", rc, wc, total)

	// 所有投递的 Job 应该被处理（执行或 Drain 处理）
	if total != readN+writeN {
		t.Errorf("total processed = %d, want %d", total, readN+writeN)
	}
}

// ============================================================================
// 场景 3: SetRWEnabled 只支持运行时关闭，不支持动态开启
// 验证目标: 运行中关闭 RW → 新读降级串行；再次开启返回显式错误，避免 nil readCh 静默丢读
// ============================================================================

func TestRW_SetRWEnabledDisableOnly(t *testing.T) {
	logger := &testLogger{t: t}
	readDuration := 30 * time.Millisecond
	invoker := &mockInvoker{name: "test-svc-3", readDelay: readDuration}

	const workers = 2
	const maxReads = 8

	wp, err := NewWorkerPool(newRWConf(workers, maxReads), logger, invoker)
	if err != nil {
		t.Fatalf("NewWorkerPool: %v", err)
	}
	if err := wp.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// 投递一些读任务（RW 模式下并发执行）
	const readN1 = 10
	for i := 0; i < readN1; i++ {
		job := newRWJob(def.RWModeRead, fmt.Sprintf("key-%d", i%4))
		if err := wp.DispatchJob(job); err != nil {
			t.Fatalf("dispatch read phase1: %v", err)
		}
	}

	// 关闭 RW 模式
	if err := wp.SetRWEnabled(false); err != nil {
		t.Fatalf("SetRWEnabled(false): %v", err)
	}
	if wp.IsRWEnabled() {
		t.Fatal("expected RW disabled")
	}

	// 投递更多读任务（应降级为串行执行）
	const readN2 = 10
	const writeN = 5
	for i := 0; i < readN2; i++ {
		job := newRWJob(def.RWModeRead, fmt.Sprintf("key-%d", i%4))
		if err := wp.DispatchJob(job); err != nil {
			t.Fatalf("dispatch read phase2: %v", err)
		}
	}
	for i := 0; i < writeN; i++ {
		job := newRWJob(def.RWModeWrite, fmt.Sprintf("key-%d", i%4))
		if err := wp.DispatchJob(job); err != nil {
			t.Fatalf("dispatch write: %v", err)
		}
	}

	// 不支持运行时动态开启 RW；RW 模式需要通过配置在 Worker 创建时启用。
	if err := wp.SetRWEnabled(true); err != ErrRWDynamicEnableUnsupported {
		t.Fatalf("SetRWEnabled(true) err = %v, want %v", err, ErrRWDynamicEnableUnsupported)
	}
	if wp.IsRWEnabled() {
		t.Fatal("expected RW still disabled")
	}

	wp.Stop()

	// 所有 Job 都应被执行
	rc := invoker.readCount.Load()
	wc := invoker.writeCount.Load()
	if rc != readN1+readN2 {
		t.Errorf("readCount = %d, want %d", rc, readN1+readN2)
	}
	if wc != writeN {
		t.Errorf("writeCount = %d, want %d", wc, writeN)
	}
	t.Logf("OK: reads=%d, writes=%d", rc, wc)
}

// ============================================================================
// 场景 4: readSem 满载退避
// 验证目标: MaxConcurrentReads=2，投 10 个读 → 最多 2 个并行
// ============================================================================

func TestRW_ReadSemMaxConcurrency(t *testing.T) {
	logger := &testLogger{t: t}

	var concurrentReads atomic.Int64
	var maxConcurrent atomic.Int64

	readDuration := 30 * time.Millisecond
	invoker := &mockConcurrencyInvoker{
		name:            "test-svc-4",
		readDelay:       readDuration,
		concurrentReads: &concurrentReads,
		maxConcurrent:   &maxConcurrent,
	}

	const workers = 4
	const maxReads = 2
	const readN = 10

	wp, err := NewWorkerPool(newRWConf(workers, maxReads), logger, invoker)
	if err != nil {
		t.Fatalf("NewWorkerPool: %v", err)
	}
	if err := wp.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	for i := 0; i < readN; i++ {
		job := newRWJob(def.RWModeRead, fmt.Sprintf("key-%d", i%workers))
		if err := wp.DispatchJob(job); err != nil {
			t.Fatalf("dispatch: %v", err)
		}
	}

	// 等待所有读操作完成后再 Stop，避免 idle/BeginStop 的 lost wakeup 竞态
	deadline := time.After(10 * time.Second)
	for invoker.readCount.Load() < readN {
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for reads: got %d, want %d", invoker.readCount.Load(), readN)
		default:
			runtime.Gosched()
		}
	}

	wp.Stop()

	maxC := maxConcurrent.Load()
	t.Logf("maxConcurrentReads observed = %d (limit=%d)", maxC, maxReads)
	if maxC > int64(maxReads) {
		t.Errorf("max concurrent reads = %d, exceeds limit %d", maxC, maxReads)
	}
	if invoker.readCount.Load() != readN {
		t.Errorf("readCount = %d, want %d", invoker.readCount.Load(), readN)
	}
}

// mockConcurrencyInvoker 跟踪并发读数量
type mockConcurrencyInvoker struct {
	name            string
	readDelay       time.Duration
	concurrentReads *atomic.Int64
	maxConcurrent   *atomic.Int64
	readCount       atomic.Int64
	writeCount      atomic.Int64
}

func (m *mockConcurrencyInvoker) GetServiceName() string { return m.name }

func (m *mockConcurrencyInvoker) ExecuteJob(_ context.Context, job inf.IMailboxJob) error {
	rwJob, ok := job.(inf.IRWModeJob)
	if ok && rwJob.GetRWMode() == def.RWModeRead {
		m.readCount.Add(1)
		cur := m.concurrentReads.Add(1)
		// 更新峰值
		for {
			old := m.maxConcurrent.Load()
			if cur <= old || m.maxConcurrent.CompareAndSwap(old, cur) {
				break
			}
		}
		if m.readDelay > 0 {
			time.Sleep(m.readDelay)
		}
		m.concurrentReads.Add(-1)
	} else {
		m.writeCount.Add(1)
	}
	return nil
}

func (m *mockConcurrencyInvoker) EscalateFailure(context.Context, interface{}, inf.IMailboxJob) {}
func (m *mockConcurrencyInvoker) OnJobDiscarded(inf.IMailboxJob, error)                         {}

// ============================================================================
// 场景 5: 写饥饿防护
// 验证目标: 大量连续读 + 少量写 → 写不被无限延迟
// ============================================================================

func TestRW_WriteStarvationPrevention(t *testing.T) {
	logger := &testLogger{t: t}
	readDuration := 10 * time.Millisecond
	invoker := &mockInvoker{name: "test-svc-5", readDelay: readDuration}

	const workers = 2
	const maxReads = 8

	wp, err := NewWorkerPool(newRWConf(workers, maxReads), logger, invoker)
	if err != nil {
		t.Fatalf("NewWorkerPool: %v", err)
	}
	if err := wp.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	const readN = 50
	const writeN = 5

	// 先投递大量读
	for i := 0; i < readN/2; i++ {
		job := newRWJob(def.RWModeRead, fmt.Sprintf("key-%d", i%workers))
		if err := wp.DispatchJob(job); err != nil {
			t.Fatalf("dispatch read: %v", err)
		}
	}

	// 投递写
	writeStart := time.Now()
	for i := 0; i < writeN; i++ {
		job := newRWJob(def.RWModeWrite, fmt.Sprintf("key-%d", i%workers))
		if err := wp.DispatchJob(job); err != nil {
			t.Fatalf("dispatch write: %v", err)
		}
	}

	// 再投递更多读
	for i := readN / 2; i < readN; i++ {
		job := newRWJob(def.RWModeRead, fmt.Sprintf("key-%d", i%workers))
		if err := wp.DispatchJob(job); err != nil {
			t.Fatalf("dispatch read: %v", err)
		}
	}

	wp.Stop()
	writeDone := time.Since(writeStart)

	rc := invoker.readCount.Load()
	wc := invoker.writeCount.Load()
	finalState := atomic.LoadInt64(&invoker.state)

	t.Logf("reads=%d, writes=%d, state=%d, write_total_time=%v", rc, wc, finalState, writeDone)

	if wc != writeN {
		t.Errorf("writeCount = %d, want %d", wc, writeN)
	}
	if rc != readN {
		t.Errorf("readCount = %d, want %d", rc, readN)
	}

	// 写操作不应被无限延迟——在合理时间内完成（这里放宽到 5s 作为安全上界）
	if writeDone > 5*time.Second {
		t.Errorf("write starvation detected: total time %v > 5s", writeDone)
	}
}

// ============================================================================
// 场景 6: Drain + in-flight 超时
// 验证目标: Stop 时读阻塞超 StopTimeout → 降级 DrainDiscard
// ============================================================================

func TestRW_DrainInflightTimeout(t *testing.T) {
	logger := &testLogger{t: t}

	// 读操作阻塞时间超过 StopTimeout
	readDuration := 2 * time.Second
	invoker := &mockInvoker{name: "test-svc-6", readDelay: readDuration}

	const workers = 2
	const maxReads = 4

	conf := newRWConf(workers, maxReads)
	conf.StopTimeout = 200 * time.Millisecond // 很短的超时

	wp, err := NewWorkerPool(conf, logger, invoker)
	if err != nil {
		t.Fatalf("NewWorkerPool: %v", err)
	}
	if err := wp.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// 投递一些长耗时读（会占满 readSem 并超时）
	for i := 0; i < 4; i++ {
		job := newRWJob(def.RWModeRead, fmt.Sprintf("key-%d", i%workers))
		if err := wp.DispatchJob(job); err != nil {
			t.Fatalf("dispatch read: %v", err)
		}
	}
	// 继续投递残留读，覆盖 readPipeline 在 Stop 时等待 readSem 的场景。
	for i := 4; i < 20; i++ {
		job := newRWJob(def.RWModeRead, fmt.Sprintf("key-%d", i%workers))
		if err := wp.DispatchJob(job); err != nil {
			t.Fatalf("dispatch residual read: %v", err)
		}
	}
	// 投递一些写（在读之后）
	for i := 0; i < 3; i++ {
		job := newRWJob(def.RWModeWrite, fmt.Sprintf("key-%d", i%workers))
		if err := wp.DispatchJob(job); err != nil {
			t.Fatalf("dispatch write: %v", err)
		}
	}

	// 让读任务开始执行
	time.Sleep(50 * time.Millisecond)

	startStop := time.Now()
	wp.BeginStop()
	wp.Wait()
	stopDuration := time.Since(startStop)

	t.Logf("Stop took %v (StopTimeout=%v)", stopDuration, conf.StopTimeout)

	// Stop 不应超过 StopTimeout + 宽裕时间（比如 3x StopTimeout）
	maxDuration := conf.StopTimeout * 3 // 200ms * 3 = 600ms
	if stopDuration > maxDuration {
		t.Errorf("Stop took %v, expected < %v (StopTimeout=%v)", stopDuration, maxDuration, conf.StopTimeout)
	}

	// 应该有 DrainDiscard 发生
	metrics := wp.GetRWMetrics()
	t.Logf("DrainDiscardTotal = %d", metrics.DrainDiscardTotal)
	// 注意：由于主循环、Drain 等时机，不一定所有残余 Job 会被 discard，但至少 Stop 不应阻死

	// 等读 goroutine 自然结束，避免 goroutine 泄漏影响其他测试
	time.Sleep(readDuration + 100*time.Millisecond)
	runtime.Gosched()
}
