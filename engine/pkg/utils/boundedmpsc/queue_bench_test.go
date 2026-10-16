package boundedmpsc

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/njtc406/emberengine/v2/engine/pkg/utils/mpsc"
)

type tryQueue[T any] interface {
	Push(T) bool
	Pop() (T, bool)
}
type perfChannel[T any] chan T

func (q perfChannel[T]) Push(v T) bool {
	select {
	case q <- v:
		return true
	default:
		return false
	}
}
func (q perfChannel[T]) Pop() (v T, ok bool) {
	select {
	case v = <-q:
		return v, true
	default:
		return v, false
	}
}
func perfQueue[T any](name string) tryQueue[T] {
	switch name {
	case "MutexRing":
		q, err := New[T](4096)
		if err != nil {
			panic(err)
		}
		return q
	case "Channel":
		return make(perfChannel[T], 4096)
	default:
		return mpsc.New[T]()
	}
}
func BenchmarkQueue(b *testing.B) {
	for _, name := range []string{"MutexRing", "Channel", "UnboundedMPSC"} {
		for _, p := range []struct {
			name string
			n    int
		}{{"1P1C", 1}, {"2P1C", 2}, {"4P1C", 4}, {"MaxP1C", runtime.GOMAXPROCS(0)}} {
			for _, scenario := range []string{"Steady", "MostlyEmpty", "Batch16", "Batch64", "AlwaysFull"} {
				if scenario == "AlwaysFull" && name == "UnboundedMPSC" {
					continue
				}
				b.Run(fmt.Sprintf("%s/%s/Cap4096/Int/%s", name, p.name, scenario), func(b *testing.B) { benchQueue(b, name, p.n, scenario, 42) })
				b.Run(fmt.Sprintf("%s/%s/Cap4096/InterfacePointer/%s", name, p.name, scenario), func(b *testing.B) { v := 42; benchQueue[any](b, name, p.n, scenario, &v) })
			}
		}
	}
}
func benchQueue[T any](b *testing.B, name string, producers int, scenario string, value T) {
	b.StopTimer()
	b.ReportAllocs()
	q := perfQueue[T](name)
	if scenario == "AlwaysFull" {
		for i := 0; i < 4096; i++ {
			if !q.Push(value) {
				b.Fatal("prefill")
			}
		}
	}
	var rejected, consumed atomic.Int64
	var ready, done sync.WaitGroup
	start := make(chan struct{})
	finish := make(chan struct{})
	exit := make(chan struct{})
	ready.Add(producers + 1)
	done.Add(producers + 1)
	for p := 0; p < producers; p++ {
		go func(p int) {
			defer done.Done()
			ready.Done()
			<-start
			for i := p; i < b.N; i += producers {
				if scenario == "AlwaysFull" {
					if q.Push(value) {
						panic("full queue accepted")
					}
					rejected.Add(1)
					continue
				}
				if scenario == "MostlyEmpty" {
					for consumed.Load() < int64(i) {
						runtime.Gosched()
					}
				}
				for !q.Push(value) {
					rejected.Add(1)
					runtime.Gosched()
				}
			}
			<-exit
		}(p)
	}
	batch := 1
	if scenario == "Batch16" {
		batch = 16
	}
	if scenario == "Batch64" {
		batch = 64
	}
	dst := make([]T, batch)
	go func() {
		defer done.Done()
		ready.Done()
		<-start
		if scenario == "AlwaysFull" {
			for rejected.Load() < int64(b.N) {
				runtime.Gosched()
			}
		} else {
			for consumed.Load() < int64(b.N) {
				n := 0
				if native, ok := q.(interface{ PopBatch([]T) int }); ok && batch > 1 {
					n = native.PopBatch(dst)
				} else {
					for n < batch {
						if _, ok := q.Pop(); !ok {
							break
						}
						n++
					}
				}
				if n == 0 {
					runtime.Gosched()
				} else {
					consumed.Add(int64(n))
				}
			}
		}
		close(finish)
		<-exit
	}()
	ready.Wait()
	b.StartTimer()
	close(start)
	<-finish
	b.StopTimer()
	close(exit)
	done.Wait()
	if scenario != "AlwaysFull" && consumed.Load() != int64(b.N) {
		b.Fatalf("consumed %d want %d", consumed.Load(), b.N)
	}
	b.ReportMetric(float64(consumed.Load())/b.Elapsed().Seconds(), "msg/s")
	b.ReportMetric(float64(rejected.Load()), "rejects")
}
