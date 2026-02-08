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

	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
)

// ITimerScheduler 定时器调度器接口
// TODO 看看有无必要加上ctx，这个主要可能是用于传递一些调试参数
type ITimerScheduler interface {
	AfterFunc(d time.Duration, name string, f TimerCallback, args ...interface{}) (uint64, error)
	// AfterAsyncFunc 异步任务,执行函数是在独立的goroutine中执行
	AfterAsyncFunc(d time.Duration, name string, f func(...interface{}), args ...interface{}) (uint64, error)

	TickerFunc(d time.Duration, name string, f TimerCallback, args ...interface{}) (uint64, error)
	// TickerAsyncFunc 异步任务,执行函数是在独立的goroutine中执行
	TickerAsyncFunc(d time.Duration, name string, f func(...interface{}), args ...interface{}) (uint64, error)

	CronFunc(spec string, name string, f TimerCallback, args ...interface{}) (uint64, error)
	// CronAsyncFunc 异步任务,执行函数是在独立的goroutine中执行
	CronAsyncFunc(spec string, name string, f func(...interface{}), args ...interface{}) (uint64, error)

	CancelTimer(taskId uint64)
	Stop()

	GetTimerCbChannel() chan ITimer
}

var (
	defaultSeed uint64 = 10000
)

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
	closed    int32
	shards    []*timerBucket
	c         chan ITimer
	tw        *TimingWheel // 关联的timingwheel实例
	timerPool pool.IPool[*Timer]
	logger    log.ILoggerX
}

// NewJobScheduler 创建一个新的任务调度器
// chanSize: 回调通道大小
// bucketSize: 桶数量，用于分片存储任务以提高并发性能
func NewJobScheduler(jobName string, chanSize, bucketSize int, tw *TimingWheel, logger log.ILoggerX, isDebug bool) ITimerScheduler {
	if logger == nil {
		l, err := log.NewDefaultLogger(nil)
		if err != nil {
			panic(fmt.Sprintf("create logger failed: %v", err))
		}
		logger = log.NewLoggerX(l, log.Fields{"name": jobName})
	}
	if chanSize <= 0 {
		chanSize = 100000
	}
	if bucketSize <= 0 {
		bucketSize = 10
	}
	if tw == nil {
		tw = globTW
	}
	if tw == nil {
		logger.Panic("timing wheel is nil")
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
		tw:     tw,
		timerPool: pool.NewSyncPoolWrapper(
			func() *Timer {
				return &Timer{}
			},
			func() pool.IStatsRecorder {
				if isDebug {
					return pool.NewStatsRecorder("timerPool")
				} else {
					return pool.NewNoStatsRecorder()
				}
			}(),
			pool.WithRef(func(t *Timer) {
				t.Ref()
			}),
			pool.WithUnRef(func(t *Timer) bool {
				return t.UnRef()
			}),
			pool.WithReset(func(t *Timer) {
				t.Reset()
			}),
		),
	}
}

func (scheduler *jobScheduler) getShard(timerId uint64) *timerBucket {
	return scheduler.shards[timerId%uint64(len(scheduler.shards))]
}

