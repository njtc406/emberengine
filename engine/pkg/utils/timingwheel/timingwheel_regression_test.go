package timingwheel

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/log"
)

// TimingWheel 回归/并发测试用例集合。
//
// 约定：
// - 每个 Test 上方用中文说明：验证点、覆盖的边界/竞态时序。
// - 测试函数命名遵循“测什么叫什么”，不使用 P0/P1/P2 前缀。

// ---------------------------------------------------------------------------
// Timer.Do() defer + nil scheduler
//
// 验证点：Timer.Do() 在 taskScheduler=nil 时不会 panic。
// 边界：
// - taskScheduler 为 nil（历史上 defer 中调用 scheduler.CancelTimer 触发空指针）
// - 直接调用 Do()（不依赖 timingwheel/scheduler 运行）
// ---------------------------------------------------------------------------
func TestTimerDo_NilScheduler_NoPanic(t *testing.T) {
	tm := &Timer{}
	tm.task = func(ctx context.Context, timer *Timer, args ...interface{}) error { return nil }
	// taskScheduler is intentionally nil

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Timer.Do() panicked with nil scheduler: %v", r)
		}
	}()

	if err := tm.Do(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ScheduleFunc loop + nil scheduler + wheel closed
//
// 验证点：ScheduleFunc 设置的 loop 在 timingwheel 已关闭且 scheduler=nil 时不 panic。
// 边界：
// - tw.closed=true（loop 触发“关闭时取消”逻辑）
// - t.taskScheduler=nil（历史上可能出现 nil.CancelTimer）
// - 直接调用 loop()（模拟最坏时序）
// ---------------------------------------------------------------------------
func TestScheduleFuncLoop_WheelClosedAndNilScheduler_NoPanic(t *testing.T) {
	// NewTimingWheel allows nil logger and will create a default logger.
	tw := NewTimingWheel(time.Millisecond, 20, nil)
	tm := &Timer{}
	tm.interval = time.Millisecond
	// taskScheduler is intentionally nil

	if err := tw.ScheduleFunc(tm); err != nil {
		t.Fatalf("ScheduleFunc failed: %v", err)
	}
	if tm.loop == nil {
		t.Fatalf("expected loop to be set")
	}

	// Simulate wheel being closed when loop runs.
	tw.closed.Store(true)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("loop panicked with nil scheduler on closed wheel: %v", r)
		}
	}()

	tm.loop()
}

// ---------------------------------------------------------------------------
// posted-then-cancelled timer pointer + pool reuse
//
// 验证点：Timer 已过期并被投递到 cb channel 后，又被 Cancel/Reset 回收复用，
// 消费者随后执行旧指针时：
// - 不 panic（避免 nil bucket/element 访问）
// - 返回 ErrTimerReuse（ABA/复用保护生效）
//
// 边界：
// - 先让 timer 过期 -> 进入 cb channel，但不消费
// - 再 CancelTimer（触发 stop/reset/pool.Put）
// - 最后才启动/放开消费者去 Do()（最大化“旧指针”概率）
// ---------------------------------------------------------------------------
func TestPostedThenCancelled_TimerReuseProtection(t *testing.T) {
	logger, err := log.NewDefaultLogger(nil)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	Start(time.Millisecond, 64, logger)
	defer Stop()

	const n = 2000
	scheduler := NewJobScheduler(
		"posted-then-cancelled",
		n+100, // ensure channel won't get full
		16,
		GetTimingWheel(),
		log.NewLoggerX(logger, log.Fields{"pkg": "posted-then-cancelled"}),
		false,
	)

	ids := make([]uint64, 0, n)
	for i := 0; i < n; i++ {
		id, err := scheduler.AfterFunc(time.Millisecond, fmt.Sprintf("timer_%d", i), func(ctx context.Context, timer *Timer, args ...interface{}) error {
			return nil
		})
		if err != nil {
			t.Fatalf("AfterFunc failed: %v", err)
		}
		ids = append(ids, id)
	}

	// Let timers expire and get posted to callback channel, but don't consume yet.
	time.Sleep(10 * time.Millisecond)

	// Cancel all timers while old timer pointers may already be in callback channel.
	for _, id := range ids {
		scheduler.CancelTimer(id)
	}

	ctx := context.Background()
	var reuseErrCount atomic.Int32
	var okCount atomic.Int32
	var otherErrCount atomic.Int32

	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		for it := range scheduler.GetTimerCbChannel() {
			err := it.Do(ctx)
			if errors.Is(err, def.ErrTimerReuse) {
				reuseErrCount.Add(1)
				continue
			}
			if err == nil {
				okCount.Add(1)
				continue
			}
			otherErrCount.Add(1)
		}
	}()

	// Give consumer time to drain, then stop scheduler to close the channel.
	time.Sleep(20 * time.Millisecond)
	scheduler.Stop()
	<-consumerDone

	if reuseErrCount.Load() == 0 {
		// If this is zero, the test didn't hit the critical path (timer reset while pointer is still pending).
		t.Fatalf("expected some ErrTimerReuse, got 0 (ok=%d otherErr=%d)", okCount.Load(), otherErrCount.Load())
	}
	if otherErrCount.Load() != 0 {
		t.Fatalf("unexpected non-nil errors: %d (reuse=%d ok=%d)", otherErrCount.Load(), reuseErrCount.Load(), okCount.Load())
	}
	if okCount.Load() != 0 {
		// Ideally all should be rejected as reuse because we cancelled before consuming.
		t.Fatalf("expected okCount=0, got %d (reuse=%d)", okCount.Load(), reuseErrCount.Load())
	}
}

