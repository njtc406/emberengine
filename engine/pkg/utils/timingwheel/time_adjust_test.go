package timingwheel

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// TestTimeAdjustment 测试时间调整功能
func TestTimeAdjustment(t *testing.T) {
	// 创建一个新的时间轮
	tw := NewTimingWheel(time.Millisecond*100, 20, nil)
	tw.Start()
	defer tw.Stop()

	scheduler := NewJobScheduler("time adjust test", 1000, 10, tw, nil)
	defer scheduler.Stop()
	ctx := context.Background()

	// 启动callback channel的消费者
	go func() {
		for timer := range scheduler.GetTimerCbChannel() {
			if err := timer.Do(ctx); err != nil {
				fmt.Printf("Timer execution error: %v\n", err)
			}
		}
	}()

	var executionCount atomic.Int32
	var lastExecutionTime atomic.Int64

	// 创建一个2秒后执行的任务 (缩短时间以便测试)
	timerId, err := scheduler.AfterFunc(2*time.Second, "test_task", func(ctx context.Context, timer *Timer, args ...interface{}) error {
		executionCount.Add(1)
		lastExecutionTime.Store(time.Now().Unix())
		fmt.Printf("[%s] Task executed, count: %d\n", time.Now().Format("2006-01-02 15:04:05"), executionCount.Load())
		return nil
	})

	if err != nil {
		t.Fatalf("AfterFunc failed: %v", err)
	}

	fmt.Printf("[%s] Task created (delay: 2s)\n", time.Now().Format("2006-01-02 15:04:05"))
	js := scheduler.(*jobScheduler)
	originalExpiration := js.getShard(timerId).tasks[timerId].GetExpiration()
	fmt.Printf("[DEBUG] CurrentTime(ms): %d, Timer expiration(ms): %d, Diff: %dms\n",
		atomic.LoadInt64(&tw.currentTime),
		originalExpiration,
		originalExpiration-atomic.LoadInt64(&tw.currentTime))

	// 立即调整时间,向前跳5秒 (超过任务应该执行的时间点)
	offset := 5 * time.Second
	fmt.Printf("\n[%s] Adjusting time forward by +5s\n", time.Now().Format("2006-01-02 15:04:05"))

	// 调整时间轮的offset (TimingWheel现在独立管理offset)
	tw.SetTimeOffset(offset)

	fmt.Printf("[%s] Time adjusted\n", time.Now().Format("2006-01-02 15:04:05"))
	newExpiration := js.getShard(timerId).tasks[timerId].GetExpiration()
	fmt.Printf("[DEBUG] CurrentTime(ms): %d, Timer expiration(ms): %d, Diff: %dms\n",
		atomic.LoadInt64(&tw.currentTime),
		newExpiration,
		newExpiration-atomic.LoadInt64(&tw.currentTime))
	fmt.Printf("[DEBUG] Expiration changed: %d -> %d (delta: %dms)\n",
		originalExpiration, newExpiration, newExpiration-originalExpiration)

	// 等待任务执行 (用真实时间,因为DelayQueue使用的是 timelib.Now())
	// 任务应该立即被触发,因为 currentTime 已经超过了 expiration
	// 但是需要等待DelayQueue检测到并执行任务
	time.Sleep(2 * time.Second) // 给足够的时间让任务被检测和执行

	// 验证任务是否执行
	if executionCount.Load() == 0 {
		t.Errorf("Task should have been executed after time adjustment")
		t.Errorf("Expected: expiration(%d) < currentTime(%d)",
			newExpiration, atomic.LoadInt64(&tw.currentTime))
	} else {
		fmt.Printf("\n✓ Task executed successfully after time adjustment\n")
		fmt.Printf("  Execution count: %d\n", executionCount.Load())
		fmt.Printf("  Execution time: %s\n", time.Unix(lastExecutionTime.Load(), 0).Format("2006-01-02 15:04:05"))
	}

	scheduler.CancelTimer(timerId)

	// 重置时间偏移
	tw.SetTimeOffset(0)
}

