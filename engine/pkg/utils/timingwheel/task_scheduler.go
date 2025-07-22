// Package timingwheel
// @Title  任务调度器
// @Description  desc
// @Author  yr  2025/1/13
// @Update  yr  2025/1/13
package timingwheel

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type ITimerScheduler interface {
	AfterFuncWithStorage(d time.Duration, name string, f TimerCallback, args ...interface{}) (uint64, error)
	AfterFunc(d time.Duration, name string, f TimerCallback, args ...interface{}) *Timer
	// AfterAsyncFunc 异步任务,执行函数是在独立的goroutine中执行
	AfterAsyncFunc(d time.Duration, name string, f func(...interface{}), args ...interface{}) *Timer

	TickerFuncWithStorage(d time.Duration, name string, f TimerCallback, args ...interface{}) (uint64, error)
	TickerFunc(d time.Duration, name string, f TimerCallback, args ...interface{}) *Timer
	// TickerAsyncFunc 异步任务,执行函数是在独立的goroutine中执行
	TickerAsyncFunc(d time.Duration, name string, f func(...interface{}), args ...interface{}) *Timer

	CronFuncWithStorage(spec string, name string, f TimerCallback, args ...interface{}) (uint64, error)
	CronFunc(spec string, name string, f TimerCallback, args ...interface{}) *Timer
	// CronAsyncFunc 异步任务,执行函数是在独立的goroutine中执行
	CronAsyncFunc(spec string, name string, f func(...interface{}), args ...interface{}) *Timer

	CancelTimer(taskId uint64) bool
	Stop()

	GetTimerCbChannel() chan ITimer
}

var (
	defaultSeed uint64 = 10000
)

type TimerBucket struct {
	sync.Mutex
	tasks map[uint64]*Timer
}

// add 添加任务
func (b *TimerBucket) add(timer *Timer) bool {
	b.Lock()
	defer b.Unlock()
	if _, ok := b.tasks[timer.timerId]; ok {
		return false
	}
	b.tasks[timer.timerId] = timer
	return true
}

func (b *TimerBucket) remove(timerId uint64) *Timer {
	b.Lock()
	defer b.Unlock()
	if tm, ok := b.tasks[timerId]; ok {
		//fmt.Println("task remove")
		delete(b.tasks, timerId)
		return tm
	}

	return nil
}

type TaskScheduler struct {
	closed int32
	shards []*TimerBucket

	c chan ITimer
}

func NewTaskScheduler(chanSize, bucketSize int) ITimerScheduler {
	if chanSize <= 0 {
		chanSize = 100000
	}
	if bucketSize <= 0 {
		bucketSize = 10
	}
	shards := make([]*TimerBucket, bucketSize)
	for i := range shards {
		shards[i] = &TimerBucket{
			tasks: make(map[uint64]*Timer),
		}
	}
	return &TaskScheduler{
		shards: shards,
		c:      make(chan ITimer, chanSize),
	}
}

func (scheduler *TaskScheduler) getShard(timerId uint64) *TimerBucket {
	return scheduler.shards[timerId%uint64(len(scheduler.shards))]
}

func (scheduler *TaskScheduler) add(t *Timer) bool {
	timerBucket := scheduler.getShard(t.timerId)
	if timerBucket == nil {
		fmt.Println("bucket is nil")
		return false
	}

	if !t.isActive() {
		fmt.Println("task is not active")
		// 任务已经被取消
		return false
	}

	if !timerBucket.add(t) {
		fmt.Println("task had add")
		return false
	}

	return true
}

func (scheduler *TaskScheduler) remove(taskId uint64) *Timer {
	shard := scheduler.getShard(taskId)
	if shard == nil {
		return nil
	}

	return shard.remove(taskId)
}

func (scheduler *TaskScheduler) GetTimerCbChannel() chan ITimer {
	return scheduler.c
}

// AfterFuncWithStorage 延时任务(任务会被保存下来)
func (scheduler *TaskScheduler) AfterFuncWithStorage(d time.Duration, name string, f TimerCallback, args ...interface{}) (uint64, error) {
	// 创建task
	tm := tw.AfterFunc(d, func(t *Timer) {
		t.name = name
		t.task = f
		t.taskArgs = args
		t.c = scheduler.c
		t.scheduler = scheduler
	})
	// 加入任务
	if !scheduler.add(tm) {
		return 0, fmt.Errorf("after task add failed")
	}

	return tm.timerId, nil
}