// ---------------------------------------------------------------------------
// posted-then-cancelled 多批次压力
//
// 验证点：同上，但多批次/大数量，增强覆盖不同 interleaving。
// 边界：
// - 批量创建 -> 等过期 -> 批量取消
// - 所有批次都取消后才启动消费者（尽可能处理 stale pointers）
// ---------------------------------------------------------------------------
func TestPostedThenCancelled_StressManyBatches(t *testing.T) {
	logger, err := log.NewDefaultLogger(nil)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	Start(time.Millisecond, 128, logger)
	defer Stop()

	scheduler := NewJobScheduler(
		"posted-then-cancelled-stress",
		200000,
		32,
		GetTimingWheel(),
		log.NewLoggerX(logger, log.Fields{"pkg": "posted-then-cancelled-stress"}),
		false,
	)

	ctx := context.Background()
	var reuseErrCount atomic.Int32
	var okCount atomic.Int32
	var otherErrCount atomic.Int32

	const (
		batches   = 20
		perBatch  = 2000
		expireDur = 2 * time.Millisecond
	)

	for b := 0; b < batches; b++ {
		ids := make([]uint64, 0, perBatch)
		for i := 0; i < perBatch; i++ {
			id, err := scheduler.AfterFunc(expireDur, "stress", func(ctx context.Context, timer *Timer, args ...interface{}) error {
				return nil
			})
			if err != nil {
				t.Fatalf("AfterFunc failed (batch=%d i=%d): %v", b, i, err)
			}
			ids = append(ids, id)
		}

		// Ensure most timers have expired and been posted before cancelling them.
		time.Sleep(10 * time.Millisecond)
		for _, id := range ids {
			scheduler.CancelTimer(id)
		}
	}

	// Start consumer only AFTER cancellations, so it processes stale timer pointers.
	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		for it := range scheduler.GetTimerCbChannel() {
			err := it.Do(ctx)
			if errors.Is(err, def.ErrTimerReuse) {
				reuseErrCount.Add(1)
				continue
			}
			if err == nil {
				okCount.Add(1)
				continue
			}
			otherErrCount.Add(1)
		}
	}()

	// Let consumer drain.
	time.Sleep(50 * time.Millisecond)
	scheduler.Stop()
	<-consumerDone

	if otherErrCount.Load() != 0 {
		t.Fatalf("unexpected non-nil errors: %d (reuse=%d ok=%d)", otherErrCount.Load(), reuseErrCount.Load(), okCount.Load())
	}
	if reuseErrCount.Load() == 0 {
		// Not strictly required for correctness, but indicates we exercised the critical path.
		t.Fatalf("expected some ErrTimerReuse in stress test, got 0 (ok=%d)", okCount.Load())
	}
}

