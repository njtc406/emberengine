// Package mpmc
// @Title  title
// @Description  desc
// @Author  yr  2025/2/8
// @Update  yr  2025/2/8
package mpmc

import (
	"runtime"
	"testing"
	"time"
)

func BenchmarkMPMCQueue(b *testing.B) {
	capacity := int64(4096)
	q := NewQueue[int](capacity)
	var backoff1 = 1
	var maxBackoff1 = 1
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// 入队
			backoff1 = 1
			for !q.Push(1) {
				if backoff1 < maxBackoff1 {
					backoff1 *= 2
				}
				for i := 0; i < backoff1; i++ {
					if q.Push(1) {
						goto dequeue
					}
					runtime.Gosched()
				}
			}
		dequeue:
			// 出队
			backoff1 = 1
			for {
				_, ok := q.Pop()
				if ok {
					break
				}
				if backoff1 < maxBackoff1 {
					backoff1 *= 2
				}
				for i := 0; i < backoff1; i++ {
					time.Sleep(time.Microsecond * time.Duration(backoff1))
				}
			}
		}
	})
}

func BenchmarkChannel(b *testing.B) {
	ch := make(chan int, 4096)
	var backoff1 = 1
	var maxBackoff1 = 1
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// 入队
			select {
			case ch <- 1:
			default:
				if backoff1 < maxBackoff1 {
					backoff1 *= 2
				}
				for i := 0; i < backoff1; i++ {
					time.Sleep(time.Microsecond * time.Duration(backoff1))
				}
			}

			// 出队
			select {
			case <-ch:
			default:
				if backoff1 < maxBackoff1 {
					backoff1 *= 2
				}
				for i := 0; i < backoff1; i++ {
					time.Sleep(time.Microsecond * time.Duration(backoff1))
				}
			}
		}
	})
	b.StopTimer()
}

/*
cpu: AMD Ryzen 7 5700X 8-Core Processor
BenchmarkMPMCQueueParallel-16            5703537               213.3 ns/op             0 B/op          0 allocs/op
BenchmarkChannel-16                      6220136               193.2 ns/op             0 B/op          0 allocs/op
PASS
ok      github.com/njtc406/emberengine/engine/utils/mpmc        2.970s

goos: windows
goarch: amd64
cpu: AMD Ryzen 7 5700X 8-Core Processor
BenchmarkMPMCQueueParallel-16            5599890               215.2 ns/op             0 B/op          0 allocs/op
BenchmarkChannel-16                      6589651               194.5 ns/op             0 B/op          0 allocs/op
PASS
ok      github.com/njtc406/emberengine/engine/utils/mpmc        3.023s

========================================================
上面两次测试是queue中使用了runtime.Gosched()出让cpu
========================================================

goos: windows
goarch: amd64
pkg: github.com/njtc406/emberengine/engine/utils/mpmc
cpu: AMD Ryzen 7 5700X 8-Core Processor
BenchmarkMPMCQueueParallel-16           96434905                12.48 ns/op            0 B/op          0 allocs/op
BenchmarkChannel-16                      6193010               192.2 ns/op             0 B/op          0 allocs/op
PASS
ok      github.com/njtc406/emberengine/engine/utils/mpmc        3.734s

goos: windows
goarch: amd64
pkg: github.com/njtc406/emberengine/engine/utils/mpmc
cpu: AMD Ryzen 7 5700X 8-Core Processor
BenchmarkMPMCQueueParallel-16           81360896                12.45 ns/op            0 B/op          0 allocs/op
BenchmarkChannel-16                      6096594               195.8 ns/op             0 B/op          0 allocs/op
PASS
ok      github.com/njtc406/emberengine/engine/utils/mpmc        2.557s

========================================================
这两次测试queue使用了指数退避出让cpu,比起runtime.Gosched()性能提升了10几倍,同时也比原始的channel更快
========================================================
*/
