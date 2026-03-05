package timingwheel

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel/delayqueue"
	"github.com/njtc406/emberengine/engine/pkg/utils/xcontext"
)

// adjustState 封装时间偏移调整相关的状态，所有层级（包括 overflow wheel）共享同一实例。
type adjustState struct {
	mu            sync.Mutex    // 保护整个调整过程，确保串行执行
	adjusting     atomic.Bool   // 是否正在调整时间偏移
	pendingTimers chan *Timer   // 时间调整期间的待处理 timer 缓冲队列
	adjustDone    chan struct{} // adjusting 结束时广播通知，替代 busy-wait
}

func newAdjustState() *adjustState {
	return &adjustState{
		pendingTimers: make(chan *Timer, 10000),
		adjustDone:    make(chan struct{}),
	}
}

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

	// adjust 封装时间偏移调整状态（所有层级共享同一指针）
	adjust *adjustState
}

// NewTimingWheel 使用指定的刻度和轮大小创建一个 TimingWheel 实例。
func NewTimingWheel(tick time.Duration, wheelSize int64, logger log.ILoggerX) *TimingWheel {
	tickMs := int64(tick / time.Millisecond)
	if tickMs <= 0 {
		if logger != nil {
			logger.Errorf("tick must be greater than or equal to 1ms, got=%v, fallback to 1ms", tick)
		}
		tickMs = 1
	}

	if wheelSize <= 0 {
		if logger != nil {
			logger.Errorf("wheelSize must be greater than 0, got=%d, fallback to 20", wheelSize)
		}
		wheelSize = 20
	}

	startMs := timeToMs(time.Now())

	return newTimingWheel(
		tickMs,
		wheelSize,
		startMs,
		delayqueue.New(int(wheelSize)),
		logger,
		new(atomic.Bool),
		newAdjustState(),
	)
}

// newTimingWheel 是内部辅助函数，用于创建 TimingWheel 实例。
// 也用于创建 overflow wheel（共享同一 adjust 实例）。
func newTimingWheel(tickMs int64, wheelSize int64, startMs int64, queue *delayqueue.DelayQueue, logger log.ILoggerX, closed *atomic.Bool, adj *adjustState) *TimingWheel {
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
		logger:      logger,
		closed:      closed,
		adjust:      adj,
	}
}