// ---------------------------------------------------------------------------
// GetName() 与 Reset 并发
//
// 验证点：GetName 与 Reset 并发不 panic。
// 边界：
// - Reset 可能清空 name/task 等字段
// - GetName 并发读取，确保内部同步/快照逻辑安全
// ---------------------------------------------------------------------------
func TestTimerGetName_ConcurrentWithReset_NoPanic(t *testing.T) {
	const n = 500
	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		tm := &Timer{}
		tm.name = "hello"
		tm.task = func(ctx context.Context, timer *Timer, args ...interface{}) error { return nil }

		wg.Add(2)
		go func() {
			defer wg.Done()
			// Simulate Reset path
			tm.Reset()
		}()
		go func() {
			defer wg.Done()
			// Concurrent GetName — should not panic or race
			_ = tm.GetName()
		}()
	}
	wg.Wait()
}

// 边界补充：在 Do() 回调内部调用 GetName()（同一 timer 正在执行中）。
func TestTimerGetName_InsideDo_ReturnsCorrectName(t *testing.T) {
	tm := &Timer{}
	tm.name = "inside-do-test"
	tm.task = func(ctx context.Context, timer *Timer, args ...interface{}) error {
		name := timer.GetName()
		if name != "inside-do-test" {
			t.Errorf("expected name 'inside-do-test', got '%s'", name)
		}
		return nil
	}

	if err := tm.Do(context.Background()); err != nil {
		t.Fatalf("Do() unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Reset() 与 bucket.Flush 并发（element 读写）
//
// 验证点：Reset() 与 bucket.Flush 并发时不 panic。
// 边界：
// - Flush 读取 element 链表
// - Reset 通过 bucket.Remove 安全清理 element（避免裸写 nil）
// ---------------------------------------------------------------------------
func TestTimerReset_ConcurrentWithBucketFlush_NoPanic(t *testing.T) {
	b := newBucket()
	const n = 200
	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		tm := &Timer{}
		b.Add(tm)

		wg.Add(2)
		go func() {
			defer wg.Done()
			// Concurrent Flush (reads element)
			b.Flush(func(timer *Timer) {})
		}()
		go func() {
			defer wg.Done()
			// Concurrent Reset (clears element safely via Remove)
			tm.Reset()
		}()
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// scheduler.Stop() 与 runTimer 投递并发（避免 send on closed channel）
//
// 验证点：scheduler.Stop() 与过期投递并发时不出现 "send on closed channel" panic。
// 边界：
// - timer 已过期、可能正准备投递到 cb channel
// - Stop 先标记 closed 再 close(channel)，与投递存在竞态
// ---------------------------------------------------------------------------
func TestJobSchedulerStop_NoSendOnClosedChannelPanic(t *testing.T) {
	logger, err := log.NewDefaultLogger(nil)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	Start(time.Millisecond, 64, logger)
	defer Stop()

	scheduler := NewJobScheduler(
		"closed-send",
		100,
		4,
		GetTimingWheel(),
		log.NewLoggerX(logger, log.Fields{"pkg": "closed-send"}),
		false,
	)

	// Create some timers that will expire soon
	for i := 0; i < 50; i++ {
		_, _ = scheduler.AfterFunc(time.Millisecond, "closed_send_test", func(ctx context.Context, timer *Timer, args ...interface{}) error {
			return nil
		})
	}

	// Let timers expire
	time.Sleep(10 * time.Millisecond)

	// Stop scheduler (closes channel) while expired timers might still be enqueued
	// This should not panic with "send on closed channel"
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Panic after scheduler.Stop(): %v", r)
		}
	}()

	scheduler.Stop()
	// Give a bit of time for any lingering goroutines
	time.Sleep(10 * time.Millisecond)
}

// 压力边界：反复创建 scheduler，并发 AfterFunc 与 Stop。
func TestJobScheduler_ConcurrentAddAndStop_NoPanic(t *testing.T) {
	logger, err := log.NewDefaultLogger(nil)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	Start(time.Millisecond, 64, logger)
	defer Stop()

	for round := 0; round < 10; round++ {
		scheduler := NewJobScheduler(
			"concurrent-stop",
			1000,
			4,
			GetTimingWheel(),
			log.NewLoggerX(logger, log.Fields{"pkg": "concurrent-stop"}),
			false,
		)

		ctx := context.Background()
		consumerDone := make(chan struct{})
		go func() {
			defer close(consumerDone)
			for it := range scheduler.GetTimerCbChannel() {
				_ = it.Do(ctx)
			}
		}()

		var wg sync.WaitGroup
		var addCount atomic.Int32

		// Goroutine 1: rapidly add timers
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				_, err := scheduler.AfterFunc(time.Millisecond, "rapid", func(ctx context.Context, timer *Timer, args ...interface{}) error {
					return nil
				})
				if err == nil {
					addCount.Add(1)
				}
			}
		}()

		// Goroutine 2: stop scheduler after a short delay
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(time.Millisecond * 2)
			scheduler.Stop()
		}()

		wg.Wait()
		_ = addCount.Load()
		<-consumerDone
	}
}

