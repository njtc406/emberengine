// Package idle
// 模块名: 指数退避
// 功能描述: 描述
// 作者:  yr  2025/11/24 00:42
// 最后更新:  yr  2025/11/24 00:42
package idle

import (
	"math/rand/v2"
	"sync/atomic"
	"time"
)

// ExponentialBackoff 提供线程安全的指数退避延迟计算。
//
// 设计目标：
//   - 在高并发场景下避免锁竞争，仅使用原子操作；
//   - 延迟上界由 MaxDelay 控制，支持最大重试次数 MaxRetries；
//   - 使用 full jitter 策略，随机 [0, exp) 之间的延迟。
//
// 注意：虽然支持并发调用，但典型场景仍是单 goroutine 使用。
type ExponentialBackoff struct {
	BaseDelay  time.Duration
	MaxDelay   time.Duration
	MaxRetries int
	retry      atomic.Int32 // 当前重试次数，通过原子访问
}

func NewExponentialBackoff(baseDelay, maxDelay time.Duration, maxRetries int) *ExponentialBackoff {
	if baseDelay <= 0 {
		baseDelay = 1
	}
	if maxDelay <= 0 {
		maxDelay = baseDelay
	}
	return &ExponentialBackoff{
		BaseDelay:  baseDelay,
		MaxDelay:   maxDelay,
		MaxRetries: maxRetries,
	}
}

func (eb *ExponentialBackoff) NextDelay() time.Duration {
	// 先原子递增，再基于“旧值”计算本次退避层级，避免多次原子读。
	cur := eb.retry.Add(1) - 1

	// 达到最大重试次数后，始终返回 MaxDelay，并将 retry 固定在 MaxRetries。
	if eb.MaxRetries > 0 && cur >= int32(eb.MaxRetries) {
		// 确保 retry 不无限增长
		eb.retry.Store(int32(eb.MaxRetries))
		return eb.MaxDelay
	}

	// cap 移位，避免超大位移
	shift := cur
	if shift > 62 {
		shift = 62
	}

	// 计算指数（使用 int64 防止 overflow）
	expInt := int64(eb.BaseDelay) << uint(shift)
	if expInt <= 0 {
		expInt = int64(eb.MaxDelay)
	}
	maxInt := int64(eb.MaxDelay)
	if expInt > maxInt {
		expInt = maxInt
	}

	// Full jitter: 随机 [0, exp)
	if expInt <= 1 {
		return 0
	}
	jitter := rand.Int64N(expInt)
	return time.Duration(jitter)
}

func (eb *ExponentialBackoff) Reset() {
	// 将 retry 置零
	eb.retry.Store(0)
}
