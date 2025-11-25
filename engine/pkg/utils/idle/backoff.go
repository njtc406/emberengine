// Package backoff
// 模块名: 指数退避
// 功能描述: 描述
// 作者:  yr  2025/11/24 00:42
// 最后更新:  yr  2025/11/24 00:42
package idle

import (
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"
)

// 单线程中使用,不使用锁
type ExponentialBackoff struct {
	BaseDelay  time.Duration
	MaxDelay   time.Duration
	MaxRetries int
	retry      int32 // 通过原子访问

	rndMu sync.Mutex
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
	// 读取并更新 retry 原子地
	cur := atomic.LoadInt32(&eb.retry)
	if eb.MaxRetries > 0 && cur >= int32(eb.MaxRetries) {
		// 保持在最大延迟；同时保证 retry 不无限增长
		atomic.StoreInt32(&eb.retry, int32(eb.MaxRetries))
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
	var jitter int64
	eb.rndMu.Lock()
	if expInt <= 1 {
		jitter = 0
	} else {
		jitter = rand.Int64N(expInt)
	}
	eb.rndMu.Unlock()

	// 递增 retry（允许并发竞争，但最终值通过原子设置）
	atomic.AddInt32(&eb.retry, 1)

	return time.Duration(jitter)
}

func (eb *ExponentialBackoff) Reset() {
	// 将 retry 置零
	atomic.StoreInt32(&eb.retry, 0)
}
