// Package timingwheel
// @Title  任务调度器
// @Description  desc
// @Author  yr  2025/1/13
// @Update  yr  2025/1/13
package timingwheel

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	inf "github.com/njtc406/emberengine/v2/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/v2/engine/pkg/log"
)

// ITimerScheduler 定时器调度器接口
// TODO 看看有无必要加上ctx，这个主要可能是用于传递一些调试参数
type ITimerScheduler interface {
	AfterFunc(d time.Duration, name string, f TimerCallback, args ...interface{}) (uint64, error)
	// AfterAsyncFunc 异步任务,执行函数是在独立的goroutine中执行
	AfterAsyncFunc(d time.Duration, name string, f TimerCallback, args ...interface{}) (uint64, error)

	TickerFunc(d time.Duration, name string, f TimerCallback, args ...interface{}) (uint64, error)
	// TickerAsyncFunc 异步任务,执行函数是在独立的goroutine中执行
	TickerAsyncFunc(d time.Duration, name string, f TimerCallback, args ...interface{}) (uint64, error)

	CronFunc(spec string, name string, f TimerCallback, args ...interface{}) (uint64, error)
	// CronAsyncFunc 异步任务,执行函数是在独立的goroutine中执行
	CronAsyncFunc(spec string, name string, f TimerCallback, args ...interface{}) (uint64, error)

	CancelTimer(taskId uint64)
	Stop()

	GetTimerCbChannel() <-chan ITimer
}

const defaultSeed uint64 = 10000

var ErrClosed = errors.New("timingwheel: closed")

// timerBucket 定时器桶，用于存储和管理定时器任务
type timerBucket struct {
	sync.Mutex
	tasks map[uint64]*Timer
}

// add 添加任务
func (b *timerBucket) add(timer *Timer) bool {
	b.Lock()
	defer b.Unlock()
	if _, ok := b.tasks[timer.timerId]; ok {
		return false
	}
	b.tasks[timer.timerId] = timer
	return true
}

func (b *timerBucket) remove(timerId uint64) *Timer {
	b.Lock()
	defer b.Unlock()
	if tm, ok := b.tasks[timerId]; ok {
		//fmt.Println("task remove")
		delete(b.tasks, timerId)
		return tm
	}

	return nil
}

type jobScheduler struct {
	closed int32
	addMu  sync.RWMutex
	shards []*timerBucket
	c      chan ITimer
	sendMu sync.RWMutex
	tw     *TimingWheel // 关联的timingwheel实例
	logger log.ILoggerX
}

// NewJobScheduler 创建一个新的任务调度器
// chanSize: 回调通道大小
// bucketSize: 桶数量，用于分片存储任务以提高并发性能
func NewJobScheduler(jobName string, chanSize, bucketSize int, tw inf.INodeTimingWheel, logger log.ILoggerX) (ITimerScheduler, error) {
	if chanSize <= 0 {
		chanSize = 100000
	}
	if bucketSize <= 0 {
		bucketSize = 10
	}
	var wheel *TimingWheel
	if tw != nil {
		if w, ok := tw.(*TimingWheel); ok {
			wheel = w
		} else {
			if logger != nil {
				logger.Errorf("timing wheel type invalid: %T", tw)
			}
			return nil, fmt.Errorf("timing wheel type invalid for scheduler[%s]: %T", jobName, tw)
		}
	}
	if wheel == nil {
		if logger != nil {
			logger.Errorf("timing wheel is nil, scheduler[%s] requires node-injected timing wheel", jobName)
		}
		return nil, fmt.Errorf("timing wheel is nil for scheduler[%s]", jobName)
	}
	shards := make([]*timerBucket, bucketSize)
	for i := range shards {
		shards[i] = &timerBucket{
			tasks: make(map[uint64]*Timer),
		}
	}
	return &jobScheduler{
		shards: shards,
		c:      make(chan ITimer, chanSize),
		tw:     wheel,
		logger: logger,
	}, nil
}

func (scheduler *jobScheduler) getShard(timerId uint64) *timerBucket {
	return scheduler.shards[timerId%uint64(len(scheduler.shards))]
}

func (scheduler *jobScheduler) add(t *Timer) bool {
	scheduler.addMu.RLock()
	defer scheduler.addMu.RUnlock()

	if scheduler.isClosed() {
		return false
	}

	if !t.isActive() {
		// 任务已经被取消
		return false
	}

	if !scheduler.getShard(t.timerId).add(t) {
		return false
	}

	return true
}

func (scheduler *jobScheduler) remove(taskId uint64) *Timer {
	shard := scheduler.getShard(taskId)
	if shard == nil {
		return nil
	}

	return shard.remove(taskId)
}

func (scheduler *jobScheduler) GetTimerCbChannel() <-chan ITimer {
	return scheduler.c
}

func (scheduler *jobScheduler) postTimer(t *Timer) bool {
	scheduler.sendMu.RLock()
	defer scheduler.sendMu.RUnlock()

	if scheduler.isClosed() {
		return false
	}

	select {
	case scheduler.c <- t:
		return true
	default:
		return false
	}
}

