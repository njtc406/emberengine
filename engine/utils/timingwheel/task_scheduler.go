// Package timingwheel
// @Title  任务调度器
// @Description  desc
// @Author  yr  2025/1/13
// @Update  yr  2025/1/13
package timingwheel

import (
	"fmt"
	"github.com/njtc406/emberengine/engine/inf"
	"sync"
	"time"
)

var (
	defaultSeed uint64 = 10000
)

type TimerOptFun func(job *Timer)

type TimerShard struct {
	sync.Mutex
	tasks map[uint64]*Timer
}

// add 添加任务
func (shard *TimerShard) add(timer *Timer) bool {
	shard.Lock()
	defer shard.Unlock()
	if _, ok := shard.tasks[timer.timerId]; ok {
		return false
	}
	shard.tasks[timer.timerId] = timer
	return true
}

func (shard *TimerShard) remove(timerId uint64) *Timer {
	shard.Lock()
	defer shard.Unlock()
	if tm, ok := shard.tasks[timerId]; ok {
		//fmt.Println("task remove")
		delete(shard.tasks, timerId)
		return tm
	}

	return nil
}

type TaskScheduler struct {
	shards []*TimerShard

	C chan inf.ITimer
}

func NewTaskScheduler(chanSize, shardCount int) *TaskScheduler {
	shards := make([]*TimerShard, shardCount)
	for i := range shards {
		shards[i] = &TimerShard{
			tasks: make(map[uint64]*Timer),
		}
	}
	return &TaskScheduler{
		shards: shards,
		C:      make(chan inf.ITimer, chanSize),
	}
}

func (scheduler *TaskScheduler) getShard(timerId uint64) *TimerShard {
	return scheduler.shards[timerId%uint64(len(scheduler.shards))]
}

func (scheduler *TaskScheduler) add(t *Timer) bool {
	shard := scheduler.getShard(t.timerId)
	if shard == nil {
		fmt.Println("shard is nil")
		return false
	}

	if !t.isActive() {
		fmt.Println("task is not active")
		// 任务已经被取消
		return false
	}

	if !shard.add(t) {
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

// AfterFuncWithStorage 延时任务(任务会被保存下来)
func (scheduler *TaskScheduler) AfterFuncWithStorage(d time.Duration, f func(uint64, ...interface{}), onAddTask TimerOption, onDelTask TimerOption, args ...interface{}) (uint64, error) {
	// 创建task
	tm := tw.AfterFunc(d, func(t *Timer) {
		t.onTimerAdd = onAddTask
		t.onTimerDel = onDelTask
		t.task = f
		t.taskArgs = args
		t.c = scheduler.C
		t.scheduler = scheduler
	})
	// 加入任务
	if !scheduler.add(tm) {
		return 0, fmt.Errorf("after task add failed")
	}

	return tm.timerId, nil
}

// AfterFunc 添加任务(任务不会被保存下来)
func (scheduler *TaskScheduler) AfterFunc(d time.Duration, f func(uint64, ...interface{}), onAddTask TimerOption, onDelTask TimerOption, args ...interface{}) *Timer {
	// 创建task
	return tw.AfterFunc(d, func(t *Timer) {
		t.onTimerAdd = onAddTask
		t.onTimerDel = onDelTask
		t.task = f
		t.taskArgs = args
		t.c = scheduler.C
	})
}

// TickerFuncWithStorage 循环任务(任务会被保存下来)
func (scheduler *TaskScheduler) TickerFuncWithStorage(d time.Duration, f func(uint64, ...interface{}), onAddTask TimerOption, onDelTask TimerOption, args ...interface{}) (uint64, error) {
	// 创建task
	tm := tw.ScheduleFunc(func(t *Timer) {
		t.interval = d
		t.onTimerAdd = onAddTask
		t.onTimerDel = onDelTask
		t.task = f
		t.taskArgs = args
		t.c = scheduler.C
		t.scheduler = scheduler
	})
	// 加入任务
	if !scheduler.add(tm) {
		return 0, fmt.Errorf("ticker task add failed")
	}

	return tm.timerId, nil
}

// TickerFunc 循环任务(任务不会被保存下来)
func (scheduler *TaskScheduler) TickerFunc(d time.Duration, f func(uint64, ...interface{}), onAddTask TimerOption, onDelTask TimerOption, args ...interface{}) *Timer {
	// 创建task
	return tw.ScheduleFunc(func(t *Timer) {
		t.interval = d
		t.onTimerAdd = onAddTask
		t.onTimerDel = onDelTask
		t.task = f
		t.taskArgs = args
		t.c = scheduler.C
	})
}

// CronFuncWithStorage 循环任务(任务会被保存下来),请注意,这个函数的精度只到秒
//
// spec: cron表达式 秒 分 时 日 月 周(可选) | @every 5s
// 示例: 0 */1 * * * 每分钟执行一次
// 示例: @every 5s 每5秒执行一次
func (scheduler *TaskScheduler) CronFuncWithStorage(spec string, f func(uint64, ...interface{}), onAddTask TimerOption, onDelTask TimerOption, args ...interface{}) (uint64, error) {
	// 创建task
	tm := tw.ScheduleFunc(func(t *Timer) {
		t.spec = spec
		t.onTimerAdd = onAddTask
		t.onTimerDel = onDelTask
		t.task = f
		t.taskArgs = args
		t.c = scheduler.C
		t.scheduler = scheduler
	})
	// 加入任务
	if !scheduler.add(tm) {
		return 0, fmt.Errorf("cron task add failed")
	}

	return tm.timerId, nil
}

func (scheduler *TaskScheduler) CronFunc(spec string, f func(uint64, ...interface{}), onAddTask TimerOption, onDelTask TimerOption, args ...interface{}) *Timer {
	// 创建task
	// 创建task
	return tw.ScheduleFunc(func(t *Timer) {
		t.spec = spec
		t.onTimerAdd = onAddTask
		t.onTimerDel = onDelTask
		t.task = f
		t.taskArgs = args
		t.c = scheduler.C
	})
}

func (scheduler *TaskScheduler) Cancel(taskId uint64) bool {
	task := scheduler.remove(taskId)
	if task == nil {
		return true
	}

	return task.Stop()
}
