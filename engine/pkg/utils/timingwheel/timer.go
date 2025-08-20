// Package timingwheel
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/8/21 0021 0:07
// 最后更新:  yr  2025/8/21 0021 0:07
package timingwheel

import (
	"container/list"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
	"github.com/njtc406/emberengine/engine/pkg/utils/timelib"
	"reflect"
	"runtime"
	"sync/atomic"
	"time"
	"unsafe"
)

type ITimer interface {
	Do()
	GetName() string
	GetTimerId() uint64
}

var timerPool = pool.NewSyncPoolWrapper(
	func() *Timer {
		return &Timer{}
	},
	pool.NewStatsRecorder("timerPool"),
	pool.WithRef(func(t *Timer) {
		t.Ref()
	}),
	pool.WithUnRef(func(t *Timer) {
		t.UnRef()
	}),
	pool.WithReset(func(t *Timer) {
		t.Reset()
	}),
)

func createTimer() *Timer {
	return timerPool.Get()
}

func releaseTimer(t *Timer) {
	if t.IsRef() {
		// 可能会在多线程中被调用,所以做个判断
		timerPool.Put(t)
	}
}

func GetTimerPoolStats() *pool.Stats {
	return timerPool.Stats()
}

type TimerOption func(t *Timer)
type TimerCallback func(timer *Timer, args ...interface{})

// Timer represents a single event. When the Timer expires, the given
// task will be executed.
type Timer struct {
	dto.DataRef
	Scheduler
	timerId    uint64               // 任务唯一id
	name       string               // 任务名称
	expiration atomic.Int64         // in milliseconds 任务到期时间
	interval   time.Duration        // 间隔时间 > 0 表示循环执行
	spec       string               // cron表达式
	cancel     atomic.Bool          // 任务是否已经取消
	task       TimerCallback        // 任务
	taskArgs   []interface{}        // 任务参数
	c          chan ITimer          // timer触发通道(后面看下能不能使用mpsc来替换channel,效率可能会更高一点,后面做一下channel和mpsc的性能对比)
	loop       func()               // 循环执行
	asyncTask  func(...interface{}) // 异步任务
	scheduler  *TaskScheduler       // 任务调度器

	// The bucket that holds the list to which this timer's element belongs.
	//
	// NOTE: This field may be updated and read concurrently,
	// through Timer.Stop() and Bucket.Flush().
	b unsafe.Pointer // type: *bucket

	// The timer's element.
	element *list.Element
}

func (t *Timer) Reset() {
	t.name = ""
	t.timerId = 0
	t.expiration.Store(0)
	t.interval = 0
	t.spec = ""
	t.cancel.Store(false)
	t.task = nil
	t.taskArgs = nil
	t.c = nil
	t.loop = nil
	t.asyncTask = nil
	t.b = nil
	t.element = nil
}

func (t *Timer) GetName() string {
	if t.name != "" {
		return t.name
	}
	if t.task != nil {
		return runtime.FuncForPC(reflect.ValueOf(t.task).Pointer()).Name()
	}
	if t.asyncTask != nil {
		return runtime.FuncForPC(reflect.ValueOf(t.asyncTask).Pointer()).Name()
	}

	return ""
}

func (t *Timer) GetTimerId() uint64 {
	return t.timerId
}

func (t *Timer) getBucket() *bucket {
	return (*bucket)(atomic.LoadPointer(&t.b))
}

func (t *Timer) setBucket(b *bucket) {
	atomic.StorePointer(&t.b, unsafe.Pointer(b))
}

// Stop prevents the Timer from firing. It returns true if the call
// stops the timer, false if the timer has already expired or been stopped.
//
// If the timer t has already expired and the t.task has been started in its own
// goroutine; Stop does not wait for t.task to complete before returning. If the caller
// needs to know whether t.task is completed, it must coordinate with t.task explicitly.
func (t *Timer) Stop() bool {
	if !t.IsRef() || !t.cancel.CompareAndSwap(false, true) {
		return false
	}
	stopped := false
	for b := t.getBucket(); b != nil; b = t.getBucket() {
		// If b.Remove is called just after the timing wheel's goroutine has:
		//     1. removed t from b (through b.Flush -> b.remove)
		//     2. moved t from b to another bucket ab (through b.Flush -> b.remove and ab.Add)
		// this may fail to remove t due to the change of t's bucket.
		stopped = b.Remove(t)

		// Thus, here we re-get t's possibly new bucket (nil for case 1, or ab (non-nil) for case 2),
		// and retry until the bucket becomes nil, which indicates that t has finally been removed.
	}
	return stopped
}

func (t *Timer) isActive() bool {
	return !t.cancel.Load()
}

func (t *Timer) Do() {
	if t.isActive() {
		if t.task != nil {
			t.task(t, t.taskArgs...)
		}

		if t.loop == nil {
			// 不是循环任务,释放任务
			// 如果有关联了任务调度器,则移除调度器上的记录
			if t.scheduler != nil {
				_ = t.scheduler.remove(t.timerId)
			}

			// 释放任务
			releaseTimer(t)
		}
		return
	}

	releaseTimer(t)
}

func (t *Timer) Next(tm time.Time) time.Time {
	if t.interval > 0 {
		return timelib.Now().Add(t.interval)
	}

	if t.spec != "" {
		sd, err := cronParser.Parse(t.spec)
		if err != nil {
			//log.SysLogger.Errorf("task %d parse cron [%s] failed: %v", t.timerId, t.spec, err)
			return time.Time{}
		}
		return sd.Next(tm)
	}

	return time.Time{}
}

func (t *Timer) SetTimerId(id uint64) {
	t.timerId = id
}

func (t *Timer) SetExpiration(expiration int64) {
	t.expiration.Store(expiration)
}

func (t *Timer) GetExpiration() int64 {
	return t.expiration.Load()
}

func (t *Timer) SetInterval(interval time.Duration) {
	t.interval = interval
}

func (t *Timer) SetSpec(spec string) {
	t.spec = spec
}

func (t *Timer) SetTask(task TimerCallback) {
	t.task = task
}

func (t *Timer) SetTaskArgs(args ...interface{}) {
	t.taskArgs = args
}

func (t *Timer) SetC(c chan ITimer) {
	t.c = c
}

func (t *Timer) SetAsyncTask(f func(...interface{})) {
	t.asyncTask = f
}

func (t *Timer) SetScheduler(scheduler *TaskScheduler) {
	t.scheduler = scheduler
}