// ---------------------------------------------------------------------------
// Stop() 与回调里 CancelTimer 交织（避免死锁）
//
// 验证点：Stop() 与 Do() 回调里 CancelTimer 交织时不死锁。
// 边界：
// - 回调内 CancelTimer 需要获取 shard 锁/触发 release
// - Stop() 也会遍历 shard/tasks；历史上锁顺序导致互相等待
// ---------------------------------------------------------------------------
func TestJobSchedulerStop_NoDeadlock_WhenCallbackCancelsTimer(t *testing.T) {
	logger, err := log.NewDefaultLogger(nil)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	Start(time.Millisecond, 64, logger)
	defer Stop()

	scheduler := NewJobScheduler(
		"stop-deadlock",
		1000,
		4,
		GetTimingWheel(),
		log.NewLoggerX(logger, log.Fields{"pkg": "stop-deadlock"}),
		false,
	)

	// Store timer IDs so the callback can cancel other timers
	var ids sync.Map

	// Create timers whose callbacks will try to cancel other timers
	for i := 0; i < 100; i++ {
		idx := i
		id, err := scheduler.AfterFunc(time.Millisecond*5, "callback-cancel", func(ctx context.Context, timer *Timer, args ...interface{}) error {
			// Try to cancel another timer from within callback
			targetID := uint64(idx + 1)
			if _, ok := ids.Load(targetID); ok {
				scheduler.CancelTimer(targetID)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("AfterFunc failed: %v", err)
		}
		ids.Store(uint64(idx), id)
	}

	ctx := context.Background()
	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		for it := range scheduler.GetTimerCbChannel() {
			_ = it.Do(ctx)
		}
	}()

	// Let timers expire
	time.Sleep(20 * time.Millisecond)

	// Stop should not deadlock — test will time out if it does
	done := make(chan struct{})
	go func() {
		scheduler.Stop()
		close(done)
	}()

	select {
	case <-done:
		// OK, Stop returned
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() deadlocked")
	}

	<-consumerDone
}

// ---------------------------------------------------------------------------
// SetTimeOffset 与 timers/Flush 并发（不应全丢）
//
// 验证点：SetTimeOffset 与定时器触发/Flush 并发时，不会导致所有 timer 丢失（至少会执行一些）。
// 边界：
// - 多批次 SetTimeOffset（正向递增 + 最终归零）
// - timers 同时在到期、入队、flush、投递 cb channel
// ---------------------------------------------------------------------------
func TestSetTimeOffset_ConcurrentWithTimers_ExecutesSome(t *testing.T) {
	logger, err := log.NewDefaultLogger(nil)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	Start(time.Millisecond, 64, logger)
	defer Stop()

	scheduler := NewJobScheduler(
		"offset-race",
		10000,
		8,
		GetTimingWheel(),
		log.NewLoggerX(logger, log.Fields{"pkg": "offset-race"}),
		false,
	)

	ctx := context.Background()
	var executed atomic.Int32

	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		for it := range scheduler.GetTimerCbChannel() {
			if err := it.Do(ctx); err == nil {
				executed.Add(1)
			}
		}
	}()

	const numTimers = 100

	// Create timers with various delays
	for i := 0; i < numTimers; i++ {
		delay := time.Duration(10+i*2) * time.Millisecond
		_, err := scheduler.AfterFunc(delay, "offset", func(ctx context.Context, timer *Timer, args ...interface{}) error {
			return nil
		})
		if err != nil {
			t.Fatalf("AfterFunc failed: %v", err)
		}
	}

	// Concurrently adjust time offset while timers are firing
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			time.Sleep(5 * time.Millisecond)
			SetTimeOffset(time.Duration(i*10) * time.Millisecond)
		}
		// Reset offset
		SetTimeOffset(0)
	}()

	wg.Wait()
	time.Sleep(500 * time.Millisecond)

	scheduler.Stop()
	<-consumerDone

	ex := executed.Load()
	t.Logf("Executed %d / %d timers during offset adjustment", ex, numTimers)

	// We should have executed a reasonable number of timers (not all lost)
	if ex == 0 {
		t.Fatalf("No timers executed during offset adjustment — likely lost")
	}
}

