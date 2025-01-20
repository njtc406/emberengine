package timingwheel

import (
	"errors"
	"github.com/njtc406/emberengine/engine/utils/timelib"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/njtc406/emberengine/engine/utils/timingwheel/delayqueue"
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
	waitGroup waitGroupWrapper
}

// NewTimingWheel creates an instance of TimingWheel with the given tick and wheelSize.
func NewTimingWheel(tick time.Duration, wheelSize int64) *TimingWheel {
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
	)
}

// newTimingWheel is an internal helper function that really creates an instance of TimingWheel.
func newTimingWheel(tickMs int64, wheelSize int64, startMs int64, queue *delayqueue.DelayQueue) *TimingWheel {
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
	}
}

// add inserts the timer t into the current timing wheel.
func (tw *TimingWheel) add(t *Timer) bool {
	currentTime := atomic.LoadInt64(&tw.currentTime)
	if t.expiration < currentTime+tw.tick {
		// Already expired
		return false
	} else if t.expiration < currentTime+tw.interval {
		// Put it into its own bucket
		virtualID := t.expiration / tw.tick
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
				)),
			)
			overflowWheel = atomic.LoadPointer(&tw.overflowWheel)
		}
		return (*TimingWheel)(overflowWheel).add(t)
	}
}

// addOrRun inserts the timer t into the current timing wheel, or run the
// timer's task if it has already expired.
func (tw *TimingWheel) addOrRun(t *Timer, isNew bool) {
	if !tw.add(t) {
		// 任务到期,立即执行
		if isNew {
			// 新增,执行onTimerAdd
			if t.onTimerAdd != nil {
				t.onTimerAdd(t)
			}
		}

		if t.asyncTask != nil {
			go t.asyncTask(t.taskArgs...)
		}

		if t.task != nil {
			// TODO 这里之后优化一下,和service的mailbox一起优化,使用mpse来接收消息,防止消费者太慢导致阻塞
			// 当然,这里几乎不会出现,如果出现,那么肯定是业务逻辑有问题,但是防止列表满导致任务丢失
			// 执行任务
			select {
			case t.c <- t:
			default:
				// 队列已满,本次不执行
				//log.SysLogger.Errorf("task queue is full, task will not be executed, taskId:%d ")
			}
		}

		if t.loop != nil {
			// 是循环任务,执行loop
			t.loop()
		} else {
			// 如果有关联了任务调度器,则移除调度器上的记录
			if t.scheduler != nil {
				_ = t.scheduler.remove(t.timerId)
			}

			// 执行任务删除逻辑
			if t.onTimerDel != nil {
				t.onTimerDel(t)
			}

			// 释放任务
			timerPool.Put(t)
		}
		return
	}
	if isNew {
		// 新增,执行onTimerAdd
		if t.onTimerAdd != nil {
			t.onTimerAdd(t)
		}
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
	close(tw.exitC)
	tw.waitGroup.Wait()
}

// AfterFunc waits for the duration to elapse and then calls f in its own goroutine.
// It returns a Timer that can be used to cancel the call using its Stop method.
func (tw *TimingWheel) AfterFunc(d time.Duration, options ...TimerOption) *Timer {
	t := timerPool.Get().(*Timer)
	t.expiration = timeToMs(timelib.Now().Add(d))
	for _, opt := range options {
		opt(t)
	}

	if t.timerId <= 0 {
		t.timerId = tw.genTimerId()
	}

	tw.addOrRun(t, true)
	return t
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
func (tw *TimingWheel) ScheduleFunc(options ...TimerOption) (t *Timer) {
	t = timerPool.Get().(*Timer)
	for _, opt := range options {
		opt(t)
	}
	expiration := t.Next(timelib.Now())
	if expiration.IsZero() {
		// No time is scheduled, return nil.
		timerPool.Put(t)
		return
	}

	if t.timerId <= 0 {
		t.timerId = tw.genTimerId()
	}
	t.expiration = timeToMs(expiration)
	t.loop = func() {
		if !t.isActive() {
			return
		}
		expiration := t.Next(msToTime(t.expiration))
		if !expiration.IsZero() {
			t.expiration = timeToMs(expiration)
			tw.addOrRun(t, false)
		}
	}

	tw.addOrRun(t, true)

	return
}
