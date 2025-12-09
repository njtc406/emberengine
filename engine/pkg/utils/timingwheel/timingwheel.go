package timingwheel

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/timelib"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel/delayqueue"
)

// TimingWheel is an implementation of Hierarchical Timing Wheels.
type TimingWheel struct {
	tick      int64 // in milliseconds
	wheelSize int64

	interval    int64 // in milliseconds
	currentTime int64 // in milliseconds
	buckets     []*bucket
	queue       *delayqueue.DelayQueue

	timerIdSeed uint64

	// The higher-level overflow wheel.
	//
	// NOTE: This field may be updated and read concurrently, through Add().
	overflowWheel unsafe.Pointer // type: *TimingWheel

	exitC     chan struct{}
	closed    *atomic.Bool
	waitGroup waitGroupWrapper

	logger *log.Logger

	// adjustMu protects time adjustment operations
	adjustMu sync.RWMutex
}

// NewTimingWheel creates an instance of TimingWheel with the given tick and wheelSize.
func NewTimingWheel(tick time.Duration, wheelSize int64, logger *log.Logger) *TimingWheel {
	tickMs := int64(tick / time.Millisecond)
	if tickMs <= 0 {
		panic(errors.New("tick must be greater than or equal to 1ms"))
	}

	startMs := timeToMs(timelib.Now())

	return newTimingWheel(
		tickMs,
		wheelSize,
		startMs,
		delayqueue.New(int(wheelSize)),
		logger,
		new(atomic.Bool),
	)
}

// newTimingWheel is an internal helper function that really creates an instance of TimingWheel.
func newTimingWheel(tickMs int64, wheelSize int64, startMs int64, queue *delayqueue.DelayQueue, logger *log.Logger, closed *atomic.Bool) *TimingWheel {
	buckets := make([]*bucket, wheelSize)
	for i := range buckets {
		buckets[i] = newBucket()
	}
	return &TimingWheel{
		tick:        tickMs,
		wheelSize:   wheelSize,
		currentTime: truncate(startMs, tickMs),
		interval:    tickMs * wheelSize,
		buckets:     buckets,
		queue:       queue,
		exitC:       make(chan struct{}),
		logger:      logger,
		closed:      closed,
	}
}

// add inserts the timer t into the current timing wheel.
func (tw *TimingWheel) add(t *Timer) bool {
	// 如果timingwheel已关闭，不能添加任务
	if tw.closed.Load() {
		return false
	}

	currentTime := atomic.LoadInt64(&tw.currentTime)
	expire := t.GetExpiration()
	if expire < currentTime+tw.tick {
		// Already expired
		return false
	} else if expire < currentTime+tw.interval {
		// Put it into its own bucket
		virtualID := expire / tw.tick
		b := tw.buckets[virtualID%tw.wheelSize]
		b.Add(t)

		// Set the bucket expiration time
		if b.SetExpiration(virtualID * tw.tick) {
			// The bucket needs to be enqueued since it was an expired bucket.
			// We only need to enqueue the bucket when its expiration time has changed,
			// i.e. the wheel has advanced and this bucket get reused with a new expiration.
			// Any further calls to set the expiration within the same wheel cycle will
			// pass in the same value and hence return false, thus the bucket with the
			// same expiration will not be enqueued multiple times.
			tw.queue.Offer(b, b.Expiration())
		}

		return true
	} else {
		// Out of the interval. Put it into the overflow wheel
		overflowWheel := atomic.LoadPointer(&tw.overflowWheel)
		if overflowWheel == nil {
			atomic.CompareAndSwapPointer(
				&tw.overflowWheel,
				nil,
				unsafe.Pointer(newTimingWheel(
					tw.interval,
					tw.wheelSize,
					currentTime,
					tw.queue,
					tw.logger,
					tw.closed,
				)),
			)
			overflowWheel = atomic.LoadPointer(&tw.overflowWheel)
		}
		return (*TimingWheel)(overflowWheel).add(t)
	}
}