// ---------------------------------------------------------------------------
// genTimerId() 溢出时跳过 0
//
// 验证点：timerIdSeed 溢出回 0 时应跳过，永不返回 0。
// 边界：
// - 直接把 seed 设置到 MaxUint64-1，连续调用覆盖：MaxUint64、(skip 0 -> 1)、2
// ---------------------------------------------------------------------------
func TestTimingWheelGenTimerID_NeverReturnsZero(t *testing.T) {
	tw := NewTimingWheel(time.Millisecond, 20, nil)
	tw.Start()
	defer tw.Stop()

	// Test normal generation doesn't return 0
	const n = 10000
	for i := 0; i < n; i++ {
		id := tw.genTimerId()
		if id == 0 {
			t.Fatalf("genTimerId returned 0 at iteration %d", i)
		}
	}

	// Simulate overflow scenario: set seed to MaxUint64-1
	atomic.StoreUint64(&tw.timerIdSeed, ^uint64(0)-1) // MaxUint64 - 1

	// Next call should return MaxUint64
	id1 := tw.genTimerId()
	if id1 != ^uint64(0) {
		t.Errorf("expected MaxUint64, got %d", id1)
	}

	// Next call would overflow to 0, but should skip to 1
	id2 := tw.genTimerId()
	if id2 == 0 {
		t.Fatal("genTimerId returned 0 after overflow")
	}
	if id2 != 1 {
		t.Errorf("expected 1 after overflow skip, got %d", id2)
	}

	// Subsequent calls should continue normally
	id3 := tw.genTimerId()
	if id3 != 2 {
		t.Errorf("expected 2, got %d", id3)
	}
}

// ---------------------------------------------------------------------------
// overflow wheel 共享 adjusting 状态
//
// 验证点：SetTimeOffset(adjusting=true) 期间，落在 overflow wheel 的 timer 不会走错分支/丢失。
// 边界：
// - interval 很小，强制大量 timer 进入 overflow wheel
// - SetTimeOffset 与 AfterFunc 并发
// ---------------------------------------------------------------------------
func TestOverflowWheel_SharesAdjustingState(t *testing.T) {
	// tick=10ms, wheelSize=10 => interval=100ms
	// Timer with delay > 100ms will go to overflow wheel
	tw := NewTimingWheel(10*time.Millisecond, 10, nil)
	tw.Start()
	defer tw.Stop()

	scheduler := NewJobScheduler("overflow-adjusting", 1000, 10, tw, nil, true)
	defer scheduler.Stop()

	ctx := context.Background()
	go func() {
		for timer := range scheduler.GetTimerCbChannel() {
			_ = timer.Do(ctx)
		}
	}()

	var executed atomic.Int32
	const numTimers = 50

	var wg sync.WaitGroup
	wg.Add(numTimers + 1)

	// Start SetTimeOffset in parallel with timer additions
	go func() {
		defer wg.Done()
		time.Sleep(5 * time.Millisecond)
		tw.SetTimeOffset(200 * time.Millisecond)
	}()

	for i := 0; i < numTimers; i++ {
		go func() {
			defer wg.Done()
			// 150ms delay will exceed the 100ms interval, forcing overflow wheel
			_, _ = scheduler.AfterFunc(150*time.Millisecond, "overflow-timer", func(ctx context.Context, timer *Timer, args ...interface{}) error {
				executed.Add(1)
				return nil
			})
		}()
	}

	wg.Wait()
	time.Sleep(200 * time.Millisecond)

	count := executed.Load()
	if count < int32(numTimers)*70/100 {
		t.Errorf("expected at least %d timers to execute, got %d", numTimers*70/100, count)
	}
}

