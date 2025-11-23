// Package backoff
// 模块名: 指数退避
// 功能描述: 描述
// 作者:  yr  2025/11/24 00:42
// 最后更新:  yr  2025/11/24 00:42
package backoff

import (
	"math/rand/v2"
	"sync/atomic"
	"time"
)

type ExponentialBackoff struct {
	BaseDelay  time.Duration
	MaxDelay   time.Duration
	MaxRetries int
	retry      int32
}

func NewExponentialBackoff(baseDelay, maxDelay time.Duration, maxRetries int) *ExponentialBackoff {
	return &ExponentialBackoff{
		BaseDelay:  baseDelay,
		MaxDelay:   maxDelay,
		MaxRetries: maxRetries,
	}
}

func (eb *ExponentialBackoff) NextDelay() time.Duration {
	cur := atomic.LoadInt32(&eb.retry)

	if eb.MaxRetries > 0 && cur >= int32(eb.MaxRetries) {
		return eb.MaxDelay
	}

	// cap 2^n shift 防止溢出
	shift := cur
	if shift > 62 {
		shift = 62
	}

	// 计算指数
	exp := eb.BaseDelay << shift
	if exp > eb.MaxDelay {
		exp = eb.MaxDelay
	}

	// 加入 jitter（FullJitter）
	delay := time.Duration(rand.Int64N(int64(exp)))

	atomic.AddInt32(&eb.retry, 1)
	return delay
}

func (eb *ExponentialBackoff) Reset() {
	atomic.StoreInt32(&eb.retry, 0)
}
