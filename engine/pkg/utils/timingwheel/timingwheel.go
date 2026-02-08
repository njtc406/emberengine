package timingwheel

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel/delayqueue"
)

// TimingWheel 是分层时间轮的实现。
type TimingWheel struct {
	tick      int64 // 毫秒为单位的刻度
	wheelSize int64

	interval    int64 // 毫秒为单位的轮盘周期
	currentTime int64 // 毫秒为单位的当前时间
	buckets     []*bucket
	queue       *delayqueue.DelayQueue

	timerIdSeed uint64

	// 上层的溢出时间轮。
	// 注意：此字段可被并发读写（通过 Add()），因此使用原子指针。
	overflowWheel unsafe.Pointer // type: *TimingWheel

	exitC     chan struct{}
	closed    *atomic.Bool
	waitGroup waitGroupWrapper

	logger log.ILoggerX

	// timeOffsetNs 表示以纳秒为单位的时间偏移, 应用于所有时间计算
	// 使用 atomic.Int64 以支持高并发场景下的无锁读写
	timeOffsetNs atomic.Int64

	// adjustMu 保护时间偏移调整操作
	adjusting     *atomic.Bool  // 是否正在调整时间偏移（所有层级共享顶层指针）
	pendingTimers chan *Timer   // 时间调整期间的待处理 timer 缓冲队列（所有层级共享顶层 channel）
	adjustDone    chan struct{} // adjusting 结束时广播通知，替代 busy-wait
	adjustMu      sync.Mutex    // 保护整个调整过程，确保串行执行
}

// NewTimingWheel 使用指定的刻度和轮大小创建一个 TimingWheel 实例。
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

	startMs := timeToMs(time.Now())

	return newTimingWheel(
		tickMs,
		wheelSize,
		startMs,
		delayqueue.New(int(wheelSize)),
		logger,
		new(atomic.Bool),
	)
}

// newTimingWheel 是内部辅助函数，用于真正创建 TimingWheel 实例。
func newTimingWheel(tickMs int64, wheelSize int64, startMs int64, queue *delayqueue.DelayQueue, logger log.ILoggerX, closed *atomic.Bool) *TimingWheel {
	buckets := make([]*bucket, wheelSize)
	for i := range buckets {
		buckets[i] = newBucket()
	}
	adjustingFlag := new(atomic.Bool)
	pendingCh := make(chan *Timer, 10000)
	adjustDoneCh := make(chan struct{})
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
		adjusting:     adjustingFlag,
		pendingTimers: pendingCh,
		adjustDone:    adjustDoneCh,
		// timeOffsetNs 默认为0，无需显式初始化
	}
}

// newOverflowWheel 创建 overflow wheel，共享父层的 adjusting/pendingTimers/adjustDone，
// 避免 overflow wheel 的 add() 绕过顶层的 adjusting 保护。
func newOverflowWheel(tickMs int64, wheelSize int64, startMs int64, queue *delayqueue.DelayQueue, logger log.ILoggerX, closed *atomic.Bool, adjusting *atomic.Bool, pendingTimers chan *Timer, adjustDone chan struct{}) *TimingWheel {
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
		adjusting:     adjusting,
		pendingTimers: pendingTimers,
		adjustDone:    adjustDone,
	}
}