// add 将定时器 t 插入到当前时间轮。
func (tw *TimingWheel) add(t *Timer) bool {
	// 如果timingwheel已关闭，不能添加任务
	if tw.closed.Load() {
		return false
	}

	// 如果正在调整offset,将timer放入缓冲队列
	if tw.adjust.adjusting.Load() {
		select {
		case tw.adjust.pendingTimers <- t:
			return true // 已加入缓冲队列
		default:
			// 缓冲队列已满，等待 adjusting 完成再重试
			if tw.logger != nil {
				tw.logger.Warnf("[TimingWheel] Pending timer queue is full, waiting for SetTimeOffset to complete")
			}
			<-tw.adjust.adjustDone
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
			childWheel := newTimingWheel(
				tw.interval,
				tw.wheelSize,
				currentTime,
				tw.queue,
				tw.logger,
				tw.closed,
				tw.adjust, // overflow wheel 共享同一 adjustState
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
//
// Timer 不复用，所有字段在创建后只读，因此无需快照拷贝。
func (tw *TimingWheel) runTimer(t *Timer, runLoop bool) {
	if !t.isActive() {
		return
	}

	if t.asyncTask {
		// 异步任务,在独立goroutine中执行
		go func() {
			defer func() {
				if err := recover(); err != nil {
					if tw.logger != nil {
						tw.logger.Errorf("task panic, task_name:%s, err:%v", t.name, err)
					} else {
						fmt.Printf("task panic, task_name:%s, err:%v\n", t.name, err)
					}
				}
				if t.loop == nil && t.taskScheduler != nil {
					// 不是循环任务，释放
					t.taskScheduler.CancelTimer(t.timerId)
				}
			}()
			if err := t.task(xcontext.New(nil), t, t.taskArgs...); err != nil {
				if tw.logger != nil {
					tw.logger.Errorf("async task execute failed, task_name:%s, err:%v", t.name, err)
				} else {
					fmt.Printf("async task execute failed, task_name:%s, err:%v\n", t.name, err)
				}
			}
		}()
	} else if t.task != nil {
		// 同步任务,投递到callback channel,由消费者执行
		if t.taskScheduler == nil {
			return
		}

		// 防止往已关闭的 channel 发送导致 panic。
		func() {
			defer func() {
				if r := recover(); r != nil {
					if tw.logger != nil {
						tw.logger.Warnf("send to closed scheduler channel, task_name:%s, recover:%v", t.name, r)
					}
				}
			}()
			select {
			case t.taskScheduler.GetTimerCbChannel() <- t:
				// 投递成功
			default:
				// 队列已满,本次不执行
				if tw.logger != nil {
					tw.logger.Errorf("task queue is full, task will not be executed, task_name:%s", t.name)
				} else {
					fmt.Printf("task queue is full, task will not be executed, task_name:%s\n", t.name)
				}
				if t.loop == nil {
					// 不是循环任务，释放
					t.taskScheduler.CancelTimer(t.timerId)
				}
			}
		}()
	}

	if runLoop && t.loop != nil {
		// 循环任务,再次加入
		t.loop()
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

// AfterFunc 在指定的时长后调用任务函数 f。
func (tw *TimingWheel) AfterFunc(d time.Duration, t *Timer) {
	t.expiration.Store(timeToMs(tw.getNow().Add(d)))
	tw.addOrRun(t)
}

// Scheduler 定义任务的执行计划（由 cron 解析器返回）。
type Scheduler interface {
	// Next 返回给定（上一次）时间之后的下一次执行时间。
	// 如果没有下一次时间则返回零时间。
	Next(time.Time) time.Time
}

// ScheduleFunc 根据 Timer 的 Next() 提供的执行计划周期性调用任务函数。
// 返回一个可通过 Stop 方法取消的 Timer。
//
// 如果调用方希望中途终止执行计划，必须显式停止定时器并确认定时器已停止，
// 因为当前实现中在定时任务到期与重新调度之间存在短暂的间隙。
func (tw *TimingWheel) ScheduleFunc(t *Timer) error {
	expiration := t.Next(tw.getNow())
	if expiration.IsZero() {
		return fmt.Errorf("next time is zero")
	}

	t.expiration.Store(timeToMs(expiration))
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
			t.expiration.Store(timeToMs(expiration))
			tw.addOrRun(t)
		}
	}

	tw.addOrRun(t)

	return nil
}

// SetTimeOffset 设置时间偏移量，同步操作，会阻塞直到所有 timer 重算完毕。
// offset: 时间偏移量（可正可负）
func (tw *TimingWheel) SetTimeOffset(offset time.Duration) {
	if tw.closed.Load() {
		return
	}

	// 获取互斥锁,保护整个调整过程,确保串行执行
	tw.adjust.mu.Lock()
	defer tw.adjust.mu.Unlock()

	// 设置调整标志，阻止新的 timer 直接插入（改为缓冲到 pendingTimers）
	tw.adjust.adjusting.Store(true)

	// 计算偏移差值
	oldOffsetNs := tw.timeOffsetNs.Load()
	newOffsetNs := int64(offset)
	offsetDeltaNs := newOffsetNs - oldOffsetNs

	if tw.logger != nil {
		tw.logger.Infof("[TimingWheel] Setting time offset from %v to %v (delta: %v)",
			time.Duration(oldOffsetNs), offset, time.Duration(offsetDeltaNs))
	}

	if offsetDeltaNs == 0 {
		// 没有实际变化,提前返回
		tw.endAdjusting()
		return
	}

	// 原子更新 offset
	tw.timeOffsetNs.Store(newOffsetNs)
	offsetDeltaMs := offsetDeltaNs / int64(time.Millisecond)

	// 1. 收集所有活跃 timer
	allTimers := tw.collectAllTimers()
	if tw.logger != nil {
		tw.logger.Infof("[TimingWheel] Collected %d active timers for offset adjustment", len(allTimers))
	}

	// 2. 清空所有 bucket 并调整所有层级的 currentTime
	oldCurrentTime := atomic.LoadInt64(&tw.currentTime)
	newCurrentTime := oldCurrentTime + offsetDeltaMs

	tw.clearAllBucketsRecursive()
	atomic.StoreInt64(&tw.currentTime, truncate(newCurrentTime, tw.tick))
	overflowWheel := atomic.LoadPointer(&tw.overflowWheel)
	if overflowWheel != nil {
		(*TimingWheel)(overflowWheel).adjustCurrentTimeRecursive(offsetDeltaMs)
	}

	// 3. 解除 adjusting 阻塞，允许正常 add() 恢复
	//    后续的 addOrRun 调用不再需要特殊的 "direct" 路径
	tw.endAdjusting()

	// 4. 单轮循环：跨过执行点的 timer 立即执行，其余重新插入
	for _, t := range allTimers {
		if !t.isActive() {
			continue
		}

		if offsetDeltaMs > 0 && newCurrentTime >= t.expiration.Load() {
			// timer 跨过了执行点
			if t.interval > 0 || t.isCron {
				// 周期性任务：执行一次，然后基于新当前时间重算下次
				if tw.logger != nil {
					tw.logger.Infof("[TimingWheel] Periodic timer %s crossed execution time due to offset, executing once", t.name)
				}
				tw.runTimer(t, false)
				next := t.Next(msToTime(newCurrentTime))
				if !next.IsZero() {
					t.expiration.Store(timeToMs(next))
					tw.addOrRun(t)
				}
			} else {
				// 一次性任务：直接执行
				if tw.logger != nil {
					tw.logger.Infof("[TimingWheel] One-time timer %s crossed execution time due to offset, executing", t.name)
				}
				tw.runTimer(t, false)
			}
		} else {
			// 未跨过执行点：保持原 expiration，重新插入
			tw.addOrRun(t)
		}
	}

	// 5. 排空调整期间累积的 pending timers
	tw.drainPendingTimers()

	if tw.logger != nil {
		tw.logger.Infof("[TimingWheel] Time offset adjustment completed, currentTime: %d -> %d", oldCurrentTime, newCurrentTime)
	}
}

// endAdjusting 结束 adjusting 状态，广播通知所有等待者
func (tw *TimingWheel) endAdjusting() {
	tw.adjust.adjusting.Store(false)
	close(tw.adjust.adjustDone)
	tw.adjust.adjustDone = make(chan struct{})
}

// drainPendingTimers 排空调整期间累积的 pending timers（adjusting 已为 false）
func (tw *TimingWheel) drainPendingTimers() {
	for {
		select {
		case t := <-tw.adjust.pendingTimers:
			tw.addOrRun(t)
		default:
			return
		}
	}
}

// clearAllBucketsRecursive clears all timers from buckets in this wheel and its overflow wheels.
func (tw *TimingWheel) clearAllBucketsRecursive() {
	tw.clearAllBuckets()
	overflowWheel := atomic.LoadPointer(&tw.overflowWheel)
	if overflowWheel != nil {
		(*TimingWheel)(overflowWheel).clearAllBucketsRecursive()
	}
}

// adjustCurrentTimeRecursive 递归调整 overflow wheel 的 currentTime
func (tw *TimingWheel) adjustCurrentTimeRecursive(offsetMs int64) {
	if tw.closed.Load() {
		return
	}

	oldCurrentTime := atomic.LoadInt64(&tw.currentTime)
	newCurrentTime := oldCurrentTime + offsetMs
	atomic.StoreInt64(&tw.currentTime, truncate(newCurrentTime, tw.tick))

	overflowWheel := atomic.LoadPointer(&tw.overflowWheel)
	if overflowWheel != nil {
		(*TimingWheel)(overflowWheel).adjustCurrentTimeRecursive(offsetMs)
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
