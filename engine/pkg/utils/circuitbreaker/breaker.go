// Package circuitbreaker 提供熔断器实现
// @Title  熔断器
// @Description  用于保护系统免受级联故障的影响，当错误率过高时自动熔断
// @Author  yr  2024/12/27
// @Update  yr  2024/12/27
package circuitbreaker

import (
	"sync"
	"sync/atomic"
	"time"
)

// State 熔断器状态
type State int32

const (
	// StateClosed 关闭状态（正常工作）
	StateClosed State = iota
	// StateOpen 打开状态（熔断中，拒绝请求）
	StateOpen
	// StateHalfOpen 半开状态（允许少量请求探测）
	StateHalfOpen
)

// String 返回状态的字符串表示
func (s State) String() string {
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

// Config 熔断器配置
type Config struct {
	// Threshold 触发熔断的连续失败次数阈值
	Threshold int64
	// Timeout 熔断恢复超时时间（从 Open 进入 HalfOpen）
	Timeout time.Duration
	// HalfOpenMax 半开状态允许的最大请求数
	HalfOpenMax int64
	// OnStateChange 状态变更回调（可选）
	OnStateChange func(from, to State)
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		Threshold:   5,
		Timeout:     30 * time.Second,
		HalfOpenMax: 3,
	}
}

// CircuitBreaker 熔断器
//
// 使用方式：
//
//	breaker := circuitbreaker.New(nil) // 使用默认配置
//
//	// 在执行操作前检查
//	if !breaker.Allow() {
//	    return ErrCircuitOpen
//	}
//
//	// 执行操作...
//	err := doSomething()
//
//	// 根据结果记录
//	if err != nil {
//	    breaker.RecordFailure()
//	} else {
//	    breaker.RecordSuccess()
//	}
//
// 在 EscalateFailure 中使用：
//
//	func (s *MyService) EscalateFailure(ctx context.Context, reason interface{}, evt inf.IEvent) {
//	    s.breaker.RecordFailure()
//	    if s.breaker.State() == circuitbreaker.StateOpen {
//	        // 熔断状态，执行降级逻辑
//	        s.handleCircuitOpen()
//	    }
//	}
type CircuitBreaker struct {
	config *Config

	state           atomic.Int32 // 当前状态
	failures        atomic.Int64 // 连续失败次数
	successes       atomic.Int64 // 半开状态下的连续成功次数
	halfOpenCount   atomic.Int64 // 半开状态下已放行的请求数
	lastFailureTime atomic.Int64 // 最后一次失败时间（UnixNano）

	mu sync.Mutex // 保护状态转换
}

// New 创建熔断器
func New(config *Config) *CircuitBreaker {
	if config == nil {
		config = DefaultConfig()
	}
	if config.Threshold <= 0 {
		config.Threshold = 5
	}
	if config.Timeout <= 0 {
		config.Timeout = 30 * time.Second
	}
	if config.HalfOpenMax <= 0 {
		config.HalfOpenMax = 3
	}

	cb := &CircuitBreaker{
		config: config,
	}
	cb.state.Store(int32(StateClosed))
	return cb
}

// State 获取当前状态
func (cb *CircuitBreaker) State() State {
	return State(cb.state.Load())
}

// Failures 获取当前失败计数
func (cb *CircuitBreaker) Failures() int64 {
	return cb.failures.Load()
}

// Allow 检查是否允许执行操作
//
// 返回 true 表示允许执行，false 表示熔断中应拒绝
func (cb *CircuitBreaker) Allow() bool {
	state := cb.State()

	switch state {
	case StateClosed:
		return true

	case StateOpen:
		// 检查是否可以进入半开状态
		if cb.shouldTransitionToHalfOpen() {
			cb.transitionTo(StateHalfOpen)
			cb.halfOpenCount.Store(1) // 当前请求算第一个
			return true
		}
		return false

	case StateHalfOpen:
		// 半开状态下限制请求数量
		count := cb.halfOpenCount.Add(1)
		return count <= cb.config.HalfOpenMax
	}

	return false
}

// RecordSuccess 记录一次成功
func (cb *CircuitBreaker) RecordSuccess() {
	state := cb.State()

	switch state {
	case StateClosed:
		// 正常状态，重置失败计数
		cb.failures.Store(0)

	case StateHalfOpen:
		// 半开状态，累计成功次数
		successes := cb.successes.Add(1)
		// 达到半开允许的请求数且全部成功，恢复到关闭状态
		if successes >= cb.config.HalfOpenMax {
			cb.transitionTo(StateClosed)
		}
	}
}

// RecordFailure 记录一次失败
func (cb *CircuitBreaker) RecordFailure() {
	cb.lastFailureTime.Store(time.Now().UnixNano())
	state := cb.State()

	switch state {
	case StateClosed:
		// 正常状态，累计失败次数
		failures := cb.failures.Add(1)
		if failures >= cb.config.Threshold {
			cb.transitionTo(StateOpen)
		}

	case StateHalfOpen:
		// 半开状态下失败，立即回到打开状态
		cb.transitionTo(StateOpen)
	}
}

// Reset 手动重置熔断器到关闭状态
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	oldState := cb.State()
	cb.state.Store(int32(StateClosed))
	cb.failures.Store(0)
	cb.successes.Store(0)
	cb.halfOpenCount.Store(0)

	if cb.config.OnStateChange != nil && oldState != StateClosed {
		cb.config.OnStateChange(oldState, StateClosed)
	}
}

// shouldTransitionToHalfOpen 检查是否应该从 Open 转换到 HalfOpen
func (cb *CircuitBreaker) shouldTransitionToHalfOpen() bool {
	lastFailure := cb.lastFailureTime.Load()
	if lastFailure == 0 {
		return false
	}
	elapsed := time.Since(time.Unix(0, lastFailure))
	return elapsed >= cb.config.Timeout
}

// transitionTo 状态转换
func (cb *CircuitBreaker) transitionTo(newState State) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	oldState := State(cb.state.Load())
	if oldState == newState {
		return
	}

	// 执行状态转换
	cb.state.Store(int32(newState))

	// 重置相关计数器
	switch newState {
	case StateClosed:
		cb.failures.Store(0)
		cb.successes.Store(0)
		cb.halfOpenCount.Store(0)
	case StateOpen:
		cb.successes.Store(0)
		cb.halfOpenCount.Store(0)
	case StateHalfOpen:
		cb.successes.Store(0)
		cb.halfOpenCount.Store(0)
	}

	// 回调通知
	if cb.config.OnStateChange != nil {
		cb.config.OnStateChange(oldState, newState)
	}
}

// Stats 熔断器统计信息
type Stats struct {
	State         State
	Failures      int64
	Successes     int64
	HalfOpenCount int64
	LastFailure   time.Time
}

// Stats 获取统计信息
func (cb *CircuitBreaker) Stats() Stats {
	lastFailure := cb.lastFailureTime.Load()
	var lastFailureTime time.Time
	if lastFailure > 0 {
		lastFailureTime = time.Unix(0, lastFailure)
	}

	return Stats{
		State:         cb.State(),
		Failures:      cb.failures.Load(),
		Successes:     cb.successes.Load(),
		HalfOpenCount: cb.halfOpenCount.Load(),
		LastFailure:   lastFailureTime,
	}
}
