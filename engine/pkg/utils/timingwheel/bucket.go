package timingwheel

import (
	"container/list"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/njtc406/emberengine/engine/pkg/dto"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
	"github.com/njtc406/emberengine/engine/pkg/utils/timelib"
)

type ITimer interface {
	Do()
	GetName() string
	GetTimerId() uint64
}

var timerPool = pool.NewPoolEx(make(chan pool.IPoolData, 100000), func() pool.IPoolData {
	return &Timer{}
})

func createTimer() *Timer {
	return timerPool.Get().(*Timer)
}

func releaseTimer(t *Timer) {
	timerPool.Put(t)
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
	expiration int64                // in milliseconds 任务到期时间
	interval   time.Duration        // 间隔时间 > 0 表示循环执行
	spec       string               // cron表达式
	cancel     int32                // 0未取消 1取消
	task       TimerCallback        // 任务
	taskArgs   []interface{}        // 任务参数
	c          chan ITimer          // service直接触发通道
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
	t.expiration = 0
	t.interval = 0
	t.spec = ""
	t.cancel = 0
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
	if t.IsRef() {
		atomic.StoreInt32(&t.cancel, 1)
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
	return atomic.LoadInt32(&t.cancel) == 0
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

	if t.IsRef() {
		releaseTimer(t)
	}
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
	atomic.StoreInt64(&t.expiration, expiration)
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

type bucket struct {
	// 64-bit atomic operations require 64-bit alignment, but 32-bit
	// compilers do not ensure it. So we must keep the 64-bit field
	// as the first field of the struct.
	//
	// For more explanations, see https://golang.org/pkg/sync/atomic/#pkg-note-BUG
	// and https://go101.org/article/memory-layout.html.
	expiration int64

	mu     sync.Mutex
	timers *list.List
}

func newBucket() *bucket {
	return &bucket{
		timers:     list.New(),
		expiration: -1,
	}
}

func (b *bucket) Expiration() int64 {
	return atomic.LoadInt64(&b.expiration)
}

func (b *bucket) SetExpiration(expiration int64) bool {
	return atomic.SwapInt64(&b.expiration, expiration) != expiration
}

func (b *bucket) Add(t *Timer) {
	b.mu.Lock()

	e := b.timers.PushBack(t)
	t.setBucket(b)
	t.element = e

	b.mu.Unlock()
}

func (b *bucket) remove(t *Timer) bool {
	if t.getBucket() != b {
		// If remove is called from within t.Stop, and this happens just after the timing wheel's goroutine has:
		//     1. removed t from b (through b.Flush -> b.remove)
		//     2. moved t from b to another bucket ab (through b.Flush -> b.remove and ab.Add)
		// then t.getBucket will return nil for case 1, or ab (non-nil) for case 2.
		// In either case, the returned value does not equal to b.
		return false
	}
	b.timers.Remove(t.element)
	t.setBucket(nil)
	t.element = nil
	return true
}

func (b *bucket) Remove(t *Timer) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.remove(t)
}

func (b *bucket) Flush(reinsert func(*Timer)) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for e := b.timers.Front(); e != nil; {
		next := e.Next()

		t := e.Value.(*Timer)
		b.remove(t)
		// Note that this operation will either execute the timer's task, or
		// insert the timer into another bucket belonging to a lower-level wheel.
		//
		// In either case, no further lock operation will happen to b.mu.
		if reinsert != nil {
			reinsert(t)
		}

		e = next
	}

	b.SetExpiration(-1)
}