// AfterFunc 添加任务(任务不会被保存下来)
func (scheduler *TaskScheduler) AfterFunc(d time.Duration, name string, f TimerCallback, args ...interface{}) *Timer {
	// 创建task
	return tw.AfterFunc(d, func(t *Timer) {
		t.name = name
		t.task = f
		t.taskArgs = args
		t.c = scheduler.c
	})
}

// AfterAsyncFunc 异步执行任务(任务不会被保存下来)
func (scheduler *TaskScheduler) AfterAsyncFunc(d time.Duration, name string, f func(...interface{}), args ...interface{}) *Timer {
	// 创建task
	return tw.AfterFunc(d, func(t *Timer) {
		t.name = name
		t.asyncTask = f
		t.taskArgs = args
		t.c = scheduler.c
	})
}

// TickerFuncWithStorage 循环任务(任务会被保存下来)
func (scheduler *TaskScheduler) TickerFuncWithStorage(d time.Duration, name string, f TimerCallback, args ...interface{}) (uint64, error) {
	// 创建task
	tm := tw.ScheduleFunc(func(t *Timer) {
		t.name = name
		t.interval = d
		t.task = f
		t.taskArgs = args
		t.c = scheduler.c
		t.scheduler = scheduler
	})
	if tm == nil {
		return 0, fmt.Errorf("ticker task create failed")
	}
	// 加入任务
	if !scheduler.add(tm) {
		return 0, fmt.Errorf("ticker task add failed")
	}

	return tm.timerId, nil
}

// TickerFunc 循环任务(任务不会被保存下来)
func (scheduler *TaskScheduler) TickerFunc(d time.Duration, name string, f TimerCallback, args ...interface{}) *Timer {
	// 创建task
	return tw.ScheduleFunc(func(t *Timer) {
		t.name = name
		t.interval = d
		t.task = f
		t.taskArgs = args
		t.c = scheduler.c
	})
}

// TickerAsyncFunc 异步循环任务(任务不会被保存下来)
func (scheduler *TaskScheduler) TickerAsyncFunc(d time.Duration, name string, f func(...interface{}), args ...interface{}) *Timer {
	// 创建task
	return tw.ScheduleFunc(func(t *Timer) {
		t.name = name
		t.interval = d
		t.asyncTask = f
		t.taskArgs = args
		t.c = scheduler.c
	})
}

// CronFuncWithStorage 循环任务(任务会被保存下来),请注意,这个函数的精度只到秒
//
// spec: cron表达式 秒 分 时 日 月 周(可选) | @every 5s
// 示例: 0 */1 * * * 每分钟执行一次
// 示例: @every 5s 每5秒执行一次
func (scheduler *TaskScheduler) CronFuncWithStorage(spec string, name string, f TimerCallback, args ...interface{}) (uint64, error) {
	// 创建task
	tm := tw.ScheduleFunc(func(t *Timer) {
		t.name = name
		t.spec = spec
		t.task = f
		t.taskArgs = args
		t.c = scheduler.c
		t.scheduler = scheduler
	})
	if tm == nil {
		return 0, fmt.Errorf("cron task create failed")
	}
	// 加入任务
	if !scheduler.add(tm) {
		return 0, fmt.Errorf("cron task add failed")
	}

	return tm.timerId, nil
}

func (scheduler *TaskScheduler) CronFunc(spec string, name string, f TimerCallback, args ...interface{}) *Timer {
	// 创建task
	return tw.ScheduleFunc(func(t *Timer) {
		t.name = name
		t.spec = spec
		t.task = f
		t.taskArgs = args
		t.c = scheduler.c
	})
}

// CronAsyncFunc 异步循环任务(任务不会被保存下来)
func (scheduler *TaskScheduler) CronAsyncFunc(spec string, name string, f func(...interface{}), args ...interface{}) *Timer {
	// 创建task
	return tw.ScheduleFunc(func(t *Timer) {
		t.name = name
		t.spec = spec
		t.asyncTask = f
		t.taskArgs = args
		t.c = scheduler.c
	})
}

func (scheduler *TaskScheduler) CancelTimer(taskId uint64) bool {
	if taskId == 0 {
		return true
	}
	task := scheduler.remove(taskId)
	if task == nil {
		return true
	}
	ok := task.Stop()
	releaseTimer(task)
	return ok
}

func (scheduler *TaskScheduler) Stop() {
	atomic.StoreInt32(&scheduler.closed, 1)
	for _, shard := range scheduler.shards {
		shard.Lock()
		for timerId, task := range shard.tasks {
			task.Stop()
			delete(shard.tasks, timerId)
		}
		shard.Unlock()
	}
	close(scheduler.c)
}