// runTimer 执行Timer的任务
// runLoop: 是否执行循环逻辑(loop函数)
func (tw *TimingWheel) runTimer(t *Timer, runLoop bool) {
	if !t.isActive() {
		return
	}

	// 在执行前冻结snapGen，防止ABA问题
	t.snapGen.Store(t.generation.Load())
	taskArgs := t.taskArgs
	loop := t.loop

	if t.asyncTask != nil {
		// 异步任务,在独立goroutine中执行
		asyncTask := t.asyncTask
		go func() {
			defer func() {
				if err := recover(); err != nil {
					if tw.logger != nil {
						tw.logger.Errorf("task panic, task_name:%s, err:%v", t.name, err)
					} else {
						fmt.Printf("task panic, task_name:%s, err:%v\n", t.name, err)
					}
				}
				if loop == nil {
					// 不是循环任务，释放
					t.taskScheduler.CancelTimer(t.GetTimerId())
				}
			}()
			asyncTask(taskArgs...)
		}()
	} else if t.task != nil {
		// 同步任务,投递到callback channel,由消费者执行
		select {
		case t.taskScheduler.GetTimerCbChannel() <- t:
			// 投递成功
		default:
			// 队列已满,本次不执行
			if tw.logger != nil {
				tw.logger.Errorf("task queue is full, task will not be executed, task_name:%s", t.name)
			} else {
				fmt.Printf("task queue is full, task will not be executed, task_name:%s\n", t.name)
			}
			if loop == nil {
				// 不是循环任务，释放
				t.taskScheduler.CancelTimer(t.GetTimerId())
			}
		}
	}

	if runLoop && t.loop != nil {
		// 循环任务,再次加入
		t.loop()
	}
}

// addOrRun inserts the timer t into the current timing wheel, or run the
// timer's task if it has already expired.
func (tw *TimingWheel) addOrRun(t *Timer) {
	if tw.closed.Load() {
		return
	}

	if !tw.add(t) {
		// 任务已经过期，立即执行
		tw.runTimer(t, true)
		return
	}
}

func (tw *TimingWheel) genTimerId() uint64 {
	return atomic.AddUint64(&tw.timerIdSeed, 1)
}

func (tw *TimingWheel) advanceClock(expiration int64) {
	currentTime := atomic.LoadInt64(&tw.currentTime)
	if expiration >= currentTime+tw.tick {
		currentTime = truncate(expiration, tw.tick)
		atomic.StoreInt64(&tw.currentTime, currentTime)

		// Try to advance the clock of the overflow wheel if present
		overflowWheel := atomic.LoadPointer(&tw.overflowWheel)
		if overflowWheel != nil {
			(*TimingWheel)(overflowWheel).advanceClock(currentTime)
		}
	}
}

// Start starts the current timing wheel.
func (tw *TimingWheel) Start() {
	tw.waitGroup.Wrap(func() {
		tw.queue.Poll(tw.exitC, func() int64 {
			return timeToMs(timelib.Now())
		})
	})

	tw.waitGroup.Wrap(func() {
		for {
			select {
			case elem := <-tw.queue.C:
				b := elem.(*bucket)
				tw.advanceClock(b.Expiration())
				b.Flush(tw.addOrRun)
			case <-tw.exitC:
				return
			}
		}
	})
}

// Stop stops the current timing wheel.
//
// If there is any timer's task being running in its own goroutine, Stop does
// not wait for the task to complete before returning. If the caller needs to
// know whether the task is completed, it must coordinate with the task explicitly.
func (tw *TimingWheel) Stop() {
	if tw.closed.Swap(true) {
		return
	}
	close(tw.exitC)
	tw.waitGroup.Wait()
}

func (tw *TimingWheel) IsClosed() bool {
	return tw.closed.Load()
}

// AfterFunc waits for the duration to elapse and then calls f in its own goroutine.
// It returns a Timer that can be used to cancel the call using its Stop method.
func (tw *TimingWheel) AfterFunc(d time.Duration, t *Timer) {
	t.SetExpiration(timeToMs(timelib.Now().Add(d)))

	tw.addOrRun(t)
}

// Scheduler determines the execution plan of a task.
type Scheduler interface {
	// Next returns the next execution time after the given (previous) time.
	// It will return a zero time if no next time is scheduled.
	//
	// All times must be UTC.
	Next(time.Time) time.Time
}

