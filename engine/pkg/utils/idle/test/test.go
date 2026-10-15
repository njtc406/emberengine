package main

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/utils/idle"
)

//func main() {
//	condThresholds := []int{100, 500, 1000, 5000}
//	baseDelay := 1 * time.Microsecond
//	maxDelay := 10 * time.Microsecond
//	maxRetries := 10
//	producers := 4
//	duration := 5 * time.Second
//
//	fmt.Println("condThreshold\tQPS\tCPU(%)")
//
//	for _, threshold := range condThresholds {
//		c := idle.NewController(true, baseDelay, maxDelay, threshold, maxRetries)
//		ch := make(chan int, 8192)
//
//		var consumed uint64
//		var stop int32 = 0
//		var wg sync.WaitGroup
//
//		// worker
//		wg.Add(1)
//		go func() {
//			defer wg.Done()
//			for atomic.LoadInt32(&stop) == 0 {
//				select {
//				case v := <-ch:
//					_ = v
//					atomic.AddUint64(&consumed, 1)
//					c.Reset()
//				default:
//					c.Idle()
//				}
//			}
//			// exit 前唤醒防止 cond 死锁
//			c.Wake()
//		}()
//
//		// producers
//		for p := 0; p < producers; p++ {
//			wg.Add(1)
//			go func() {
//				defer wg.Done()
//				for atomic.LoadInt32(&stop) == 0 {
//					select {
//					case ch <- 1:
//						c.Wake()
//					default:
//						// channel 满了也不阻塞
//						time.Sleep(time.Microsecond)
//					}
//				}
//			}()
//		}
//
//		time.Sleep(duration)
//		atomic.StoreInt32(&stop, 1)
//		wg.Wait()
//
//		qps := float64(consumed) / duration.Seconds()
//		numGoroutine := runtime.NumGoroutine()
//		cpuEst := float64(numGoroutine) * 100.0 / float64(runtime.NumCPU())
//
//		fmt.Printf("%d\t%.0f\t%.2f\n", threshold, qps, cpuEst)
//	}
//}

/*
AMD Ryzen 7 2700 Eight-Core Processor
condThreshold		QPS			CPU(%)
100				3,566,831		12.5
500				3,509,949		6.25
1000			3,492,181		6.25
5000			3,552,915		6.25
*/

func main() {
	const N = 10_000_000

	fmt.Println("=== Backoff-only ===")
	bo := idle.NewExponentialBackoff(1*time.Microsecond, 10*time.Microsecond, 1000)
	ch := make(chan int, 4096)
	var consumed uint64
	var stop int32

	go func() {
		for atomic.LoadInt32(&stop) == 0 {
			select {
			case v := <-ch:
				_ = v
				atomic.AddUint64(&consumed, 1)
				bo.Reset()
			default:
				time.Sleep(bo.NextDelay())
			}
		}
	}()

	t0 := time.Now()
	for i := 0; i < N; i++ {
		ch <- 1
	}
	time.Sleep(50 * time.Millisecond)
	atomic.StoreInt32(&stop, 1)
	delta := time.Since(t0)
	fmt.Printf("Backoff-only: consumed=%d, elapsed=%v, qps=%.2f\n", consumed, delta, float64(consumed)/delta.Seconds())

	fmt.Println("=== AdaptiveController ===")
	ac := idle.NewAdaptiveController(true, 1*time.Microsecond, 10*time.Microsecond, 1000, 1000)
	ch2 := make(chan int, 4096)
	var consumed2 uint64
	stop = 0
	go func() {
		for atomic.LoadInt32(&stop) == 0 {
			select {
			case v := <-ch2:
				_ = v
				atomic.AddUint64(&consumed2, 1)
				ac.Reset()
			default:
				ac.Idle()
			}
		}
	}()

	t1 := time.Now()
	for i := 0; i < N; i++ {
		ch2 <- 1
		ac.Wake()
	}
	time.Sleep(50 * time.Millisecond)
	atomic.StoreInt32(&stop, 1)
	delta2 := time.Since(t1)
	fmt.Printf("AdaptiveController: consumed=%d, elapsed=%v, qps=%.2f\n", consumed2, delta2, float64(consumed2)/delta2.Seconds())
}
