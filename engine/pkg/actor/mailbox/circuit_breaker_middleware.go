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

	"github.com/njtc406/emberengine/engine/pkg/dto"
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
	failures     atomic.Int32
	successes    atomic.Int32
	halfOpenReqs atomic.Int32
	lastFailTime atomic.Int64 // Unix nano
	windowStart  atomic.Int64 // Unix nano
	mu           sync.Mutex   // 仅用于状态转换的临界区保护

	// 统计
	totalRequests   atomic.Uint64
	rejectedByBreak atomic.Uint64
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
	}
	m.state.Store(int32(StateClosed))
	now := time.Now().UnixNano()
	m.windowStart.Store(now)
	m.lastFailTime.Store(now)
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
			state, m.totalRequests.Load(), m.rejectedByBreak.Load())
	}
}

func (m *CircuitBreakerMiddleware) OnReceive(mctx inf.IMiddlewareContext) dto.MiddlewareResult {
	m.totalRequests.Add(1)

	// 检查并重置过期的统计窗口（在 OnReceive 也检查，避免流量归零后 failures 虚假累加）
	now := time.Now()
	nowNano := now.UnixNano()
	windowStartNano := m.windowStart.Load()
	if now.Sub(time.Unix(0, windowStartNano)) >= m.windowDuration {
		if m.windowStart.CompareAndSwap(windowStartNano, nowNano) {
			m.failures.Store(0)
		}
	}

	state := CircuitState(m.state.Load())

	switch state {
	case StateClosed:
		// 关闭状态，直接放行
		return dto.Continue()

	case StateOpen:
		// 检查是否应该转换到半开状态（使用原子操作读取时间）
		lastFailNano := m.lastFailTime.Load()
		if time.Since(time.Unix(0, lastFailNano)) >= m.cooldownDuration {
			// 【P1-4 修复】状态切换 + 计数清零必须原子组合，否则与并发探测者
			// 在 HalfOpen 分支的 CAS 增加发生 race，导致探测计数被 Store(...) 覆盖、
			// 实际探测请求数短时超过 halfOpenMaxAllowed。
			m.mu.Lock()
			if m.state.Load() == int32(StateOpen) {
				// 真正的 Open→HalfOpen 切换者：先清零计数，再翻状态
				m.halfOpenReqs.Store(0)
				m.successes.Store(0)
				m.state.Store(int32(StateHalfOpen))
				if m.logger != nil {
					m.logger.Infof("CircuitBreaker state: open -> half-open")
				}
			}
			m.mu.Unlock()
			// 状态已确定为 HalfOpen，统一走下面的 CAS 取额度逻辑
			state = CircuitState(m.state.Load())
			if state != StateHalfOpen {
				// 极少数情况：刚切完又被探测失败翻回 Open
				m.rejectedByBreak.Add(1)
				return dto.Reject(ErrCircuitBreakerOpen)
			}
		} else {
			// 冷却时间未到，拒绝请求
			m.rejectedByBreak.Add(1)
			return dto.Reject(ErrCircuitBreakerOpen)
		}
		fallthrough

	case StateHalfOpen:
		// 原子地尝试获取探测机会
		for {
			current := m.halfOpenReqs.Load()
			if current >= int32(m.halfOpenMaxAllowed) {
				// 超过探测请求数限制
				m.rejectedByBreak.Add(1)
				return dto.Reject(ErrCircuitBreakerOpen)
			}
			// 尝试 CAS 增加计数
			if m.halfOpenReqs.CompareAndSwap(current, current+1) {
				// 成功获取探测机会
				return dto.Continue()
			}
			// CAS 失败，重试
		}
	}

	return dto.Continue()
}

func (m *CircuitBreakerMiddleware) OnComplete(mctx inf.IMiddlewareContext, err error, panicVal interface{}) {
	isFailure := err != nil || panicVal != nil
	state := CircuitState(m.state.Load())

	// 检查并重置过期的统计窗口
	now := time.Now()
	nowNano := now.UnixNano()
	windowStartNano := m.windowStart.Load()
	if now.Sub(time.Unix(0, windowStartNano)) >= m.windowDuration {
		// 尝试 CAS 更新窗口起始时间
		if m.windowStart.CompareAndSwap(windowStartNano, nowNano) {
			// 成功更新窗口，重置失败计数
			m.failures.Store(0)
		}
	}

	switch state {
	case StateClosed:
		if isFailure {
			// 原子地增加失败计数
			newFailures := m.failures.Add(1)
			m.lastFailTime.Store(nowNano)
			if int(newFailures) >= m.failureThreshold {
				// 尝试 CAS 转换到打开状态
				if m.state.CompareAndSwap(int32(StateClosed), int32(StateOpen)) {
					if m.logger != nil {
						m.logger.Warnf("CircuitBreaker state: closed -> open (failures=%d/%d, windowStart=%v)",
							newFailures, m.failureThreshold, time.Unix(0, windowStartNano))
					}
				}
			}
		}

	case StateHalfOpen:
		if isFailure {
			// 探测失败：HalfOpen → Open，需要在临界区内重置探测计数，
			// 避免下次 Open→HalfOpen 切换时与并发探测者出现 race。
			m.mu.Lock()
			if m.state.Load() == int32(StateHalfOpen) {
				m.state.Store(int32(StateOpen))
				m.halfOpenReqs.Store(0)
				m.successes.Store(0)
				m.lastFailTime.Store(nowNano)
				if m.logger != nil {
					m.logger.Warnf("CircuitBreaker state: half-open -> open (probe failed)")
				}
			}
			m.mu.Unlock()
		} else {
			// 探测成功，原子地增加成功计数
			newSuccesses := m.successes.Add(1)
			if int(newSuccesses) >= m.successThreshold {
				// HalfOpen → Closed：在临界区内重置所有计数
				m.mu.Lock()
				if m.state.Load() == int32(StateHalfOpen) {
					m.state.Store(int32(StateClosed))
					m.failures.Store(0)
					m.successes.Store(0)
					m.halfOpenReqs.Store(0)
					if m.logger != nil {
						m.logger.Infof("CircuitBreaker state: half-open -> closed (recovered)")
					}
				}
				m.mu.Unlock()
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
	return m.totalRequests.Load(), m.rejectedByBreak.Load(), m.GetState()
}

// Reset 手动重置熔断器到关闭状态。
//
// 持 mu 锁与 OnReceive 的 Open→HalfOpen 切换、OnComplete 的 HalfOpen→Open/Closed
// 组合写串行化，避免多个 atomic.Store 之间被 OnComplete 的状态机切换插入，
// 导致部分字段被覆盖回旧值。
func (m *CircuitBreakerMiddleware) Reset() {
	m.mu.Lock()
	m.state.Store(int32(StateClosed))
	m.failures.Store(0)
	m.successes.Store(0)
	m.halfOpenReqs.Store(0)
	now := time.Now().UnixNano()
	m.windowStart.Store(now)
	m.lastFailTime.Store(now)
	m.mu.Unlock()
	if m.logger != nil {
		m.logger.Infof("CircuitBreaker manually reset to closed state")
	}
}