// ScheduleFunc calls f (in its own goroutine) according to the execution
// plan scheduled by s. It returns a Timer that can be used to cancel the
// call using its Stop method.
//
// If the caller want to terminate the execution plan halfway, it must
// stop the timer and ensure that the timer is stopped actually, since in
// the current implementation, there is a gap between the expiring and the
// restarting of the timer. The wait time for ensuring is short since the
// gap is very small.
//
// Internally, ScheduleFunc will ask the first execution time (by calling
// s.Next()) initially, and create a timer if the execution time is non-zero.
// Afterwards, it will ask the next execution time each time f is about to
// be executed, and f will be called at the next execution time if the time
// is non-zero.
func (tw *TimingWheel) ScheduleFunc(t *Timer) error {
	expiration := t.Next(timelib.Now())
	if expiration.IsZero() {
		return fmt.Errorf("next time is zero")
	}

	t.SetExpiration(timeToMs(expiration))
	t.loop = func() {
		// 如果timingwheel已关闭，不能添加任务
		if tw.closed.Load() {
			t.taskScheduler.CancelTimer(t.GetTimerId())
			return
		}
		if !t.isActive() {
			return
		}
		expiration := t.Next(msToTime(t.GetExpiration()))
		if !expiration.IsZero() {
			t.SetExpiration(timeToMs(expiration))
			tw.addOrRun(t)
		}
	}

	tw.addOrRun(t)

	return nil
}

// AdjustTime adjusts all timers in the timing wheel after time offset change.
// This is designed for development/testing environments only.
// offsetMs: the time offset in milliseconds (can be positive or negative)
//
// WARNING: This operation is expensive and will block all timer operations.
// DO NOT use in production environment.
//
// IMPORTANT: Different timer types have different behaviors:
// - Regular timers (AfterFunc, TickerFunc): maintain absolute time, trigger immediately if expired
// - Cron timers: check if adjustment crosses trigger point, execute once if crossed
func (tw *TimingWheel) AdjustTime(offsetMs int64) {
	if tw.closed.Load() {
		return
	}

	// 使用写锁保护整个调整过程
	tw.adjustMu.Lock()
	defer tw.adjustMu.Unlock()

	if tw.logger != nil {
		tw.logger.Infof("[TimingWheel] Start adjusting time, offset: %d ms", offsetMs)
	}

	// 1. 收集所有活跃的 timer
	allTimers := tw.collectAllTimers()
	if tw.logger != nil {
		tw.logger.Infof("[TimingWheel] Collected %d active timers", len(allTimers))
	}

	// 2. 对Cron定时器做特殊处理:检查是否跨过触发点
	oldCurrentTime := atomic.LoadInt64(&tw.currentTime)
	newCurrentTime := oldCurrentTime + offsetMs

	for _, t := range allTimers {
		if t.isCron && t.isActive() {
			// 检查时间调整是否跨过了触发点
			if tw.shouldTriggerCronOnAdjust(t, oldCurrentTime, newCurrentTime) {
				if tw.logger != nil {
					tw.logger.Infof("[TimingWheel] Cron timer %s crossed trigger point, executing once", t.name)
				}
				// 立即触发一次执行
				tw.executeCronTimerOnce(t)
			}
		}
	}

	// 3. 清空所有 bucket
	tw.clearAllBuckets()

	// 4. 调整 currentTime
	atomic.StoreInt64(&tw.currentTime, truncate(newCurrentTime, tw.tick))

	// 5. 重新插入所有 timer
	for _, t := range allTimers {
		tw.addOrRun(t)
	}

	// 6. 递归调整 overflow wheel
	overflowWheel := atomic.LoadPointer(&tw.overflowWheel)
	if overflowWheel != nil {
		(*TimingWheel)(overflowWheel).adjustTimeInternal(offsetMs)
	}

	if tw.logger != nil {
		tw.logger.Infof("[TimingWheel] Time adjustment completed, currentTime: %d -> %d", oldCurrentTime, newCurrentTime)
	}
}

