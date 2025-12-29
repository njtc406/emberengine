// Package mailbox
// @Title  限流中间件
// @Description  基于令牌桶的限流中间件
// @Author  yr  2025/2/8
// @Update  yr  2025/2/8
package mailbox

import (
	"errors"
	"sync"
	"time"

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
type RateLimitMiddleware struct {
	logger     log.ILoggerX
	rate       float64   // 每秒产生的令牌数
	burst      int       // 桶容量（突发流量上限）
	tokens     float64   // 当前令牌数
	lastUpdate time.Time // 上次更新时间
	mu         sync.Mutex

	// 统计
	accepted uint64
	rejected uint64

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
//   - rate: 每秒允许的请求数
//   - burst: 突发流量上限（桶容量）
func NewRateLimitMiddleware(rate float64, burst int, opts ...RateLimitOption) *RateLimitMiddleware {
	m := &RateLimitMiddleware{
		rate:       rate,
		burst:      burst,
		tokens:     float64(burst), // 初始填满
		lastUpdate: time.Now(),
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
		m.logger.Infof("RateLimitMiddleware started: rate=%.2f/s, burst=%d", m.rate, m.burst)
	}
}

func (m *RateLimitMiddleware) OnStop() {
	if m.logger != nil {
		m.logger.Infof("RateLimitMiddleware stopped: accepted=%d, rejected=%d", m.accepted, m.rejected)
	}
}

func (m *RateLimitMiddleware) OnReceive(mctx inf.IMiddlewareContext) inf.MiddlewareResult {
	// 检查是否跳过限流
	if m.skipFunc != nil && m.skipFunc(mctx) {
		return inf.Continue()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// 计算新增的令牌
	now := time.Now()
	elapsed := now.Sub(m.lastUpdate).Seconds()
	m.tokens += elapsed * m.rate
	if m.tokens > float64(m.burst) {
		m.tokens = float64(m.burst)
	}
	m.lastUpdate = now

	// 尝试获取令牌
	if m.tokens >= 1 {
		m.tokens--
		m.accepted++
		return inf.Continue()
	}

	// 被限流
	m.rejected++
	return inf.Reject(ErrRateLimitExceeded)
}

func (m *RateLimitMiddleware) OnComplete(mctx inf.IMiddlewareContext, err error, panicVal interface{}) {
	// 限流中间件不需要后置处理
}

// GetStats 获取统计信息
func (m *RateLimitMiddleware) GetStats() (accepted, rejected uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.accepted, m.rejected
}
