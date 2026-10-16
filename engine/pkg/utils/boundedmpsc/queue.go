// Package boundedmpsc provides a fixed-capacity multiple-producer,
// single-consumer FIFO queue. Operations serialize on a mutex; they never wait
// for capacity or data, but may wait for the mutex.
package boundedmpsc

import (
	"errors"
	"sync"
)

var (
	// ErrCapacityTooSmall indicates a capacity below two.
	ErrCapacityTooSmall = errors.New("boundedmpsc: capacity must be at least 2")
	// ErrCapacityNotPower2 indicates a capacity that is not a power of two.
	ErrCapacityNotPower2 = errors.New("boundedmpsc: capacity must be a power of two")
)

// Queue is a bounded FIFO. Construct it with New; it must not be copied.
// Push supports concurrent producers. Pop, PopBatch, and Empty must all be
// called by the same consumer goroutine. Len and Cap may be called concurrently.
// Storage is fixed at construction and consumed slots are cleared before reuse.
type Queue[T any] struct {
	_      noCopy
	mu     sync.Mutex
	buffer []T
	mask   uint64
	head   uint64
	tail   uint64
	count  int
}

type noCopy struct{}

func (*noCopy) Lock()   {}
func (*noCopy) Unlock() {}

// New creates a queue with exactly capacity slots. Capacity must be a power of
// two and at least two; invalid capacities are rejected, never rounded up.
func New[T any](capacity int) (*Queue[T], error) {
	if capacity < 2 {
		return nil, ErrCapacityTooSmall
	}
	if capacity&(capacity-1) != 0 {
		return nil, ErrCapacityNotPower2
	}
	return &Queue[T]{buffer: make([]T, capacity), mask: uint64(capacity - 1)}, nil
}

// Push appends value, returning false only when the queue is full while locked.
// It does not allocate or wait for the consumer to free capacity.
func (q *Queue[T]) Push(value T) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.count == len(q.buffer) {
		return false
	}
	q.buffer[q.tail&q.mask] = value
	q.tail++
	q.count++
	return true
}

// Pop removes the oldest value, or returns the zero value and false when empty.
func (q *Queue[T]) Pop() (T, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	var zero T
	if q.count == 0 {
		return zero, false
	}
	index := q.head & q.mask
	value := q.buffer[index]
	q.buffer[index] = zero
	q.head++
	q.count--
	return value, true
}

// PopBatch removes up to len(dst) oldest values into dst without allocating.
// It returns the number written and leaves the remainder of dst unchanged.
// A nil or empty dst consumes nothing.
func (q *Queue[T]) PopBatch(dst []T) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := min(len(dst), q.count)
	var zero T
	for i := 0; i < n; i++ {
		index := q.head & q.mask
		dst[i] = q.buffer[index]
		q.buffer[index] = zero
		q.head++
	}
	q.count -= n
	return n
}

// Empty reports whether the consumer has no value available at this instant.
// A producer may publish a value immediately after this method returns.
func (q *Queue[T]) Empty() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.count == 0
}

// Len returns a snapshot of the number of queued values. It is only an
// instantaneous approximation for callers and must not be used to decide
// whether a subsequent Push or Pop will succeed.
func (q *Queue[T]) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.count
}

// Cap returns the fixed capacity of the queue.
func (q *Queue[T]) Cap() int { return len(q.buffer) }
