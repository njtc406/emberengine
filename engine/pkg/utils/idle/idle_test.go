// Package idle
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/11/26 00:40
// 最后更新:  yr  2025/11/26 00:40
package idle

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestBackoffConcurrent 验证 ExponentialBackoff 在高并发下不发生 data race 并返回合理值
func TestBackoffConcurrent(t *testing.T) {
	eb := NewExponentialBackoff(1*time.Millisecond, 100*time.Millisecond, 10)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				_ = eb.NextDelay()
			}
		}()
	}
	wg.Wait()
}

// TestControllerIntegration 模拟多个 producer 和单 consumer（worker）
func TestControllerIntegration(t *testing.T) {
	c := NewController(true, 1*time.Millisecond, 50*time.Millisecond, 3, 10)

	ch := make(chan int, 1024)

	var stop int32
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for atomic.LoadInt32(&stop) == 0 {
			select {
			case v := <-ch:
				_ = v
				c.Reset()
			default:
				c.Idle()
			}
		}
	}()

	// producers
	var pwg sync.WaitGroup
	for p := 0; p < 4; p++ {
		pwg.Add(1)
		go func() {
			defer pwg.Done()
			for i := 0; i < 1000; i++ {
				ch <- i
				c.Wake()
			}
		}()
	}

	// 等待 producers 完成
	pwg.Wait()

	// 通知 worker 退出
	atomic.StoreInt32(&stop, 1)
	c.Wake() // 防止 worker 正卡在 Idle 的 Cond.Wait

	wg.Wait()
}

func BenchmarkController(b *testing.B) {
	c := NewController(false, 1*time.Microsecond, 10*time.Microsecond, 1500, 10)
	ch := make(chan int, 4096)

	var consumed uint64
	var stop int32 = 0

	// worker（模拟 mailbox worker）
	go func() {
		for atomic.LoadInt32(&stop) == 0 {
			select {
			case v := <-ch:
				_ = v
				atomic.AddUint64(&consumed, 1)
			default:
				c.Idle()
			}
		}
	}()

	// benchmark 主体：向 channel 写入数据模拟 event
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ch <- 1
		c.Wake()
	}

	b.StopTimer()
	atomic.StoreInt32(&stop, 1)
	time.Sleep(20 * time.Millisecond) // 给 worker 收尾时间

	b.ReportMetric(float64(consumed)/b.Elapsed().Seconds(), "qps")
}

// BenchmarkBackoffOnly 测试只使用 ExponentialBackoff，不使用 Controller
func BenchmarkBackoffOnly(b *testing.B) {
	// 初始化指数退避
	backoff := NewExponentialBackoff(1*time.Microsecond, 10*time.Microsecond, 1500)
	ch := make(chan int, 4096)

	var consumed uint64
	var stop int32 = 0

	// worker（模拟 mailbox worker）
	go func() {
		for atomic.LoadInt32(&stop) == 0 {
			select {
			case v := <-ch:
				_ = v
				atomic.AddUint64(&consumed, 1)
				backoff.Reset() // 处理消息时重置 backoff
			default:
				// 没有消息时直接 sleep backoff
				time.Sleep(backoff.NextDelay())
			}
		}
	}()

	// benchmark 主体：向 channel 写入数据模拟 event
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch <- 1
	}
	b.StopTimer()

	// 停止 worker
	atomic.StoreInt32(&stop, 1)
	time.Sleep(20 * time.Millisecond) // 给 worker 收尾时间

	// 输出 qps
	b.ReportMetric(float64(consumed)/b.Elapsed().Seconds(), "qps")
}

// --- AdaptiveController Worker ---
func BenchmarkAdaptiveControllerWorker(b *testing.B) {
	ac := NewAdaptiveController(true, 1*time.Microsecond, 10*time.Microsecond, 500, 1000)
	ch := make(chan int, 4096)

	var consumed uint64
	var stop int32 = 0

	// worker
	go func() {
		for atomic.LoadInt32(&stop) == 0 {
			select {
			case v := <-ch:
				_ = v
				atomic.AddUint64(&consumed, 1)
			default:
				ac.Idle()
			}
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch <- 1
		ac.Wake()
	}
	b.StopTimer()

	atomic.StoreInt32(&stop, 1)
	time.Sleep(20 * time.Millisecond) // 收尾

	b.ReportMetric(float64(consumed)/b.Elapsed().Seconds(), "qps")
}

func BenchmarkAdaptiveControllerGosched(b *testing.B) {
	ac := NewAdaptiveController(false, 1*time.Microsecond, 10*time.Microsecond, 1000, 1000)
	ch := make(chan int, 4096)

	var consumed uint64
	var stop int32 = 0

	// worker goroutine
	go func() {
		for atomic.LoadInt32(&stop) == 0 {
			select {
			case v := <-ch:
				_ = v
				atomic.AddUint64(&consumed, 1)
				//ac.Reset()
			default:
				ac.Idle() // adaptive idle
			}
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch <- 1
		ac.Wake()
	}
	b.StopTimer()

	atomic.StoreInt32(&stop, 1)
	time.Sleep(20 * time.Millisecond) // 给 worker 收尾

	b.ReportMetric(float64(consumed)/b.Elapsed().Seconds(), "qps")
}

func BenchmarkGoschedOnly(b *testing.B) {
	ch := make(chan int, 4096)
	var consumed uint64
	var stop int32

	// worker goroutine
	go func() {
		for atomic.LoadInt32(&stop) == 0 {
			select {
			case v := <-ch:
				_ = v
				atomic.AddUint64(&consumed, 1)
			default:
				// 高频空闲阶段：直接让出 CPU
				runtime.Gosched()
			}
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch <- 1
	}
	b.StopTimer()

	atomic.StoreInt32(&stop, 1)
	time.Sleep(20 * time.Millisecond) // 给 worker 收尾

	b.ReportMetric(float64(consumed)/b.Elapsed().Seconds(), "qps")
}