// ---------------------------------------------------------------------------
// SetTimeOffset + overflow timers（不应卡住/超时）
//
// 验证点：SetTimeOffset + overflow timers 场景下不会卡住（历史上会超时）。
// 边界：
// - 预先插入 overflow timers
// - 同步触发 SetTimeOffset，设置超时保护
// ---------------------------------------------------------------------------
func TestSetTimeOffset_WithOverflowTimers_NoDeadlock(t *testing.T) {
	// tick=5ms, wheelSize=10 => interval=50ms
	tw := NewTimingWheel(5*time.Millisecond, 10, nil)
	tw.Start()
	defer tw.Stop()

	scheduler := NewJobScheduler("offset-with-overflow", 1000, 10, tw, nil, true)
	defer scheduler.Stop()

	ctx := context.Background()
	go func() {
		for timer := range scheduler.GetTimerCbChannel() {
			_ = timer.Do(ctx)
		}
	}()

	var executed atomic.Int32
	const numTimers = 20

	for i := 0; i < numTimers; i++ {
		_, _ = scheduler.AfterFunc(80*time.Millisecond, "long-delay", func(ctx context.Context, timer *Timer, args ...interface{}) error {
			executed.Add(1)
			return nil
		})
	}

	time.Sleep(10 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		tw.SetTimeOffset(100 * time.Millisecond)
		close(done)
	}()

	select {
	case <-done:
		// Success - no deadlock
	case <-time.After(5 * time.Second):
		t.Fatal("SetTimeOffset deadlocked (timeout after 5s)")
	}

	time.Sleep(100 * time.Millisecond)
	count := executed.Load()
	if count < int32(numTimers)*70/100 {
		t.Errorf("expected at least %d timers to execute, got %d", numTimers*70/100, count)
	}
}

// ---------------------------------------------------------------------------
// adjusting 期间并发 add（不应 busy-wait）
//
// 验证点：并发添加 timer + 多次 SetTimeOffset 能在合理时间内完成。
// 边界：
// - 高频 AfterFunc
// - 多次 SetTimeOffset
// - 用耗时阈值作为“busy-wait 回退”的代理检测
// ---------------------------------------------------------------------------
func TestAddDuringAdjust_NoBusyWait(t *testing.T) {
	tw := NewTimingWheel(time.Millisecond, 20, nil)
	tw.Start()
	defer tw.Stop()

	scheduler := NewJobScheduler("no-busy-wait", 1000, 10, tw, nil, true)
	defer scheduler.Stop()

	ctx := context.Background()
	go func() {
		for timer := range scheduler.GetTimerCbChannel() {
			_ = timer.Do(ctx)
		}
	}()

	const numTimers = 100
	var wg sync.WaitGroup

	start := time.Now()

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numTimers/10; j++ {
				_, _ = scheduler.AfterFunc(50*time.Millisecond, "timer", func(ctx context.Context, timer *Timer, args ...interface{}) error {
					return nil
				})
				time.Sleep(time.Microsecond)
			}
		}()
	}

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			time.Sleep(time.Duration(offset*10) * time.Millisecond)
			tw.SetTimeOffset(time.Duration(offset*100) * time.Millisecond)
		}(i + 1)
	}

	wg.Wait()
	elapsed := time.Since(start)
	if elapsed > 3*time.Second {
		t.Errorf("operations took too long (%v), possible busy-wait issue", elapsed)
	}

	time.Sleep(200 * time.Millisecond)
}

// ---------------------------------------------------------------------------
// processPendingTimers 排空压力
//
// 验证点：SetTimeOffset 期间积累的 pending timers 能被稳定排空并执行一定比例。
// 边界：
// - 批次并发添加 timer
// - 中途触发 SetTimeOffset
// ---------------------------------------------------------------------------
func TestProcessPendingTimers_DrainsAndExecutes(t *testing.T) {
	tw := NewTimingWheel(time.Millisecond, 20, nil)
	tw.Start()
	defer tw.Stop()

	scheduler := NewJobScheduler("pending-drain", 2000, 10, tw, nil, true)
	defer scheduler.Stop()

	ctx := context.Background()
	go func() {
		for timer := range scheduler.GetTimerCbChannel() {
			_ = timer.Do(ctx)
		}
	}()

	var executed atomic.Int32
	const numTimers = 500

	var wg sync.WaitGroup
	for batch := 0; batch < 5; batch++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < numTimers/5; i++ {
				_, _ = scheduler.AfterFunc(time.Duration(10+i%50)*time.Millisecond, "stress", func(ctx context.Context, timer *Timer, args ...interface{}) error {
					executed.Add(1)
					return nil
				})
			}
		}()

		if batch == 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				tw.SetTimeOffset(50 * time.Millisecond)
			}()
		}
	}

	wg.Wait()
	time.Sleep(300 * time.Millisecond)

	count := executed.Load()
	if count < int32(numTimers)/2 {
		t.Errorf("too few timers executed: %d/%d", count, numTimers)
	}
}