// isClosed 返回调度器是否已关闭
func (scheduler *jobScheduler) isClosed() bool {
	return atomic.LoadInt32(&scheduler.closed) == 1
}

// AfterFunc 延时任务
func (scheduler *jobScheduler) AfterFunc(d time.Duration, name string, f TimerCallback, args ...interface{}) (uint64, error) {
	// 创建task
	t := scheduler.createTimer()
	t.name = name
	t.task = f
	t.taskArgs = args
	t.taskScheduler = scheduler

	// 加入任务(先加入调度器,防止在timingwheel中执行时,调度器还未加入)
	return scheduler.register(t, d, false)
}

// AfterAsyncFunc 异步执行任务
func (scheduler *jobScheduler) AfterAsyncFunc(d time.Duration, name string, f TimerCallback, args ...interface{}) (uint64, error) {
	// 创建task
	t := scheduler.createTimer()
	t.name = name
	t.asyncTask = true
	t.task = f
	t.taskArgs = args
	t.taskScheduler = scheduler
	// 加入任务(先加入调度器,防止在timingwheel中执行时,调度器还未加入)
	return scheduler.register(t, d, false)
}

// TickerFunc 循环任务
func (scheduler *jobScheduler) TickerFunc(d time.Duration, name string, f TimerCallback, args ...interface{}) (uint64, error) {
	// 创建task
	t := scheduler.createTimer()
	t.name = name
	t.interval = d
	t.task = f
	t.taskArgs = args
	t.taskScheduler = scheduler

	return scheduler.register(t, d, true)
}

// TickerAsyncFunc 异步循环任务
func (scheduler *jobScheduler) TickerAsyncFunc(d time.Duration, name string, f TimerCallback, args ...interface{}) (uint64, error) {
	t := scheduler.createTimer()
	t.name = name
	t.interval = d
	t.asyncTask = true
	t.task = f
	t.taskArgs = args
	t.taskScheduler = scheduler

	return scheduler.register(t, d, true)
}

// CronFunc 循环任务,请注意,这个函数的精度只到秒
//
// spec: cron表达式 秒 分 时 日 月 周(可选) | @every 5s
// 示例: 0 */1 * * * 每分钟执行一次
// 示例: @every 5s 每5秒执行一次
func (scheduler *jobScheduler) CronFunc(spec string, name string, f TimerCallback, args ...interface{}) (uint64, error) {
	// 创建task
	t := scheduler.createTimer()
	t.name = name
	t.spec = spec
	t.isCron = true // 标记为cron定时器
	t.task = f
	t.taskArgs = args
	t.taskScheduler = scheduler

	// 加入任务
	return scheduler.register(t, 0, true)
}

// CronAsyncFunc 异步循环任务(任务不会被保存下来)
func (scheduler *jobScheduler) CronAsyncFunc(spec string, name string, f TimerCallback, args ...interface{}) (uint64, error) {
	t := scheduler.createTimer()
	t.name = name
	t.spec = spec
	t.isCron = true // 标记为cron定时器
	t.asyncTask = true
	t.task = f
	t.taskArgs = args
	t.taskScheduler = scheduler

	// 加入任务
	return scheduler.register(t, 0, true)
}

func (scheduler *jobScheduler) register(timer *Timer, delay time.Duration, periodic bool) (uint64, error) {
	scheduler.addMu.RLock()
	defer scheduler.addMu.RUnlock()
	if scheduler.isClosed() || scheduler.tw.IsClosed() {
		timer.stop()
		return 0, ErrClosed
	}
	if !scheduler.getShard(timer.timerId).add(timer) {
		timer.stop()
		return 0, fmt.Errorf("timer already registered")
	}
	if periodic {
		if err := scheduler.tw.ScheduleFunc(timer); err != nil {
			scheduler.CancelTimer(timer.timerId)
			return 0, err
		}
	} else {
		scheduler.tw.AfterFunc(delay, timer)
	}
	if scheduler.tw.IsClosed() {
		scheduler.CancelTimer(timer.timerId)
		return 0, ErrClosed
	}
	return timer.timerId, nil
}

func (scheduler *jobScheduler) CancelTimer(timerId uint64) {
	if timerId == 0 {
		return
	}
	t := scheduler.remove(timerId)
	if t == nil {
		return
	}
	t.stop()
	scheduler.tw.timers.Delete(t)
}

func (scheduler *jobScheduler) Stop() {
	scheduler.addMu.Lock()
	defer scheduler.addMu.Unlock()

	if scheduler.isClosed() {
		return
	}
	atomic.StoreInt32(&scheduler.closed, 1)

	var timers []*Timer
	for _, shard := range scheduler.shards {
		shard.Lock()
		for timerId, t := range shard.tasks {
			timers = append(timers, t)
			delete(shard.tasks, timerId)
		}
		shard.Unlock()
	}
	for _, t := range timers {
		t.stop()
		scheduler.tw.timers.Delete(t)
	}

	scheduler.sendMu.Lock()
	close(scheduler.c)
	for range scheduler.c {
	}
	scheduler.sendMu.Unlock()
}

func (scheduler *jobScheduler) createTimer() *Timer {
	t := &Timer{}
	t.timerId = scheduler.tw.genTimerId()
	return t
}