// add 将定时器 t 插入到当前时间轮。
func (tw *TimingWheel) add(t *Timer) bool {
	// 如果timingwheel已关闭，不能添加任务
	if tw.closed.Load() {
		return false
	}

	// 如果正在调整offset,将timer放入缓冲队列
	if tw.adjusting.Load() {
		select {
		case tw.pendingTimers <- t:
			return true // 已加入缓冲队列
		default:
			// 缓冲队列已满，等待 adjusting 完成再重试（使用 channel 通知替代 busy-wait）
			if tw.logger != nil {
				tw.logger.Warnf("[TimingWheel] Pending timer queue is full, waiting for SetTimeOffset to complete")
			}
			<-tw.adjustDone
			// 调整完成后再次尝试
			return tw.addInternal(t)
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
		// 已经过期
		return false
	} else if expire < currentTime+tw.interval {
		// 放入当前轮的对应 bucket
		virtualID := expire / tw.tick
		b := tw.buckets[virtualID%tw.wheelSize]
		b.Add(t)

		// Set the bucket expiration time
		if b.SetExpiration(virtualID * tw.tick) {
			// 如果 bucket 的过期时间发生了变化，则需要将其入队。
			// 仅当轮次前进且该 bucket 被重用为新的过期时间时才需要入队。
			// 在同一轮周期内对相同值的重复设置将返回 false，
			// 因此不会将同一过期时间的 bucket 多次入队。
			tw.queue.Offer(b, b.Expiration())
		}

		return true
	} else {
		// Out of the interval. Put it into the overflow wheel
		overflowWheel := atomic.LoadPointer(&tw.overflowWheel)
		if overflowWheel == nil {
			childWheel := newOverflowWheel(
				tw.interval,
				tw.wheelSize,
				currentTime,
				tw.queue,
				tw.logger,
				tw.closed,
				tw.adjusting,
				tw.pendingTimers,
				tw.adjustDone,
			)
			atomic.CompareAndSwapPointer(
				&tw.overflowWheel,
				nil,
				unsafe.Pointer(childWheel),
			)
			overflowWheel = atomic.LoadPointer(&tw.overflowWheel)
		}
		return (*TimingWheel)(overflowWheel).addInternal(t)
	}
}

// runTimer 执行Timer的任务
// runLoop: 是否执行循环逻辑(loop函数)
func (tw *TimingWheel) runTimer(t *Timer, runLoop bool) {
	if !t.isActive() {
		return
	}

	// 标记执行中，阻止 Reset() 在 runTimer 期间清理字段
	if !t.executing.CompareAndSwap(false, true) {
		// 已经在执行中
		return
	}
	t.execWg.Add(1)

	// 再次检查，防止 stop() 在 CAS 之前已完成
	if !t.isActive() {
		t.executing.Store(false)
		t.execWg.Done()
		return
	}

	// 在持锁期间快照拷贝所有需要的字段，后续只用快照值
	// 这样即使 Timer 被并发取消/回收，快照值依然有效
	t.snapGen.Store(t.generation.Load())
	taskArgs := t.taskArgs
	loop := t.loop
	asyncTask := t.asyncTask
	task := t.task
	name := t.name
	scheduler := t.taskScheduler
	timerId := t.GetTimerId()

	// 释放执行锁（字段已快照完毕，后续不再直接读取 t 的可变字段）
	t.executing.Store(false)
	t.execWg.Done()

	if asyncTask != nil {
		// 异步任务,在独立goroutine中执行
		go func() {
			defer func() {
				if err := recover(); err != nil {
					if tw.logger != nil {
						tw.logger.Errorf("task panic, task_name:%s, err:%v", name, err)
					} else {
						fmt.Printf("task panic, task_name:%s, err:%v\n", name, err)
					}
				}
				if loop == nil && scheduler != nil {
					// 不是循环任务，释放
					scheduler.CancelTimer(timerId)
				}
			}()
			asyncTask(taskArgs...)
		}()
	} else if task != nil {
		// 同步任务,投递到callback channel,由消费者执行
		if scheduler == nil {
			return
		}

		// P1#6: 防止往已关闭的 channel 发送导致 panic。
		// jobScheduler.Stop() 会先设置 closed=1 再 close(c)，
		// 但 runTimer 使用的是快照 scheduler，时序上仍可能在 close 后投递。
		// 使用 defer/recover 作为最终安全网。
		func() {
			defer func() {
				if r := recover(); r != nil {
					if tw.logger != nil {
						tw.logger.Warnf("send to closed scheduler channel, task_name:%s, recover:%v", name, r)
					}
				}
			}()
			select {
			case scheduler.GetTimerCbChannel() <- t:
				// 投递成功
			default:
				// 队列已满,本次不执行
				if tw.logger != nil {
					tw.logger.Errorf("task queue is full, task will not be executed, task_name:%s", name)
				} else {
					fmt.Printf("task queue is full, task will not be executed, task_name:%s\n", name)
				}
				if loop == nil {
					// 不是循环任务，释放
					scheduler.CancelTimer(timerId)
				}
			}
		}()
	}

	if runLoop && loop != nil {
		// 循环任务,再次加入（使用快照的 loop，即使 t.loop 已被清空也安全）
		loop()
	}
}

// addOrRun 将定时器 t 插入当前时间轮；如果已过期则立即执行任务。
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
	for {
		id := atomic.AddUint64(&tw.timerIdSeed, 1)
		if id != 0 {
			return id
		}
		// uint64 溢出回 0 时跳过，因为 CancelTimer(0) 是 no-op
	}
}

// getNow 以无锁方式返回应用偏移后的当前时间
func (tw *TimingWheel) getNow() time.Time {
	offsetNs := tw.timeOffsetNs.Load()
	return time.Now().Add(time.Duration(offsetNs))
}

func (tw *TimingWheel) advanceClock(expiration int64) {
	currentTime := atomic.LoadInt64(&tw.currentTime)
	if expiration >= currentTime+tw.tick {
		currentTime = truncate(expiration, tw.tick)
		atomic.StoreInt64(&tw.currentTime, currentTime)

		// 尝试推进 overflow wheel 的时钟（如果存在）
		overflowWheel := atomic.LoadPointer(&tw.overflowWheel)
		if overflowWheel != nil {
			(*TimingWheel)(overflowWheel).advanceClock(currentTime)
		}
	}
}