// ---------------------------------------------------------------------------
// DelayQueue 短生命周期 timer 压力（避免累积泄漏）
//
// 验证点：快速创建/过期一批定时器，Poll 循环频繁 sleep/wakeup，测试可稳定完成。
// 边界：
// - iterations 较大（覆盖大量 NewTimer/Stop 路径）
// - 非功能性指标：不要求精确计数，但至少执行一定比例
// ---------------------------------------------------------------------------
func TestDelayQueue_NoTimerLeakUnderChurn(t *testing.T) {
	tw := NewTimingWheel(time.Millisecond, 20, nil)
	tw.Start()
	defer tw.Stop()

	scheduler := NewJobScheduler("delayqueue-churn", 1000, 10, tw, nil, true)
	defer scheduler.Stop()

	ctx := context.Background()
	go func() {
		for timer := range scheduler.GetTimerCbChannel() {
			_ = timer.Do(ctx)
		}
	}()

	var executed atomic.Int32
	const iterations = 100

	for i := 0; i < iterations; i++ {
		_, _ = scheduler.AfterFunc(time.Millisecond, "leak-timer", func(ctx context.Context, timer *Timer, args ...interface{}) error {
			executed.Add(1)
			return nil
		})
		time.Sleep(2 * time.Millisecond)
	}

	time.Sleep(50 * time.Millisecond)

	count := executed.Load()
	if count < int32(iterations)*80/100 {
		t.Errorf("expected at least %d executions, got %d", iterations*80/100, count)
	}
}

// ---------------------------------------------------------------------------
// 综合压力：小 interval + overflow + 多次 SetTimeOffset + 并发 AfterFunc
//
// 验证点：上述修复组合在一起不丢任务、不死锁。
// 边界：
// - 同时存在 current wheel timers 与 overflow timers
// - 多次 offset 调整
// ---------------------------------------------------------------------------
func TestTimingWheel_CombinedStress_OverflowAndTimeOffset(t *testing.T) {
	// tick=5ms, wheelSize=10 => interval=50ms (forces overflow for >50ms)
	tw := NewTimingWheel(5*time.Millisecond, 10, nil)
	tw.Start()
	defer tw.Stop()

	scheduler := NewJobScheduler("combined-stress", 2000, 10, tw, nil, true)
	defer scheduler.Stop()

	ctx := context.Background()
	go func() {
		for timer := range scheduler.GetTimerCbChannel() {
			_ = timer.Do(ctx)
		}
	}()

	var executed atomic.Int32
	var wg sync.WaitGroup

	const (
		numShortTimers = 100
		numLongTimers  = 100
		numOffsets     = 5
	)

	for i := 0; i < numShortTimers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = scheduler.AfterFunc(20*time.Millisecond, "short-timer", func(ctx context.Context, timer *Timer, args ...interface{}) error {
				executed.Add(1)
				return nil
			})
		}()
	}

	for i := 0; i < numLongTimers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = scheduler.AfterFunc(100*time.Millisecond, "long-timer", func(ctx context.Context, timer *Timer, args ...interface{}) error {
				executed.Add(1)
				return nil
			})
		}()
	}

	for i := 0; i < numOffsets; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			time.Sleep(time.Duration(idx*20) * time.Millisecond)
			tw.SetTimeOffset(time.Duration((idx+1)*50) * time.Millisecond)
		}(i)
	}

	wg.Wait()
	time.Sleep(500 * time.Millisecond)

	count := executed.Load()
	total := int32(numShortTimers + numLongTimers)
	if count < total*70/100 {
		t.Errorf("too few timers executed: %d/%d (%.1f%%)", count, total, float64(count)/float64(total)*100)
	}
}