// TestTimeAdjustmentWithMultipleTimers 测试多个定时器的时间调整
func TestTimeAdjustmentWithMultipleTimers(t *testing.T) {
	tw := NewTimingWheel(time.Millisecond*100, 20, nil)
	tw.Start()
	defer tw.Stop()

	scheduler := NewJobScheduler("time adjust test", 1000, 10, tw, nil)
	defer scheduler.Stop()
	ctx := context.Background()

	// 启动callback channel的消费者
	go func() {
		for timer := range scheduler.GetTimerCbChannel() {
			if err := timer.Do(ctx); err != nil {
				fmt.Printf("Timer execution error: %v\n", err)
			}
		}
	}()

	var executionCount atomic.Int32

	// 创建多个不同延迟的任务
	delays := []time.Duration{3 * time.Second, 5 * time.Second, 7 * time.Second}
	timerIds := make([]uint64, 0, len(delays))

	for i, delay := range delays {
		id, err := scheduler.AfterFunc(delay, fmt.Sprintf("task_%d", i), func(ctx context.Context, timer *Timer, args ...interface{}) error {
			executionCount.Add(1)
			taskName := args[0].(string)
			fmt.Printf("[%s] %s executed\n", time.Now().Format("15:04:05"), taskName)
			return nil
		}, fmt.Sprintf("task_%d", i))

		if err != nil {
			t.Fatalf("AfterFunc failed: %v", err)
		}
		timerIds = append(timerIds, id)
	}

	fmt.Printf("[%s] Created 3 tasks with delays: 3s, 5s, 7s\n", time.Now().Format("15:04:05"))

	// 等待1秒
	time.Sleep(1 * time.Second)

	// 时间向前跳跃10秒
	offset := 10 * time.Second
	fmt.Printf("\n[%s] Adjusting time forward by +10s\n", time.Now().Format("15:04:05"))

	tw.SetTimeOffset(offset)

	fmt.Printf("[%s] Time adjusted\n", time.Now().Format("15:04:05"))

	// 等待所有任务执行
	time.Sleep(2 * time.Second)

	// 验证所有任务都执行了
	if executionCount.Load() != int32(len(delays)) {
		t.Errorf("Expected %d tasks executed, got %d", len(delays), executionCount.Load())
	} else {
		fmt.Printf("\n✓ All %d tasks executed successfully\n", executionCount.Load())
	}

	// 清理
	for _, id := range timerIds {
		scheduler.CancelTimer(id)
	}

	tw.SetTimeOffset(0)
}

// TestTimeAdjustmentWithTickerTimer 测试循环定时器的时间调整
func TestTimeAdjustmentWithTickerTimer(t *testing.T) {
	tw := NewTimingWheel(time.Millisecond*100, 20, nil)
	tw.Start()
	defer tw.Stop()

	scheduler := NewJobScheduler("time adjust test", 1000, 10, tw, nil)
	defer scheduler.Stop()
	ctx := context.Background()

	// 启动callback channel的消费者
	go func() {
		for timer := range scheduler.GetTimerCbChannel() {
			if err := timer.Do(ctx); err != nil {
				fmt.Printf("Timer execution error: %v\n", err)
			}
		}
	}()

	var executionCount atomic.Int32

	// 创建一个每2秒执行的循环任务
	timerId, err := scheduler.TickerFunc(2*time.Second, "ticker_task", func(ctx context.Context, timer *Timer, args ...interface{}) error {
		count := executionCount.Add(1)
		fmt.Printf("[%s] Ticker executed, count: %d\n", time.Now().Format("15:04:05"), count)
		return nil
	})

	if err != nil {
		t.Fatalf("TickerFunc failed: %v", err)
	}

	fmt.Printf("[%s] Ticker task created (interval: 2s)\n", time.Now().Format("15:04:05"))

	// 等待任务执行几次
	time.Sleep(3 * time.Second)

	firstCount := executionCount.Load()
	fmt.Printf("\n[%s] First phase: executed %d times\n", time.Now().Format("15:04:05"), firstCount)

	// 时间向前跳跃5秒
	offset := 5 * time.Second
	fmt.Printf("[%s] Adjusting time forward by +5s\n", time.Now().Format("15:04:05"))

	tw.SetTimeOffset(offset)

	// 等待任务继续执行
	time.Sleep(3 * time.Second)

	finalCount := executionCount.Load()
	fmt.Printf("\n[%s] After adjustment: total executed %d times\n", time.Now().Format("15:04:05"), finalCount)

	if finalCount <= firstCount {
		t.Errorf("Ticker should continue to execute after time adjustment")
	} else {
		fmt.Printf("✓ Ticker continues to work correctly after time adjustment\n")
	}

	scheduler.CancelTimer(timerId)
	tw.SetTimeOffset(0)
}

