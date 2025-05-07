// Package mpsc provides an efficient implementation of a multi-producer, single-consumer lock-free queue.
//
// The Push function is safe to call from multiple goroutines. The Pop and Empty APIs must only be
// called from a single, consumer goroutine.
package mpsc

// This implementation is based on http://www.1024cores.net/home/lock-free-algorithms/queues/non-intrusive-mpsc-node-based-queue

import (
	"sync"
	"sync/atomic"
	"unsafe"
)

type node[T any] struct {
	next *node[T]
	val  T
}

type Queue[T any] struct {
	head, tail *node[T]
	_nil       T
	pool       sync.Pool
	len        int64
}

func New[T any]() *Queue[T] {
	q := &Queue[T]{}
	stub := &node[T]{}
	q.head = stub
	q.tail = stub
	q.pool.New = func() interface{} {
		return &node[T]{}
	}
	return q
}

// Push adds x to the back of the queue.
func (q *Queue[T]) Push(x T) bool {
	n := q.pool.Get().(*node[T]) // 从pool获取节点
	n.next = nil                 // 重置next
	n.val = x
	// current producer acquires head node
	// 当前生产者将新节点设置为 head（插入链表头部）。
	// 这样只需要通过原子交换更新 head 指针，不涉及其他节点，因此在并发场景下是安全的。
	// 如果改为插入尾部，则需要先更新前一个尾节点的 next 指针，再更新 tail 指针，
	// 多线程环境下容易出现竞态问题，因此选择插入头部更简洁安全。
	prev := (*node[T])(atomic.SwapPointer((*unsafe.Pointer)(unsafe.Pointer(&q.head)), unsafe.Pointer(n)))

	// release node to consumer
	atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&prev.next)), unsafe.Pointer(n))
	atomic.AddInt64(&q.len, 1)
	return true
}

// Pop removes the item from the front of the queue or nil if the queue is empty.
func (q *Queue[T]) Pop() (T, bool) {
	tail := q.tail
	next := (*node[T])(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&tail.next)))) // acquire
	if next != nil {
		q.tail = next
		v := next.val

		// clear
		tail.val = q._nil
		tail.next = nil
		q.pool.Put(tail)

		atomic.AddInt64(&q.len, -1)
		return v, true
	}
	return q._nil, false
}

// Empty returns true if the queue is empty
//
// Empty must be called from a single, consumer goroutine
func (q *Queue[T]) Empty() bool {
	tail := q.tail
	next := (*node[T])(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&tail.next))))
	return next == nil
}

func (q *Queue[T]) Len() int {
	return int(atomic.LoadInt64(&q.len))
}
