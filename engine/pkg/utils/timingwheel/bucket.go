package timingwheel

import (
	"container/list"
	"sync"
	"sync/atomic"
)

type bucket struct {
	// 64 位的原子操作要求 64 位对齐，但 32 位的编译器并不保证对齐。
	// 因此必须将 64 位字段放在结构体的第一个字段位置。
	//
	// 详细说明见: https://golang.org/pkg/sync/atomic/#pkg-note-BUG
	// 以及: https://go101.org/article/memory-layout.html
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
		// 注意：此操作要么执行定时器的任务，要么将定时器插入更低层时间轮的另一个 bucket。
		// 无论哪种情况，都不会对 b.mu 做后续加锁操作。
		if t.isActive() && reinsert != nil {
			reinsert(t)
		}

		e = next
	}

	b.SetExpiration(-1)
}
