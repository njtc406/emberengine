// Package timingwheel
// 模块名: timingwheel
// 功能描述: 高性能分层时间轮实现，提供并发安全的定时任务调度
// 作者:  yr
// 最后更新:  2025
package timingwheel

import (
	"container/list"
	"context"
	"errors"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	"github.com/njtc406/emberengine/engine/pkg/utils/safe"
)

type ITimer interface {
	Do(ctx context.Context) error
	GetName() string
	GetTimerId() uint64
}

type TimerOption func(t *Timer)
type TimerCallback func(ctx context.Context, timer *Timer, args ...interface{}) error

// Timer 表示一个定时事件。当 Timer 到期时，会执行对应的任务。
type Timer struct {
	dto.DataRef
	Scheduler
	timerId    uint64         // 任务唯一id
	generation atomic.Uint64  // Timer版本号，每次从池中获取时递增，用于防止ABA问题
	snapGen    atomic.Uint64  // 快照版本号，在addOrRun中冻结，用于验证
	expiration atomic.Int64   // in milliseconds 任务到期时间
	cancel     atomic.Bool    // 任务是否已经取消
	executing  atomic.Bool    // 标记是否正在执行
	execWg     sync.WaitGroup // 等待执行完成

	// 保存该定时器所属的 bucket 的指针。
	// 注意：该字段可能被并发更新和读取（通过 Timer.Stop() 和 Bucket.Flush()）。
	b unsafe.Pointer // type: *bucket

	// The timer's element.
	element *list.Element

	// 以下字段需要在Timer创建初始化时设置,执行期间只读,因此是并发安全的
	name          string               // 任务名称
	interval      time.Duration        // 间隔时间 > 0 表示循环执行
	spec          string               // cron表达式
	isCron        bool                 // 是否为cron定时器(用于时间调整时的特殊处理)
	task          TimerCallback        // 任务
	taskArgs      []interface{}        // 任务参数
	loop          func()               // 循环执行
	asyncTask     func(...interface{}) // 异步任务
	taskScheduler ITimerScheduler      // 任务调度器
}

func (t *Timer) Reset() {
	// 等待执行完成（使用WaitGroup，避免忙等待）
	t.execWg.Wait()

	// 递增版本号，使得旧的引用失效
	t.generation.Add(1)

	// 安全地将 timer 从所属 bucket 中移除（通过 b.mu 保护 element 的读写）
	for b := t.getBucket(); b != nil; b = t.getBucket() {
		b.Remove(t)
	}

	t.name = ""
	t.timerId = 0
	t.expiration.Store(0)
	t.interval = 0
	t.spec = ""
	t.isCron = false
	t.cancel.Store(false)
	t.executing.Store(false)
	t.task = nil
	t.taskArgs = nil
	t.taskScheduler = nil
	t.loop = nil
	t.asyncTask = nil
}

func (t *Timer) GetName() string {
	if !t.isActive() {
		return ""
	}
	// 并发安全地读取字段，防止与 Reset() 产生 data race。
	// 场景1: 在 Do() 回调内调用 → executing 已经是 true，Reset 会被 execWg.Wait 阻塞，字段安全。
	// 场景2: 外部调用 → 通过 CAS 短暂持有 executing 锁来保护字段读取。
	needRelease := false
	if !t.executing.Load() {
		// 外部调用，尝试 CAS 保护
		if !t.executing.CompareAndSwap(false, true) {
			// CAS 失败说明正在 Reset 或其他操作中
			return ""
		}
		needRelease = true
	}
	// 此时 executing=true，Reset 不会修改字段
	name := t.name
	task := t.task
	asyncTask := t.asyncTask
	if needRelease {
		t.executing.Store(false)
	}

	if name != "" {
		return name
	}
	if task != nil {
		return runtime.FuncForPC(reflect.ValueOf(task).Pointer()).Name()
	}
	if asyncTask != nil {
		return runtime.FuncForPC(reflect.ValueOf(asyncTask).Pointer()).Name()
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
	if !t.IsRef() || !t.cancel.CompareAndSwap(false, true) {
		return false
	}

	// 不再等待 execWg：stop() 可能从 Do() 回调链中被调用（如 CancelTimer），
	// 此时等待 execWg 会导致死锁。cancel 标志已设置，Do() 会在后续检查中发现
	// 并提前返回。Reset()（池回收时）仍会等待 execWg 确保安全。

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

func (t *Timer) Do(ctx context.Context) (err error) {
	// 检查是否正在执行
	if !t.executing.CompareAndSwap(false, true) {
		// 已经在执行中，不应该发生
		err = def.ErrRepeatExecute
		return
	}

	// 标记正在执行，使用WaitGroup追踪，确保执行完成后释放
	t.execWg.Add(1)
	defer func() {
		t.executing.Store(false)
		t.execWg.Done()
		// 不是循环任务, 执行完成后停止（循环任务会在timingwheel的addorrun弹出时就重新添加）
		if t.loop == nil && !errors.Is(err, def.ErrTimerReuse) && t.taskScheduler != nil { // timer被复用时不能停止
			t.taskScheduler.CancelTimer(t.timerId)
		}
	}()

	if !t.isActive() {
		// 任务取消
		return nil
	}

	// 对比锁定版本和当前版本，防止ABA问题
	if t.snapGen.Load() != t.generation.Load() {
		// 版本号不匹配，说明Timer已被回收并复用
		err = def.ErrTimerReuse
		return
	}

	// 开始执行回调任务
	err = safe.Do(func() error {
		err := t.task(ctx, t, t.taskArgs...)
		if err != nil {
			return err
		}
		return nil
	})
	// 记录执行时间
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

func (t *Timer) SetTaskScheduler(scheduler ITimerScheduler) {
	t.taskScheduler = scheduler
}

func (t *Timer) SetAsyncTask(f func(...interface{})) {
	t.asyncTask = f
}