func (scheduler *jobScheduler) add(t *Timer) bool {
	// P1#6: 调度器已关闭，拒绝添加
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

func (scheduler *jobScheduler) GetTimerCbChannel() chan ITimer {
	return scheduler.c
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
	if !scheduler.add(t) {
		scheduler.releaseTimer(t)
		return 0, fmt.Errorf("after task add failed")
	}

	scheduler.tw.AfterFunc(d, t)

	return t.GetTimerId(), nil
}

// AfterAsyncFunc 异步执行任务
func (scheduler *jobScheduler) AfterAsyncFunc(d time.Duration, name string, f func(...interface{}), args ...interface{}) (uint64, error) {
	// 创建task
	t := scheduler.createTimer()
	t.name = name
	t.asyncTask = f
	t.taskArgs = args
	t.taskScheduler = scheduler
	// 加入任务(先加入调度器,防止在timingwheel中执行时,调度器还未加入)
	if !scheduler.add(t) {
		scheduler.releaseTimer(t)
		return 0, fmt.Errorf("after async task add failed")
	}
	scheduler.tw.AfterFunc(d, t)
	return t.GetTimerId(), nil
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

	if !scheduler.add(t) {
		scheduler.releaseTimer(t)
		return 0, fmt.Errorf("ticker task add failed")
	}

	if err := scheduler.tw.ScheduleFunc(t); err != nil {
		return 0, err
	}
	// 加入任务

	return t.GetTimerId(), nil
}

// TickerAsyncFunc 异步循环任务
func (scheduler *jobScheduler) TickerAsyncFunc(d time.Duration, name string, f func(...interface{}), args ...interface{}) (uint64, error) {
	t := scheduler.createTimer()
	t.name = name
	t.interval = d
	t.asyncTask = f
	t.taskArgs = args
	t.taskScheduler = scheduler

	if !scheduler.add(t) {
		scheduler.releaseTimer(t)
		return 0, fmt.Errorf("ticker async task add failed")
	}
	// 加入任务
	if err := scheduler.tw.ScheduleFunc(t); err != nil {
		return 0, err
	}
	return t.GetTimerId(), nil
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
	if !scheduler.add(t) {
		scheduler.releaseTimer(t)
		return 0, fmt.Errorf("cron task add failed")
	}
	if err := scheduler.tw.ScheduleFunc(t); err != nil {
		return 0, err
	}
	return t.GetTimerId(), nil
}

// CronAsyncFunc 异步循环任务(任务不会被保存下来)
func (scheduler *jobScheduler) CronAsyncFunc(spec string, name string, f func(...interface{}), args ...interface{}) (uint64, error) {
	t := scheduler.createTimer()
	t.name = name
	t.spec = spec
	t.isCron = true // 标记为cron定时器
	t.asyncTask = f
	t.taskArgs = args
	t.taskScheduler = scheduler

	// 加入任务
	if !scheduler.add(t) {
		scheduler.releaseTimer(t)
		return 0, fmt.Errorf("cron async task add failed")
	}
	// 创建task
	if err := scheduler.tw.ScheduleFunc(t); err != nil {
		return 0, err
	}
	return t.GetTimerId(), nil
}

func (scheduler *jobScheduler) CancelTimer(timerId uint64) {
	if timerId == 0 {
		return
	}
	t := scheduler.remove(timerId)
	if t == nil {
		return
	}
	scheduler.releaseTimer(t)
}

func (scheduler *jobScheduler) Stop() {
	atomic.StoreInt32(&scheduler.closed, 1)

	// 先在锁内收集所有 timer，再在锁外释放，避免 releaseTimer → stop → execWg.Wait
	// 与正在执行的 Do() → CancelTimer → shard.Lock 形成死锁。
	var toRelease []*Timer
	for _, shard := range scheduler.shards {
		shard.Lock()
		for timerId, t := range shard.tasks {
			toRelease = append(toRelease, t)
			delete(shard.tasks, timerId)
		}
		shard.Unlock()
	}

	for _, t := range toRelease {
		scheduler.releaseTimer(t)
	}

	close(scheduler.c)
}

func (scheduler *jobScheduler) createTimer() *Timer {
	t := scheduler.timerPool.Get()
	t.SetTimerId(scheduler.tw.genTimerId())
	return t
}

func (scheduler *jobScheduler) releaseTimer(t *Timer) {
	if t.IsRef() {
		// 防止重复释放
		t.stop()
		// 异步放回池中：pool.Put → Reset → execWg.Wait 可能阻塞（如 runTimer 正在
		// 快照字段），若在 Do() 回调链中同步调用会导致死锁。异步化后不阻塞调用方。
		go func() {
			scheduler.timerPool.Put(t)
		}()
	}
}
