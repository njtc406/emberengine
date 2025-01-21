package timer

import (
	"container/heap"
	"github.com/njtc406/emberengine/engine/utils/timelib"
	"sync"
	"time"
)

func SetupTimer(timer ITimer) ITimer {
	if timer.IsOpen() == true {
		return nil
	}

	timer.Open(true)
	timerHeapLock.Lock() // 使用锁规避竞争条件
	heap.Push(&timerHeap, timer)
	timerHeapLock.Unlock()
	return timer
}

func NewTimer(d time.Duration) *Timer {
	c := make(chan ITimer, 1)
	timer := newTimer(d, c, nil, "")
	SetupTimer(timer)

	return timer
}

func ReleaseTimer(timer *Timer) {
	releaseTimer(timer)
}

type _TimerHeap struct {
	timers []ITimer
}

func (h *_TimerHeap) Len() int {
	return len(h.timers)
}

func (h *_TimerHeap) Less(i, j int) bool {
	return h.timers[i].GetFireTime().Before(h.timers[j].GetFireTime())
}

func (h *_TimerHeap) Swap(i, j int) {
	h.timers[i], h.timers[j] = h.timers[j], h.timers[i]
}

func (h *_TimerHeap) Push(x interface{}) {
	h.timers = append(h.timers, x.(ITimer))
}

func (h *_TimerHeap) Pop() (ret interface{}) {
	l := len(h.timers)
	h.timers, ret = h.timers[:l-1], h.timers[l-1]
	return
}

var (
	timerHeap     _TimerHeap // 定时器heap对象
	timerHeapLock sync.Mutex // 一个全局的锁
	ticker        *time.Ticker
	tickerOnce    = new(sync.Once)
)

func StartTimer(minTimerInterval time.Duration, maxTimerNum int) {
	timerHeap.timers = make([]ITimer, 0, maxTimerNum)
	heap.Init(&timerHeap) // 初始化定时器heap
	tickerOnce.Do(func() {
		ticker = time.NewTicker(minTimerInterval)
	})

	go tickRoutine()
}

func StopTimer() {
	if ticker == nil {
		return
	}
	ticker.Stop()
	ticker = nil
}

func tickRoutine() {
	for {
		select {
		case _, ok := <-ticker.C:
			if !ok {
				return
			}
			tick()
		}
	}
}

func tick() {
	now := Now()
	timerHeapLock.Lock()
	if timerHeap.Len() <= 0 { // 没有任何定时器，立刻返回
		timerHeapLock.Unlock()
		return
	}
	nextFireTime := timerHeap.timers[0].GetFireTime()
	if nextFireTime.After(now) { // 没有到时间的定时器，返回
		timerHeapLock.Unlock()
		return
	}

	t := heap.Pop(&timerHeap).(ITimer)
	timerHeapLock.Unlock()
	t.Open(false)
	t.AppendChannel(t)

	return
}

func Now() time.Time {
	return timelib.Now()
}