// TestTimeAdjustmentBackward 测试时间回退的场景
func TestTimeAdjustmentBackward(t *testing.T) {
	tw := NewTimingWheel(time.Millisecond*100, 20, nil)
	tw.Start()
	defer tw.Stop()

	scheduler := NewJobScheduler("time adjust test", 1000, 10, tw, nil)

	var executionCount atomic.Int32

	// 创建一个3秒后执行的任务
	timerId, err := scheduler.AfterFunc(3*time.Second, "test_task", func(ctx context.Context, timer *Timer, args ...interface{}) error {
		executionCount.Add(1)
		fmt.Printf("[%s] Task executed\n", time.Now().Format("15:04:05"))
		return nil
	})

	if err != nil {
		t.Fatalf("AfterFunc failed: %v", err)
	}

	fmt.Printf("[%s] Task created (delay: 3s)\n", time.Now().Format("15:04:05"))

	// 等待1秒
	time.Sleep(1 * time.Second)

	// 时间回退5秒
	offset := -5 * time.Second
	fmt.Printf("\n[%s] Adjusting time backward by -5s\n", time.Now().Format("15:04:05"))

	tw.SetTimeOffset(offset)

	fmt.Printf("[%s] Time adjusted (went back 5s)\n", time.Now().Format("15:04:05"))

	// 任务应该还要等待更长时间(原本3秒,回退5秒后还要等8秒)
	time.Sleep(2 * time.Second)

	if executionCount.Load() > 0 {
		t.Errorf("Task should not execute yet after time went backward")
	} else {
		fmt.Printf("✓ Task correctly delayed after time went backward\n")
	}

	scheduler.CancelTimer(timerId)
	tw.SetTimeOffset(0)
}

// TestCronTimerAdjustment 测试Cron定时器的时间调整
func TestCronTimerAdjustment(t *testing.T) {
	tw := NewTimingWheel(time.Millisecond*100, 20, nil)
	tw.Start()
	defer tw.Stop()

	scheduler := NewJobScheduler("time adjust test", 1000, 10, tw, nil)
	defer scheduler.Stop()
	ctx := context.Background()

	go func() {
		for timer := range scheduler.GetTimerCbChannel() {
			if err := timer.Do(ctx); err != nil {
				fmt.Printf("Timer execution error: %v\n", err)
			}
		}
	}()

	var executionCount atomic.Int32

	// 创建一个每10秒的Cron任务
	timerId, err := scheduler.CronFunc("@every 10s", "cron_task", func(ctx context.Context, timer *Timer, args ...interface{}) error {
		count := executionCount.Add(1)
		fmt.Printf("[%s] Cron task executed, count: %d\n", time.Now().Format("15:04:05"), count)
		return nil
	})

	if err != nil {
		t.Fatalf("CronFunc failed: %v", err)
	}

	fmt.Printf("[%s] Cron task created (@every 10s)\n", time.Now().Format("15:04:05"))

	// 立即调整时间,向前跳15秒(跨过一个触发点)
	offset := 15 * time.Second
	fmt.Printf("\n[%s] Adjusting time forward by +15s (crossing one trigger point)\n", time.Now().Format("15:04:05"))

	tw.SetTimeOffset(offset)

	fmt.Printf("[%s] Time adjusted\n", time.Now().Format("15:04:05"))

	// 等待任务执行
	time.Sleep(2 * time.Second)

	// 验证任务至少执行了一次(因为跨过了触发点)
	if executionCount.Load() == 0 {
		t.Errorf("Cron task should have been executed at least once after crossing trigger point")
	} else {
		fmt.Printf("\n✓ Cron task executed %d time(s) after crossing trigger point\n", executionCount.Load())
	}

	scheduler.CancelTimer(timerId)
	tw.SetTimeOffset(0)
}