// Start 启动当前时间轮。
func (tw *TimingWheel) Start() {
	tw.waitGroup.Wrap(func() {
		tw.queue.Poll(tw.exitC, func() int64 {
			return timeToMs(tw.getNow())
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

// Stop 停止当前时间轮。
//
// 如果有定时任务在独立的 goroutine 中运行，Stop 不会等待这些任务完成才返回。
// 如果调用方需要知道任务是否已完成，需要由调用方自行与任务进行协调。
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

// AfterFunc 在指定的时长后调用任务函数 f（在独立的 goroutine 中执行）。
// 返回值为可用于取消该定时调用的 `Timer`。
func (tw *TimingWheel) AfterFunc(d time.Duration, t *Timer) {
	t.SetExpiration(timeToMs(tw.getNow().Add(d)))

	tw.addOrRun(t)
}

// Scheduler 定义任务的执行计划。
type Scheduler interface {
	// Next 返回给定（上一次）时间之后的下一次执行时间。
	// 如果没有下一次时间则返回零时间。
	//
	// 所有时间都应为 UTC。
	Next(time.Time) time.Time
}

// ScheduleFunc 根据调度器 s 提供的执行计划周期性调用函数 f（在独立 goroutine 中执行）。
// 返回一个可通过 Stop 方法取消的 `Timer`。
//
// 如果调用方希望中途终止执行计划，必须显式停止定时器并确认定时器已停止，
// 因为当前实现中在定时任务到期与重新调度之间存在短暂的间隙。
//
// 内部实现：ScheduleFunc 会先调用 s.Next() 获取首次执行时间（如果非零则创建定时器），
// 每次任务即将执行时再次调用 s.Next() 计算下一次执行时间，若下一次时间非零则继续调度。
func (tw *TimingWheel) ScheduleFunc(t *Timer) error {
	expiration := t.Next(tw.getNow())
	if expiration.IsZero() {
		return fmt.Errorf("next time is zero")
	}

	t.SetExpiration(timeToMs(expiration))
	t.loop = func() {
		// 如果timingwheel已关闭，不能添加任务
		if tw.closed.Load() {
			if t.taskScheduler != nil {
				t.taskScheduler.CancelTimer(t.GetTimerId())
			}
			return
		}
		if !t.isActive() {
			return
		}
		expiration := t.Next(msToTime(t.GetExpiration()))
		if !expiration.IsZero() {
			// 安全检查：如果计算出的时间仍在过去（物理机时间跳变等异常情况），
			// 基于当前时间重新计算，避免 overflow
			now := tw.getNow()
			if expiration.Before(now) || expiration.Equal(now) {
				expiration = t.Next(now)
				if expiration.IsZero() {
					// 无法计算出有效的下次执行时间，取消定时器
					if t.taskScheduler != nil {
						t.taskScheduler.CancelTimer(t.GetTimerId())
					}
					return
				}
			}
			t.SetExpiration(timeToMs(expiration))
			tw.addOrRun(t)
		}
	}

	tw.addOrRun(t)

	return nil
}

// SetTimeOffset sets the time offset for the timing wheel.
// This operation is synchronous and blocks all timer operations until complete.
// offset: the time offset (can be positive or negative)
//
// This is the entry point that ensures TimingWheel always processes offset changes.
// If the offset is not used, it will simply remain 0 and have no impact.
func (tw *TimingWheel) SetTimeOffset(offset time.Duration) {
	if tw.closed.Load() {
		return
	}

	// 获取互斥锁,保护整个调整过程,确保串行执行
	tw.adjustMu.Lock()
	defer tw.adjustMu.Unlock()

	// 设置调整标志,阻止新的timer直接插入
	tw.adjusting.Store(true)
	defer func() {
		tw.adjusting.Store(false)
		// 广播通知所有等待者（替代 busy-wait）
		close(tw.adjustDone)
		// 重新创建 channel 供下次使用
		tw.adjustDone = make(chan struct{})
	}()

	// 原子读取旧的offset并计算差值
	oldOffsetNs := tw.timeOffsetNs.Load()
	newOffsetNs := int64(offset)
	offsetDeltaNs := newOffsetNs - oldOffsetNs

	if tw.logger != nil {
		tw.logger.Infof("[TimingWheel] Setting time offset from %v to %v (delta: %v)",
			time.Duration(oldOffsetNs), offset, time.Duration(offsetDeltaNs))
	}

	if offsetDeltaNs == 0 {
		// 没有实际变化,提前返回
		return
	}

	// 原子更新offset
	tw.timeOffsetNs.Store(newOffsetNs)
	offsetDeltaMs := offsetDeltaNs / int64(time.Millisecond)

	// 1. 收集所有活跃的 timer
	allTimers := tw.collectAllTimers()
	if tw.logger != nil {
		tw.logger.Infof("[TimingWheel] Collected %d active timers for offset adjustment", len(allTimers))
	}

	oldCurrentTime := atomic.LoadInt64(&tw.currentTime)
	newCurrentTime := oldCurrentTime + offsetDeltaMs

	// 记录已执行的Timer,避免重复处理
	executedTimers := make(map[uint64]bool)

	// 2. 清空所有bucket并调整所有层级的currentTime
	tw.clearAllBucketsRecursive()
	atomic.StoreInt64(&tw.currentTime, truncate(newCurrentTime, tw.tick))
	// 递归调整 overflow wheel 的 currentTime（bucket 已清空，避免残留）
	overflowWheel := atomic.LoadPointer(&tw.overflowWheel)
	if overflowWheel != nil {
		(*TimingWheel)(overflowWheel).adjustOffsetInternal(offsetDeltaMs)
	}

	// 3. 对于正向偏移(offsetDelta > 0),检查并执行跨过执行点的任务
	if offsetDeltaMs > 0 {
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
						tw.logger.Infof("[TimingWheel] Periodic timer %s crossed execution time due to offset, executing once", t.name)
					}
					// 调整过程中不允许触发 t.loop（它会走 addOrRun -> pendingTimers），
					// 所以这里只执行一次，然后手动计算下一次并直接插入。
					tw.runTimer(t, false)
					base := msToTime(newCurrentTime)
					next := t.Next(base)
					if !next.IsZero() {
						t.SetExpiration(timeToMs(next))
						tw.addOrRunDirect(t)
					}
					executedTimers[t.GetTimerId()] = true
				} else {
					// AfterFunc(一次性任务),立即执行
					if tw.logger != nil {
						tw.logger.Infof("[TimingWheel] One-time timer %s crossed execution time due to offset, executing", t.name)
					}
					tw.runTimer(t, false) // runLoop=false,不重复执行
					executedTimers[t.GetTimerId()] = true
				}
			}
		}
	}

	// 4. 处理未执行的Timer（保持原有绝对 expiration 不变）
	for _, t := range allTimers {
		if !t.isActive() {
			continue
		}

		// 跳过已执行的周期性任务(它们已经由loop函数重新调度)
		if executedTimers[t.GetTimerId()] {
			continue
		}

		// 重新插入时间轮：这里必须跳过 adjusting 检查，避免自我缓冲/死锁
		tw.addOrRunDirect(t)
	}

	// 5. overflow wheel 的 currentTime 已在第2步递归调整

	if tw.logger != nil {
		tw.logger.Infof("[TimingWheel] Time offset adjustment completed, currentTime: %d -> %d", oldCurrentTime, newCurrentTime)
	}

	// 处理期间累积的pending timers
	tw.processPendingTimers()
}

// clearAllBucketsRecursive clears all timers from buckets in this wheel and its overflow wheels.
func (tw *TimingWheel) clearAllBucketsRecursive() {
	tw.clearAllBuckets()
	overflowWheel := atomic.LoadPointer(&tw.overflowWheel)
	if overflowWheel != nil {
		(*TimingWheel)(overflowWheel).clearAllBucketsRecursive()
	}
}

// adjustOffsetInternal 用于overflow wheels (不需要再次收集timer)
func (tw *TimingWheel) adjustOffsetInternal(offsetMs int64) {
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
		(*TimingWheel)(overflowWheel).adjustOffsetInternal(offsetMs)
	}
}

// processPendingTimers 处理缓冲队列中所有累积的pending timers
// 此方法会持续排空队列,直到没有新的timer为止
func (tw *TimingWheel) processPendingTimers() {
	// 持续排空，直到队列为空且短时间内没有新的 timer 进入
	for {
		processed := 0
		// 一次性处理当前队列中的所有 timer
	drain:
		for {
			select {
			case pendingTimer := <-tw.pendingTimers:
				// 使用 addOrRunDirect 跳过 adjusting 检查，因为 processPendingTimers
				// 在 adjusting=true 期间被调用，若用 addOrRun 会再次放入 pendingTimers 死循环
				tw.addOrRunDirect(pendingTimer)
				processed++
			default:
				// 队列已空，退出内层循环
				break drain
			}
		}
		if processed == 0 {
			// 本轮没有处理任何 timer，说明队列已空
			break
		}
		if tw.logger != nil {
			tw.logger.Infof("[TimingWheel] Processed %d pending timers", processed)
		}
		// 继续下一轮，确保处理期间新进入的 timer
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
