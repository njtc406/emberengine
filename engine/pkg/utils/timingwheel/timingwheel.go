package timingwheel

import (
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

	logger log.ILoggerX

	// adjustMu protects time adjustment operations
	adjusting     atomic.Bool // 是否正在调整时间
	pendingTimers chan *Timer // 时间调整期间的待处理timer缓冲队列
	adjustMu      sync.RWMutex
}

// NewTimingWheel creates an instance of TimingWheel with the given tick and wheelSize.
func NewTimingWheel(tick time.Duration, wheelSize int64, logger log.ILoggerX) *TimingWheel {
	if logger == nil {
		l, err := log.NewDefaultLogger(nil)
		if err != nil {
			panic(fmt.Sprintf("create logger failed: %v", err))
		}
		logger = log.NewLoggerX(l, log.Fields{"pkg": "timingwheel"})
	}
	tickMs := int64(tick / time.Millisecond)
	if tickMs <= 0 {
		if logger == nil {
			panic("logger is nil")
		} else {
			logger.Panic("tick must be greater than or equal to 1ms")
		}
	}

	if wheelSize <= 0 {
		if logger == nil {
			panic("logger is nil")
		} else {
			logger.Panic("wheelSize must be greater than 0")
		}
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
func newTimingWheel(tickMs int64, wheelSize int64, startMs int64, queue *delayqueue.DelayQueue, logger log.ILoggerX, closed *atomic.Bool) *TimingWheel {
	buckets := make([]*bucket, wheelSize)
	for i := range buckets {
		buckets[i] = newBucket()
	}
	return &TimingWheel{
		tick:          tickMs,
		wheelSize:     wheelSize,
		currentTime:   truncate(startMs, tickMs),
		interval:      tickMs * wheelSize,
		buckets:       buckets,
		queue:         queue,
		exitC:         make(chan struct{}),
		logger:        logger,
		closed:        closed,
		pendingTimers: make(chan *Timer, 10000), // 缓冲队列，容量可配置
	}
}

// add inserts the timer t into the current timing wheel.
func (tw *TimingWheel) add(t *Timer) bool {
	// 如果timingwheel已关闭，不能添加任务
	if tw.closed.Load() {
		return false
	}

	// 如果正在调整时间,将timer放入缓冲队列
	if tw.adjusting.Load() {
		select {
		case tw.pendingTimers <- t:
			return true // 已加入缓冲队列
		default:
			// 缓冲队列已满,降级为直接插入(极端情况)
			if tw.logger != nil {
				tw.logger.Warnf("[TimingWheel] Pending timer queue is full, fallback to direct insert")
			}
		}
	}

	return tw.addInternal(t)
}

// addInternal 内部插入逻辑,不检查adjusting标志
func (tw *TimingWheel) addInternal(t *Timer) bool {
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

// addOrRunDirect 直接插入timer,跳过adjusting检查(仅用于AdjustTime中处理缓冲队列)
func (tw *TimingWheel) addOrRunDirect(t *Timer) {
	if tw.closed.Load() {
		return
	}

	// 直接调用底层add逻辑,不经过adjusting检查
	if !tw.addInternal(t) {
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
// Time Adjustment Behavior:
// 1. Backward (offset < 0, e.g., 12:00 -> 11:00):
//   - All timers maintain relative delay unchanged
//   - If 3s remaining, still 3s after adjustment
//
// 2. Forward (offset > 0, e.g., 12:00 -> 13:00):
//   - Periodic timers (Ticker/Cron): if crossed next execution time
//     -> execute once immediately, then recalculate next execution
//   - One-shot timers (AfterFunc): if time reached
//     -> execute once and recycle
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

	// 设置调整标志,阻止新的timer直接插入
	tw.adjusting.Store(true)
	defer func() {
		// 重置标志
		tw.adjusting.Store(false)
		// 重置后,处理期间累积的所有pending timer
		tw.processPendingTimers()
	}()

	// 1. 收集所有活跃的 timer
	allTimers := tw.collectAllTimers()
	if tw.logger != nil {
		tw.logger.Infof("[TimingWheel] Collected %d active timers", len(allTimers))
	}

	oldCurrentTime := atomic.LoadInt64(&tw.currentTime)
	newCurrentTime := oldCurrentTime + offsetMs

	// 记录已执行的Timer,避免重复处理
	executedTimers := make(map[uint64]bool)

	// 2. 先调整currentTime，让loop函数使用新的时间
	tw.clearAllBuckets()
	atomic.StoreInt64(&tw.currentTime, truncate(newCurrentTime, tw.tick))

	// 3. 对于往后调时间(offset > 0),检查并执行跨过执行点的周期性任务
	if offsetMs > 0 {
		for _, t := range allTimers {
			if !t.isActive() {
				continue
			}

			oldExpiration := t.GetExpiration()
			// 如果新的当前时间已经超过了原定的执行时间,说明跨过了执行点
			if newCurrentTime >= oldExpiration {
				// 周期性任务: Ticker 或 Cron
				if t.interval > 0 || t.isCron {
					if tw.logger != nil {
						tw.logger.Infof("[TimingWheel] Periodic timer %s crossed execution time, executing once", t.name)
					}
					// 先更新expiration为新的currentTime,让loop函数从新时间计算下一次执行
					t.SetExpiration(newCurrentTime)
					// 立即执行一次,runLoop=true会自动重新调度到下一次执行时间
					tw.runTimer(t, true)
					// 记录已执行,不需要再手动处理
					executedTimers[t.GetTimerId()] = true
				} else {
					// AfterFunc(一次性任务),立即执行
					if tw.logger != nil {
						tw.logger.Infof("[TimingWheel] One-time timer %s crossed execution time, executing", t.name)
					}
					tw.runTimer(t, false) // runLoop=false,不重复执行
					executedTimers[t.GetTimerId()] = true
				}
			}
		}
	}

	// 4. 处理未执行的Timer
	for _, t := range allTimers {
		if !t.isActive() {
			continue
		}

		// 跳过已执行的周期性任务(它们已经由loop函数重新调度)
		if executedTimers[t.GetTimerId()] {
			continue
		}

		// 调整过期时间,维持相对延迟
		oldExpiration := t.GetExpiration()
		newExpiration := oldExpiration + offsetMs
		t.SetExpiration(newExpiration)

		// 重新插入时间轮
		tw.addOrRun(t)
	}

	// 5. 递归调整 overflow wheel
	overflowWheel := atomic.LoadPointer(&tw.overflowWheel)
	if overflowWheel != nil {
		(*TimingWheel)(overflowWheel).adjustTimeInternal(offsetMs)
	}

	if tw.logger != nil {
		tw.logger.Infof("[TimingWheel] Time adjustment completed, currentTime: %d -> %d", oldCurrentTime, newCurrentTime)
	}
	// defer会在这里执行: 重置adjusting标志并处理累积的pending timers
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

// processPendingTimers 处理缓冲队列中所有累积的pending timers
// 此方法会持续排空队列,直到没有新的timer为止
func (tw *TimingWheel) processPendingTimers() {
	// 持续排空,直到队列为空且短时间内没有新的timer进入
	for {
		processed := 0
		// 一次性处理当前队列中的所有timer
		for {
			select {
			case pendingTimer := <-tw.pendingTimers:
				// 使用普通的add方法,此时adjusting已经是false
				tw.addOrRun(pendingTimer)
				processed++
			default:
				// 队列已空,退出内层循环
				goto CHECK
			}
		}
	CHECK:
		if processed == 0 {
			// 本轮没有处理任何timer,说明队列已空
			break
		}
		if tw.logger != nil && processed > 0 {
			tw.logger.Infof("[TimingWheel] Processed %d pending timers", processed)
		}
		// 继续下一轮,确保处理期间新进入的timer
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

// collectAllTimers collects all active timers from all buckets (including overflow wheels)
func (tw *TimingWheel) collectAllTimers() []*Timer {
	timerMap := make(map[uint64]*Timer)
	tw.collectTimersRecursive(timerMap)

	// 转为slice
	timers := make([]*Timer, 0, len(timerMap))
	for _, t := range timerMap {
		timers = append(timers, t)
	}
	return timers
}

// collectTimersRecursive 递归收集timer，使用map去重
func (tw *TimingWheel) collectTimersRecursive(timerMap map[uint64]*Timer) {
	// 收集当前层的timer
	for _, b := range tw.buckets {
		b.mu.Lock()
		for e := b.timers.Front(); e != nil; e = e.Next() {
			if t, ok := e.Value.(*Timer); ok && t.isActive() {
				timerMap[t.GetTimerId()] = t // 自动去重
			}
		}
		b.mu.Unlock()
	}

	// 递归收集overflow wheel中的timer
	overflowWheel := atomic.LoadPointer(&tw.overflowWheel)
	if overflowWheel != nil {
		(*TimingWheel)(overflowWheel).collectTimersRecursive(timerMap)
	}
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
