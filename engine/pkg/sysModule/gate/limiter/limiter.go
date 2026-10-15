// Package limiter
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/8/17 0017 1:25
// 最后更新:  yr  2025/8/17 0017 1:25
package limiter

import (
	"sync"
	"time"
)

type RateLimiter struct {
	last   time.Time
	tokens int
	limit  int // 每秒允许的 token 数
	mu     sync.Mutex
}

func NewRateLimiter(limit int) *RateLimiter {
	return &RateLimiter{
		last:   time.Now(),
		tokens: limit,
		limit:  limit,
	}
}

func (r *RateLimiter) Allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(r.last).Seconds()
	r.last = now
	r.tokens += int(elapsed * float64(r.limit))
	if r.tokens > r.limit {
		r.tokens = r.limit
	}

	if r.tokens > 0 {
		r.tokens--
		return true
	}
	return false
}