// adjustCurrentTime只调整currentTime,不处理Timer
func (tw *TimingWheel) adjustCurrentTime(offsetMs int64) {
	tw.adjustMu.Lock()
	defer tw.adjustMu.Unlock()

	oldCurrentTime := atomic.LoadInt64(&tw.currentTime)
	newCurrentTime := oldCurrentTime + offsetMs
	atomic.StoreInt64(&tw.currentTime, truncate(newCurrentTime, tw.tick))

	// 递归调整 overflow wheel
	overflowWheel := atomic.LoadPointer(&tw.overflowWheel)
	if overflowWheel != nil {
		(*TimingWheel)(overflowWheel).adjustTimeInternal(offsetMs)
	}
}

// reinsertAllTimers 重新插入所有Timer到时间轮
func (tw *TimingWheel) reinsertAllTimers() {
	tw.adjustMu.Lock()
	defer tw.adjustMu.Unlock()

	// 1. 收集所有活跃的 timer
	allTimers := tw.collectAllTimers()

	// 2. 清空所有 bucket
	tw.clearAllBuckets()

	// 3. 重新插入所有 timer
	for _, t := range allTimers {
		tw.addOrRun(t)
	}
}

// adjustTimeInternal is used for overflow wheels (no need to collect timers again)
func (tw *TimingWheel) adjustTimeInternal(offsetMs int64) {
	if tw.closed.Load() {
		return
	}

	// 调整 currentTime
	oldCurrentTime := atomic.LoadInt64(&tw.currentTime)
	newCurrentTime := oldCurrentTime + offsetMs
	atomic.StoreInt64(&tw.currentTime, truncate(newCurrentTime, tw.tick))

	// 递归调整 overflow wheel
	overflowWheel := atomic.LoadPointer(&tw.overflowWheel)
	if overflowWheel != nil {
		(*TimingWheel)(overflowWheel).adjustTimeInternal(offsetMs)
	}
}

// collectAllTimers collects all active timers from all buckets
func (tw *TimingWheel) collectAllTimers() []*Timer {
	var timers []*Timer

	for _, b := range tw.buckets {
		b.mu.Lock()
		for e := b.timers.Front(); e != nil; e = e.Next() {
			if t, ok := e.Value.(*Timer); ok && t.isActive() {
				timers = append(timers, t)
			}
		}
		b.mu.Unlock()
	}

	return timers
}

// clearAllBuckets clears all timers from all buckets
func (tw *TimingWheel) clearAllBuckets() {
	for _, b := range tw.buckets {
		b.mu.Lock()
		// 清空 bucket 中的所有 timer
		for e := b.timers.Front(); e != nil; {
			next := e.Next()
			if t, ok := e.Value.(*Timer); ok {
				t.setBucket(nil)
				t.element = nil
			}
			b.timers.Remove(e)
			e = next
		}
		// 重置过期时间
		atomic.StoreInt64(&b.expiration, -1)
		b.mu.Unlock()
	}
}

// shouldTriggerCronOnAdjust 检查Cron定时器在时间调整时是否应该触发
// 如果时间调整跨过了Cron的触发点,返回true
func (tw *TimingWheel) shouldTriggerCronOnAdjust(t *Timer, oldCurrentTime, newCurrentTime int64) bool {
	if t.spec == "" {
		return false
	}

	// 解析cron表达式
	sd, err := cronParser.Parse(t.spec)
	if err != nil {
		return false
	}

	// 计算时间范围
	oldTime := msToTime(oldCurrentTime)
	newTime := msToTime(newCurrentTime)

	// 确保oldTime < newTime (处理时间前进和后退两种情况)
	if oldTime.After(newTime) {
		// 时间回退,交换
		oldTime, newTime = newTime, oldTime
	}

	// 检查在这个时间范围内是否有触发点
	// 获取oldTime之后的第一个触发点
	nextTrigger := sd.Next(oldTime)

	// 如果nextTrigger在newTime之前,说明跨过了触发点
	return !nextTrigger.IsZero() && nextTrigger.Before(newTime)
}

// executeCronTimerOnce 立即投递一次Cron定时器执行
// 使用runTimer公共逻辑,避免重复代码和数据竞争
func (tw *TimingWheel) executeCronTimerOnce(t *Timer) {
	// runLoop=false: Cron定时器的时间调整触发不需要执行loop逻辑
	tw.runTimer(t, false)
}
