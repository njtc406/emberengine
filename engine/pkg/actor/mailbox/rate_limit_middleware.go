// Package mailbox
// @Title  限流中间件
// @Description  基于令牌桶的限流中间件
// @Author  yr  2025/2/8
// @Update  yr  2025/2/8
package mailbox

import (
	"errors"
	"sync/atomic"

	"golang.org/x/time/rate"

	"github.com/njtc406/emberengine/engine/pkg/dto"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
)

// ErrRateLimitExceeded 限流错误
var ErrRateLimitExceeded = errors.New("rate limit exceeded")

// RateLimitMiddleware 基于令牌桶的限流中间件
//
// 功能：
//   - 限制消息入队速率，超过阈值时拒绝消息
//   - 支持突发流量（burst）
//   - 可选的白名单机制（如紧急消息跳过限流）
//
// 底层使用 golang.org/x/time/rate.Limiter，内部通过 CAS 实现，无互斥锁。
type RateLimitMiddleware struct {
	logger  log.ILoggerX
	limiter *rate.Limiter
	rateVal float64 // 记录配置值，用于日志
	burst   int

	// 统计
	accepted atomic.Uint64
	rejected atomic.Uint64

	// 可选：跳过限流的条件
	skipFunc func(mctx inf.IMiddlewareContext) bool
}

// RateLimitOption 限流中间件配置选项
type RateLimitOption func(*RateLimitMiddleware)

// WithRateLimitLogger 设置日志器
func WithRateLimitLogger(logger log.ILoggerX) RateLimitOption {
	return func(m *RateLimitMiddleware) {
		m.logger = logger
	}
}

// WithRateLimitSkipFunc 设置跳过限流的条件函数
// 返回 true 时跳过限流检查
func WithRateLimitSkipFunc(fn func(mctx inf.IMiddlewareContext) bool) RateLimitOption {
	return func(m *RateLimitMiddleware) {
		m.skipFunc = fn
	}
}

// NewRateLimitMiddleware 创建限流中间件
//
// 参数：
//   - ratePerSec: 每秒允许的请求数
//   - burst: 突发流量上限（桶容量）
func NewRateLimitMiddleware(ratePerSec float64, burst int, opts ...RateLimitOption) *RateLimitMiddleware {
	m := &RateLimitMiddleware{
		limiter: rate.NewLimiter(rate.Limit(ratePerSec), burst),
		rateVal: ratePerSec,
		burst:   burst,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

func (m *RateLimitMiddleware) Name() string {
	return "RateLimit"
}

func (m *RateLimitMiddleware) OnStart() {
	if m.logger != nil {
		m.logger.Infof("RateLimitMiddleware started: rate=%.2f/s, burst=%d", m.rateVal, m.burst)
	}
}

func (m *RateLimitMiddleware) OnStop() {
	if m.logger != nil {
		m.logger.Infof("RateLimitMiddleware stopped: accepted=%d, rejected=%d", m.accepted.Load(), m.rejected.Load())
	}
}

func (m *RateLimitMiddleware) OnReceive(mctx inf.IMiddlewareContext) dto.MiddlewareResult {
	// 检查是否跳过限流
	if m.skipFunc != nil && m.skipFunc(mctx) {
		return dto.Continue()
	}

	if !m.limiter.Allow() {
		m.rejected.Add(1)
		return dto.Reject(ErrRateLimitExceeded)
	}

	m.accepted.Add(1)
	return dto.Continue()
}

func (m *RateLimitMiddleware) OnComplete(mctx inf.IMiddlewareContext, err error, panicVal interface{}) {
	// 限流中间件不需要后置处理
}

// GetStats 获取统计信息
func (m *RateLimitMiddleware) GetStats() (accepted, rejected uint64) {
	return m.accepted.Load(), m.rejected.Load()
}