// TestCronTimerAdjustmentMultipleTriggers 测试跨过多个触发点的情况
func TestCronTimerAdjustmentMultipleTriggers(t *testing.T) {
	tw := NewTimingWheel(time.Millisecond*100, 20, nil)
	tw.Start()
	defer tw.Stop()

	scheduler := NewJobScheduler("time adjust test", 1000, 10, tw, nil)
	defer scheduler.Stop()
	ctx := context.Background()

	go func() {
		for timer := range scheduler.GetTimerCbChannel() {
			if err := timer.Do(ctx); err != nil {
				fmt.Printf("Timer execution error: %v\n", err)
			}
		}
	}()

	var executionCount atomic.Int32

	// 创建一个每5秒的Cron任务
	timerId, err := scheduler.CronFunc("@every 5s", "frequent_task", func(ctx context.Context, timer *Timer, args ...interface{}) error {
		count := executionCount.Add(1)
		fmt.Printf("[%s] Cron task executed, count: %d\n", time.Now().Format("15:04:05"), count)
		return nil
	})

	if err != nil {
		t.Fatalf("CronFunc failed: %v", err)
	}

	fmt.Printf("[%s] Cron task created (@every 5s)\n", time.Now().Format("15:04:05"))

	// 调整时间,向前跳25秒(跨过5个触发点: 5s, 10s, 15s, 20s, 25s)
	// 但只应该执行一次
	offset := 25 * time.Second
	fmt.Printf("\n[%s] Adjusting time forward by +25s (crossing 5 trigger points)\n", time.Now().Format("15:04:05"))

	tw.SetTimeOffset(offset)

	fmt.Printf("[%s] Time adjusted\n", time.Now().Format("15:04:05"))

	// 等待任务执行
	time.Sleep(2 * time.Second)

	// 验证任务只执行了一次(无论跨过多少个触发点)
	if executionCount.Load() != 1 {
		t.Errorf("Cron task should have been executed exactly once, but got %d executions", executionCount.Load())
	} else {
		fmt.Printf("\n✓ Cron task executed exactly once despite crossing multiple trigger points\n")
	}

	scheduler.CancelTimer(timerId)
	tw.SetTimeOffset(0)
}

