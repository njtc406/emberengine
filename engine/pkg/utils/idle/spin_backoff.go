package idle

import (
	"runtime"
	"time"
)

// SpinBackoff 轻量确定性指数退避，适用于 TryLock 重试、信号量获取等短暂等待场景。
//
// 提供两种退避模式：
//   - Backoff(): 首次调用 Gosched 让步，后续 Sleep 指数递增至上限；
//   - Sleep():   纯 Sleep 退避（无 Gosched 前缀），从 µs 起步指数递增。
//
// 与 ExponentialBackoff 的区别：确定性延迟（无 jitter），栈分配友好（值类型）。
// 非并发安全，设计为单 goroutine 使用。
type SpinBackoff struct {
	current time.Duration
	max     time.Duration
}

// NewSpinBackoff 创建指定上限的 SpinBackoff。
func NewSpinBackoff(maxDelay time.Duration) SpinBackoff {
	return SpinBackoff{max: maxDelay}
}

// Backoff 执行一次退避：首次 Gosched 让步，后续 time.Sleep 指数递增。
func (b *SpinBackoff) Backoff() {
	if b.current == 0 {
		runtime.Gosched()
		b.current = time.Microsecond
	} else {
		time.Sleep(b.current)
		b.current *= 2
		if b.current > b.max {
			b.current = b.max
		}
	}
}

// Sleep 纯 Sleep 退避（无 Gosched 前缀），从 µs 起步指数递增至上限。
// 适用于 Gosched 已由外层循环承担的场景。
func (b *SpinBackoff) Sleep() {
	if b.current == 0 {
		b.current = time.Microsecond
	}
	time.Sleep(b.current)
	b.current *= 2
	if b.current > b.max {
		b.current = b.max
	}
}

// Reset 重置退避状态。
func (b *SpinBackoff) Reset() {
	b.current = 0
}
