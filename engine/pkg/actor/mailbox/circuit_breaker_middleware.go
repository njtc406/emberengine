// Package mailbox
// @Title  熔断中间件
// @Description  基于失败率的熔断中间件
// @Author  yr  2025/2/8
// @Update  yr  2025/2/8
package mailbox

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
)

// ErrCircuitBreakerOpen 熔断器打开错误
var ErrCircuitBreakerOpen = errors.New("circuit breaker is open")

// CircuitState 熔断器状态
type CircuitState int32

const (
	// StateClosed 关闭状态，正常处理请求
	StateClosed CircuitState = iota
	// StateOpen 打开状态，拒绝所有请求
	StateOpen
	// StateHalfOpen 半开状态，允许少量请求探测
	StateHalfOpen
)

func (s CircuitState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreakerMiddleware 熔断中间件
//
// 实现熔断器模式，当错误率超过阈值时自动打开熔断器，
// 经过冷却时间后进入半开状态，尝试放行部分请求。
//
// 状态转换：
//   - Closed -> Open: 错误率超过阈值
//   - Open -> HalfOpen: 冷却时间结束
//   - HalfOpen -> Closed: 探测请求成功
//   - HalfOpen -> Open: 探测请求失败
type CircuitBreakerMiddleware struct {
	logger log.ILoggerX

	// 配置
	failureThreshold   int           // 触发熔断的失败次数阈值
	successThreshold   int           // 半开状态下恢复的成功次数阈值
	cooldownDuration   time.Duration // 熔断冷却时间
	windowDuration     time.Duration // 统计窗口时间
	halfOpenMaxAllowed int           // 半开状态允许的最大探测请求数

	// 状态
	state        atomic.Int32
	failures     int
	successes    int
	halfOpenReqs int
	lastFailTime time.Time
	windowStart  time.Time
	mu           sync.Mutex

	// 统计
	totalRequests   uint64
	rejectedByBreak uint64
}

// CircuitBreakerOption 熔断器配置选项
type CircuitBreakerOption func(*CircuitBreakerMiddleware)

// WithCircuitBreakerLogger 设置日志器
func WithCircuitBreakerLogger(logger log.ILoggerX) CircuitBreakerOption {
	return func(m *CircuitBreakerMiddleware) {
		m.logger = logger
	}
}

// WithCooldownDuration 设置冷却时间
func WithCooldownDuration(d time.Duration) CircuitBreakerOption {
	return func(m *CircuitBreakerMiddleware) {
		m.cooldownDuration = d
	}
}

// WithWindowDuration 设置统计窗口时间
func WithWindowDuration(d time.Duration) CircuitBreakerOption {
	return func(m *CircuitBreakerMiddleware) {
		m.windowDuration = d
	}
}

// WithHalfOpenMaxAllowed 设置半开状态最大探测请求数
func WithHalfOpenMaxAllowed(n int) CircuitBreakerOption {
	return func(m *CircuitBreakerMiddleware) {
		m.halfOpenMaxAllowed = n
	}
}

// NewCircuitBreakerMiddleware 创建熔断中间件
//
// 参数：
//   - failureThreshold: 触发熔断的失败次数
//   - successThreshold: 半开状态恢复的成功次数
func NewCircuitBreakerMiddleware(failureThreshold, successThreshold int, opts ...CircuitBreakerOption) *CircuitBreakerMiddleware {
	m := &CircuitBreakerMiddleware{
		failureThreshold:   failureThreshold,
		successThreshold:   successThreshold,
		cooldownDuration:   30 * time.Second,
		windowDuration:     60 * time.Second,
		halfOpenMaxAllowed: 3,
		windowStart:        time.Now(),
	}
	m.state.Store(int32(StateClosed))
	for _, opt := range opts {
		opt(m)
	}
	return m
}

func (m *CircuitBreakerMiddleware) Name() string {
	return "CircuitBreaker"
}

func (m *CircuitBreakerMiddleware) OnStart() {
	if m.logger != nil {
		m.logger.Infof("CircuitBreakerMiddleware started: failureThreshold=%d, successThreshold=%d, cooldown=%v",
			m.failureThreshold, m.successThreshold, m.cooldownDuration)
	}
}

func (m *CircuitBreakerMiddleware) OnStop() {
	if m.logger != nil {
		state := CircuitState(m.state.Load())
		m.logger.Infof("CircuitBreakerMiddleware stopped: state=%s, total=%d, rejected=%d",
			state, m.totalRequests, m.rejectedByBreak)
	}
}

func (m *CircuitBreakerMiddleware) OnReceive(mctx inf.IMiddlewareContext) inf.MiddlewareResult {
	atomic.AddUint64(&m.totalRequests, 1)

	state := CircuitState(m.state.Load())

	switch state {
	case StateClosed:
		return inf.Continue()

	case StateOpen:
		// 检查是否应该转换到半开状态
		m.mu.Lock()
		if time.Since(m.lastFailTime) >= m.cooldownDuration {
			m.state.Store(int32(StateHalfOpen))
			m.halfOpenReqs = 0
			m.successes = 0
			m.mu.Unlock()
			if m.logger != nil {
				m.logger.Infof("CircuitBreaker state: open -> half-open")
			}
			return inf.Continue()
		}
		m.mu.Unlock()
		atomic.AddUint64(&m.rejectedByBreak, 1)
		return inf.Reject(ErrCircuitBreakerOpen)

	case StateHalfOpen:
		m.mu.Lock()
		if m.halfOpenReqs >= m.halfOpenMaxAllowed {
			m.mu.Unlock()
			atomic.AddUint64(&m.rejectedByBreak, 1)
			return inf.Reject(ErrCircuitBreakerOpen)
		}
		m.halfOpenReqs++
		m.mu.Unlock()
		return inf.Continue()
	}

	return inf.Continue()
}

func (m *CircuitBreakerMiddleware) OnComplete(mctx inf.IMiddlewareContext, err error, panicVal interface{}) {
	isFailure := err != nil || panicVal != nil
	state := CircuitState(m.state.Load())

	m.mu.Lock()
	defer m.mu.Unlock()

	// 重置过期的统计窗口
	if time.Since(m.windowStart) >= m.windowDuration {
		m.failures = 0
		m.windowStart = time.Now()
	}

	switch state {
	case StateClosed:
		if isFailure {
			m.failures++
			m.lastFailTime = time.Now()
			if m.failures >= m.failureThreshold {
				m.state.Store(int32(StateOpen))
				if m.logger != nil {
					m.logger.Warnf("CircuitBreaker state: closed -> open (failures=%d)", m.failures)
				}
			}
		}

	case StateHalfOpen:
		if isFailure {
			// 探测失败，回到打开状态
			m.state.Store(int32(StateOpen))
			m.lastFailTime = time.Now()
			if m.logger != nil {
				m.logger.Warnf("CircuitBreaker state: half-open -> open (probe failed)")
			}
		} else {
			m.successes++
			if m.successes >= m.successThreshold {
				// 探测成功，恢复关闭状态
				m.state.Store(int32(StateClosed))
				m.failures = 0
				if m.logger != nil {
					m.logger.Infof("CircuitBreaker state: half-open -> closed (recovered)")
				}
			}
		}
	}
}

// GetState 获取当前熔断器状态
func (m *CircuitBreakerMiddleware) GetState() CircuitState {
	return CircuitState(m.state.Load())
}

// GetStats 获取统计信息
func (m *CircuitBreakerMiddleware) GetStats() (total, rejected uint64, state CircuitState) {
	return atomic.LoadUint64(&m.totalRequests), atomic.LoadUint64(&m.rejectedByBreak), m.GetState()
}

// Reset 手动重置熔断器到关闭状态
func (m *CircuitBreakerMiddleware) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Store(int32(StateClosed))
	m.failures = 0
	m.successes = 0
	m.halfOpenReqs = 0
	m.windowStart = time.Now()
	if m.logger != nil {
		m.logger.Infof("CircuitBreaker manually reset to closed state")
	}
}
