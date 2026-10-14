package boundedmpsc

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	for _, tt := range []struct {
		capacity int
		wantErr  error
	}{
		{-2, ErrCapacityTooSmall}, {0, ErrCapacityTooSmall}, {1, ErrCapacityTooSmall},
		{3, ErrCapacityNotPower2}, {2, nil}, {4, nil}, {4096, nil},
	} {
		t.Run(fmt.Sprint(tt.capacity), func(t *testing.T) {
			q, err := New[int](tt.capacity)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("New(%d) error = %v, want %v", tt.capacity, err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if q != nil {
					t.Fatal("invalid capacity returned a queue")
				}
				return
			}
			if q.Cap() != tt.capacity || q.Len() != 0 || !q.Empty() {
				t.Fatalf("new queue: capacity=%d length=%d empty=%v", q.Cap(), q.Len(), q.Empty())
			}
		})
	}
}

func newQueue[T any](t *testing.T, capacity int) *Queue[T] {
	t.Helper()
	q, err := New[T](capacity)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func TestFIFOFullAndReuse(t *testing.T) {
	q := newQueue[int](t, 2)
	if v, ok := q.Pop(); ok || v != 0 {
		t.Fatalf("empty Pop = (%d, %v)", v, ok)
	}
	for cycle := 0; cycle < 100; cycle++ {
		base := cycle * 3
		if !q.Push(base) || !q.Push(base+1) || q.Push(-1) {
			t.Fatal("incorrect full admission")
		}
		if q.Len() != 2 || q.Empty() {
			t.Fatal("incorrect full state")
		}
		if got, ok := q.Pop(); !ok || got != base {
			t.Fatalf("Pop = (%d, %v), want %d", got, ok, base)
		}
		if !q.Push(base + 2) {
			t.Fatal("released slot was not reusable")
		}
		for want := base + 1; want <= base+2; want++ {
			if got, ok := q.Pop(); !ok || got != want {
				t.Fatalf("Pop = (%d, %v), want %d", got, ok, want)
			}
		}
		if !q.Empty() || q.Len() != 0 || q.Cap() != 2 {
			t.Fatal("incorrect drained state")
		}
	}
}

func TestPopBatch(t *testing.T) {
	q := newQueue[int](t, 4)
	dst := []int{-1, -1, -1, -1, -1, -1}
	if n := q.PopBatch(dst); n != 0 || dst[0] != -1 {
		t.Fatal("empty batch modified destination")
	}
	for i := 0; i < 4; i++ {
		if !q.Push(i) {
			t.Fatal("Push failed")
		}
	}
	if q.PopBatch(nil) != 0 || q.PopBatch(dst[:0]) != 0 || q.Len() != 4 {
		t.Fatal("empty destination consumed values")
	}
	if n := q.PopBatch(dst[:3]); n != 3 {
		t.Fatalf("batch count = %d, want 3", n)
	}
	for i := 0; i < 3; i++ {
		if dst[i] != i {
			t.Fatalf("dst[%d] = %d", i, dst[i])
		}
	}
	if dst[3] != -1 || q.Len() != 1 {
		t.Fatal("short batch changed extra values")
	}
	for i := 4; i < 7; i++ {
		if !q.Push(i) {
			t.Fatal("Push failed after batch")
		}
	}
	for i := range dst {
		dst[i] = -1
	}
	if n := q.PopBatch(dst); n != 4 {
		t.Fatalf("wrap batch count = %d, want 4", n)
	}
	for i := 0; i < 4; i++ {
		if dst[i] != i+3 {
			t.Fatalf("dst[%d] = %d, want %d", i, dst[i], i+3)
		}
	}
	if dst[4] != -1 || dst[5] != -1 || !q.Empty() || q.Len() != 0 {
		t.Fatal("batch changed destination suffix or failed to drain")
	}
}

func TestSlotsCleared(t *testing.T) {
	q := newQueue[*int](t, 4)
	values := [4]int{1, 2, 3, 4}
	for i := range values {
		if !q.Push(&values[i]) {
			t.Fatal("Push failed")
		}
	}
	if got, ok := q.Pop(); !ok || got != &values[0] {
		t.Fatal("pointer Pop failed")
	}
	if q.buffer[0] != nil {
		t.Fatal("Pop retained reference")
	}
	dst := make([]*int, 2)
	if q.PopBatch(dst) != 2 || dst[0] != &values[1] || dst[1] != &values[2] {
		t.Fatal("pointer batch failed")
	}
	if q.buffer[1] != nil || q.buffer[2] != nil || q.buffer[3] != &values[3] {
		t.Fatal("batch slot clearing incorrect")
	}
	if got, ok := q.Pop(); !ok || got != &values[3] {
		t.Fatal("last pointer Pop failed")
	}
	for i, value := range q.buffer {
		if value != nil {
			t.Fatalf("slot %d retained reference", i)
		}
	}
	if got, ok := q.Pop(); ok || got != nil {
		t.Fatal("empty Pop did not return zero value")
	}
	if !q.Push(nil) {
		t.Fatal("nil value rejected")
	}
	if got, ok := q.Pop(); !ok || got != nil {
		t.Fatal("nil payload confused with empty")
	}
}

func TestCounterWrap(t *testing.T) {
	q := newQueue[int](t, 4)
	q.head, q.tail = ^uint64(0)-1, ^uint64(0)-1
	for i := 1; i <= 4; i++ {
		if !q.Push(i) {
			t.Fatal("Push failed across counter wrap")
		}
	}
	if q.Push(5) || q.tail != 2 {
		t.Fatal("incorrect full state at counter wrap")
	}
	if got, ok := q.Pop(); !ok || got != 1 {
		t.Fatal("Pop failed across counter wrap")
	}
	dst := make([]int, 3)
	if q.PopBatch(dst) != 3 {
		t.Fatal("batch failed across counter wrap")
	}
	for i, v := range dst {
		if v != i+2 {
			t.Fatalf("dst[%d] = %d", i, v)
		}
	}
	if q.head != 2 || !q.Empty() || q.Len() != 0 {
		t.Fatal("incorrect drained counter wrap state")
	}
	if !q.Push(5) {
		t.Fatal("counter wrap slot not reusable")
	}
	if got, ok := q.Pop(); !ok || got != 5 {
		t.Fatal("counter wrap reuse failed")
	}
}

func TestZeroAllocations(t *testing.T) {
	t.Run("PushSuccess", func(t *testing.T) {
		q := newQueue[int](t, 2048)
		if n := testing.AllocsPerRun(1000, func() {
			if !q.Push(1) {
				panic("unexpected full queue")
			}
		}); n != 0 {
			t.Fatalf("Push allocations = %g", n)
		}
	})
	t.Run("PushFull", func(t *testing.T) {
		q := newQueue[int](t, 2)
		q.Push(1)
		q.Push(2)
		if n := testing.AllocsPerRun(1000, func() {
			if q.Push(3) {
				panic("unexpected admission")
			}
		}); n != 0 {
			t.Fatalf("full Push allocations = %g", n)
		}
	})
	t.Run("PopSuccess", func(t *testing.T) {
		q := newQueue[int](t, 2048)
		for i := 0; i < 1001; i++ {
			q.Push(i)
		}
		if n := testing.AllocsPerRun(1000, func() {
			if _, ok := q.Pop(); !ok {
				panic("unexpected empty queue")
			}
		}); n != 0 {
			t.Fatalf("Pop allocations = %g", n)
		}
	})
	t.Run("PopBatch", func(t *testing.T) {
		q := newQueue[int](t, 4096)
		for i := 0; i < 4004; i++ {
			q.Push(i)
		}
		dst := make([]int, 4)
		if n := testing.AllocsPerRun(1000, func() {
			if q.PopBatch(dst) != 4 {
				panic("unexpected short batch")
			}
		}); n != 0 {
			t.Fatalf("PopBatch allocations = %g", n)
		}
	})
}

func TestMPSC(t *testing.T) {
	type message struct{ producer, sequence int }
	producers := runtime.GOMAXPROCS(0)
	const perProducer = 200
	q := newQueue[message](t, 64)
	stop := make(chan struct{})
	start := make(chan struct{})
	var wg sync.WaitGroup
	t.Cleanup(func() { close(stop); wg.Wait() })
	for p := 0; p < producers; p++ {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			<-start
			for seq := 0; seq < perProducer; seq++ {
				for {
					select {
					case <-stop:
						return
					default:
					}
					if q.Push(message{p, seq}) {
						break
					}
					runtime.Gosched()
				}
			}
		}(p)
	}
	next := make([]int, producers)
	seen := make([]bool, producers*perProducer)
	deadline := time.Now().Add(30 * time.Second)
	close(start)
	for count := 0; count < len(seen); {
		if time.Now().After(deadline) {
			t.Fatalf("timeout after %d/%d messages", count, len(seen))
		}
		if n := q.Len(); n < 0 || n > q.Cap() {
			t.Fatalf("length out of bounds: %d", n)
		}
		_ = q.Empty()
		value, ok := q.Pop()
		if !ok {
			runtime.Gosched()
			continue
		}
		if value.producer < 0 || value.producer >= producers || value.sequence < 0 || value.sequence >= perProducer {
			t.Fatalf("invalid message %+v", value)
		}
		index := value.producer*perProducer + value.sequence
		if seen[index] {
			t.Fatalf("duplicate message %+v", value)
		}
		if value.sequence != next[value.producer] {
			t.Fatalf("out-of-order message %+v, want sequence %d", value, next[value.producer])
		}
		seen[index] = true
		next[value.producer]++
		count++
	}
	wg.Wait()
	for p, count := range next {
		if count != perProducer {
			t.Fatalf("producer %d: got %d messages", p, count)
		}
	}
	if !q.Empty() || q.Len() != 0 {
		t.Fatal("queue not drained")
	}
	if _, ok := q.Pop(); ok {
		t.Fatal("extra message")
	}
}