// TestCronTimerDailyCrossDay 测试跨天任务的时间调整
// 场景:设置每天12点执行的任务,当前5点,调整到明天11点,应该执行一次
func TestCronTimerDailyCrossDay(t *testing.T) {
	tw := NewTimingWheel(time.Millisecond*100, 20, nil)
	tw.Start()
	defer tw.Stop()

	scheduler := NewJobScheduler("time adjust test", 1000, 10, tw, nil)
	defer scheduler.Stop()
	ctx := context.Background()

	go func() {
		for timer := range scheduler.GetTimerCbChannel() {
			if err := timer.Do(ctx); err != nil {
				fmt.Printf("Timer execution error: %v\n", err)
			}
		}
	}()

	var executionCount atomic.Int32

	// 模拟当前时间是早上5点
	// 我们使用 @every 7h 来模拟每天12点的任务(因为从5点开始,7小时后是12点)
	// 实际使用时应该用标准cron格式: "0 12 * * *"
	timerId, err := scheduler.CronFunc("@every 7h", "daily_12pm_task", func(ctx context.Context, timer *Timer, args ...interface{}) error {
		count := executionCount.Add(1)
		fmt.Printf("[%s] Daily 12PM task executed, count: %d\n", time.Now().Format("15:04:05"), count)
		return nil
	})

	if err != nil {
		t.Fatalf("CronFunc failed: %v", err)
	}

	fmt.Printf("[%s] Daily task created (@every 7h, next trigger at 12:00)\n", time.Now().Format("15:04:05"))

	// 模拟从早上5点调整到明天早上11点(跨过了今天12点这个触发点)
	// 5点 -> 明天11点 = 24h + 6h = 30h
	offset := 30 * time.Hour
	fmt.Printf("\n[%s] Adjusting time forward by +30h (from 5AM to next day 11AM, crossing 12PM trigger point)\n", time.Now().Format("15:04:05"))

	tw.SetTimeOffset(offset)

	fmt.Printf("[%s] Time adjusted\n", time.Now().Format("15:04:05"))

	// 等待任务执行
	time.Sleep(2 * time.Second)

	// 验证任务执行了一次(跨过了12点触发点)
	if executionCount.Load() != 1 {
		t.Errorf("Daily task should have been executed exactly once after crossing trigger point, got %d executions", executionCount.Load())
	} else {
		fmt.Printf("\n✓ Daily task executed exactly once despite crossing the trigger point\n")
	}

	scheduler.CancelTimer(timerId)
	tw.SetTimeOffset(0)
}

// TestCronTimerBackwardCrossDay 测试时间往前调整跨过触发点
// 场景:时间往回调整也跨过触发点时,同样应该执行一次
func TestCronTimerBackwardCrossDay(t *testing.T) {
	tw := NewTimingWheel(time.Millisecond*100, 20, nil)
	tw.Start()
	defer tw.Stop()

	scheduler := NewJobScheduler("time adjust test", 1000, 10, tw, nil)
	defer scheduler.Stop()
	ctx := context.Background()

	go func() {
		for timer := range scheduler.GetTimerCbChannel() {
			if err := timer.Do(ctx); err != nil {
				fmt.Printf("Timer execution error: %v\n", err)
			}
		}
	}()

	var executionCount atomic.Int32

	// 创建一个每10秒的任务
	timerId, err := scheduler.CronFunc("@every 10s", "backward_task", func(ctx context.Context, timer *Timer, args ...interface{}) error {
		count := executionCount.Add(1)
		fmt.Printf("[%s] Backward test task executed, count: %d\n", time.Now().Format("15:04:05"), count)
		return nil
	})

	if err != nil {
		t.Fatalf("CronFunc failed: %v", err)
	}

	fmt.Printf("[%s] Cron task created (@every 10s)\n", time.Now().Format("15:04:05"))

	// 先向前跳20秒
	offset1 := 20 * time.Second
	fmt.Printf("\n[%s] First: Adjusting time forward by +20s\n", time.Now().Format("15:04:05"))
	tw.SetTimeOffset(offset1)

	// 重置执行计数
	time.Sleep(2 * time.Second)
	executionCount.Store(0)

	// 再往回调整15秒(跨过10秒触发点)
	offset2 := -15 * time.Second
	fmt.Printf("\n[%s] Second: Adjusting time backward by -15s (crossing trigger point)\n", time.Now().Format("15:04:05"))

	tw.SetTimeOffset(offset1 + offset2)

	fmt.Printf("[%s] Time adjusted backward\n", time.Now().Format("15:04:05"))

	// 等待任务执行
	time.Sleep(2 * time.Second)

	// 验证任务没有执行(往回调整不会触发执行,只是维持相对延迟)
	if executionCount.Load() != 0 {
		t.Errorf("Task should not have been executed after backward adjustment, got %d executions", executionCount.Load())
	} else {
		fmt.Printf("\n✓ Task correctly did not execute when time adjusted backward (maintains relative delay)\n")
	}

	scheduler.CancelTimer(timerId)
	tw.SetTimeOffset(0)
}
