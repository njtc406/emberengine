// Package timingwheel
// 模块名: timingwheel
// 功能描述: 高性能分层时间轮实现，提供并发安全的定时任务调度
// 作者:  yr
// 最后更新:  2025
package timingwheel

import (
	"container/list"
	"context"
	"reflect"
	"runtime"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/njtc406/emberengine/engine/pkg/utils/safe"
)

type ITimer interface {
	Do(ctx context.Context) error
	GetName() string
	GetTimerId() uint64
}

type TimerCallback func(ctx context.Context, timer *Timer, args ...interface{}) error

// Timer 表示一个定时事件。当 Timer 到期时，会执行对应的任务。
type Timer struct {
	timerId    uint64       // 任务唯一id
	expiration atomic.Int64 // in milliseconds 任务到期时间
	cancel     atomic.Bool  // 任务是否已经取消

	// 保存该定时器所属的 bucket 的指针。
	// 注意：该字段可能被并发更新和读取（通过 Timer.Stop() 和 Bucket.Flush()）。
	b unsafe.Pointer // type: *bucket

	// The timer's element.
	element *list.Element

	// 以下字段需要在Timer创建初始化时设置,执行期间只读,因此是并发安全的
	name          string          // 任务名称
	interval      time.Duration   // 间隔时间 > 0 表示循环执行
	spec          string          // cron表达式
	isCron        bool            // 是否为cron定时器(用于时间调整时的特殊处理)
	task          TimerCallback   // 任务
	taskArgs      []interface{}   // 任务参数
	loop          func()          // 循环执行
	asyncTask     bool            // 异步任务
	taskScheduler ITimerScheduler // 任务调度器
}

func (t *Timer) GetName() string {
	if t.name != "" {
		return t.name
	}
	if t.task != nil {
		return runtime.FuncForPC(reflect.ValueOf(t.task).Pointer()).Name()
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
// Stop will wait for any ongoing task execution to complete before returning.
func (t *Timer) Stop() {
	if t.taskScheduler == nil {
		return
	}
	t.taskScheduler.CancelTimer(t.timerId)
}

func (t *Timer) stop() bool {
	if !t.cancel.CompareAndSwap(false, true) {
		return false
	}

	stopped := false
	for b := t.getBucket(); b != nil; b = t.getBucket() {
		stopped = b.Remove(t)
	}

	return stopped
}

func (t *Timer) isActive() bool {
	return !t.cancel.Load()
}

func (t *Timer) Do(ctx context.Context) (err error) {
	if !t.isActive() {
		// 任务已取消
		return nil
	}

	// 开始执行回调任务
	err = safe.Do(func() error {
		err := t.task(ctx, t, t.taskArgs...)
		if err != nil {
			return err
		}
		return nil
	})

	// 不是循环任务, 执行完成后停止
	if t.loop == nil && t.taskScheduler != nil {
		t.taskScheduler.CancelTimer(t.timerId)
	}

	return
}

func (t *Timer) Next(tm time.Time) time.Time {
	if t.interval > 0 {
		// 基于传入的时间计算下一次执行时间
		return tm.Add(t.interval)
	}

	if t.spec != "" {
		sd, err := cronParser.Parse(t.spec)
		if err != nil {
			return time.Time{}
		}
		return sd.Next(tm)
	}

	return time.Time{}
}

func (t *Timer) GetExpiration() int64 {
	return t.expiration.Load()
}
