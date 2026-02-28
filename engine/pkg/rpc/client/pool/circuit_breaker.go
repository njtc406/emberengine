// Package pool
// @Title  熔断器模式
// @Description  为连接池提供熔断器保护，防止级联故障
// @Author  yr  2025/1/20
// @Update  yr  2025/1/20
package pool

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/log"
)

// CircuitBreakerState 熔断器状态
type CircuitBreakerState int32

const (
	CircuitClosed   CircuitBreakerState = iota // 关闭状态（正常）
	CircuitOpen                                // 开启状态（熔断）
	CircuitHalfOpen                            // 半开状态（试探）
)

// CircuitBreakerConfig 熔断器配置
type CircuitBreakerConfig struct {
	FailureThreshold    int           `json:"failure_threshold"`     // 故障阈值
	FailureRate         float64       `json:"failure_rate"`          // 故障率阈值
	RecoveryTimeout     time.Duration `json:"recovery_timeout"`      // 恢复超时时间
	MinRequestThreshold int           `json:"min_request_threshold"` // 最小请求阈值
	HalfOpenMaxCalls    int           `json:"half_open_max_calls"`   // 半开状态最大调用次数
}

// DefaultCircuitBreakerConfig 默认熔断器配置
func DefaultCircuitBreakerConfig() *CircuitBreakerConfig {
	return &CircuitBreakerConfig{
		FailureThreshold:    5,
		FailureRate:         0.5, // 50%失败率
		RecoveryTimeout:     30 * time.Second,
		MinRequestThreshold: 10,
		HalfOpenMaxCalls:    3,
	}
}

// CircuitBreakerMetrics 熔断器指标
type CircuitBreakerMetrics struct {
	TotalRequests   int64     `json:"total_requests"`
	FailedRequests  int64     `json:"failed_requests"`
	SuccessRequests int64     `json:"success_requests"`
	LastFailureTime time.Time `json:"last_failure_time"`
	LastSuccessTime time.Time `json:"last_success_time"`
	StateChanges    int64     `json:"state_changes"`
	LastStateChange time.Time `json:"last_state_change"`
	HalfOpenCalls   int64     `json:"half_open_calls"`
}

// CircuitBreaker 熔断器
type CircuitBreaker struct {
	config  *CircuitBreakerConfig
	state   CircuitBreakerState
	metrics *CircuitBreakerMetrics
	mutex   sync.RWMutex
	logger  log.ILoggerX

	// 时间窗口统计
	windowStart    time.Time
	windowDuration time.Duration
}

// NewCircuitBreaker 创建新的熔断器
func NewCircuitBreaker(config *CircuitBreakerConfig, logger log.ILoggerX) *CircuitBreaker {
	if config == nil {
		config = DefaultCircuitBreakerConfig()
	}

	return &CircuitBreaker{
		config:         config,
		state:          CircuitClosed,
		metrics:        &CircuitBreakerMetrics{},
		logger:         logger,
		windowStart:    time.Now(),
		windowDuration: time.Minute, // 1分钟窗口
	}
}

// CanCall 检查是否可以执行调用
func (cb *CircuitBreaker) CanCall() bool {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	switch cb.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		// 检查是否可以进入半开状态
		if time.Since(cb.metrics.LastFailureTime) > cb.config.RecoveryTimeout {
			cb.mutex.RUnlock()
			cb.mutex.Lock()
			// 双重检查
			if cb.state == CircuitOpen && time.Since(cb.metrics.LastFailureTime) > cb.config.RecoveryTimeout {
				cb.state = CircuitHalfOpen
				cb.metrics.HalfOpenCalls = 0
				cb.metrics.StateChanges++
				cb.metrics.LastStateChange = time.Now()
				if cb.logger != nil {
					cb.logger.Info("Circuit breaker entering half-open state")
				}
			}
			cb.mutex.Unlock()
			cb.mutex.RLock()
			return cb.state == CircuitHalfOpen
		}
		return false
	case CircuitHalfOpen:
		return cb.metrics.HalfOpenCalls < int64(cb.config.HalfOpenMaxCalls)
	default:
		return false
	}
}

// RecordSuccess 记录成功调用
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	atomic.AddInt64(&cb.metrics.TotalRequests, 1)
	atomic.AddInt64(&cb.metrics.SuccessRequests, 1)
	cb.metrics.LastSuccessTime = time.Now()

	if cb.state == CircuitHalfOpen {
		cb.metrics.HalfOpenCalls++
		// 如果半开状态下连续成功，切换到关闭状态
		if cb.metrics.HalfOpenCalls >= int64(cb.config.HalfOpenMaxCalls) {
			cb.state = CircuitClosed
			cb.metrics.StateChanges++
			cb.metrics.LastStateChange = time.Now()
			if cb.logger != nil {
				cb.logger.Info("Circuit breaker closing after successful recovery")
			}
		}
	}
}

// RecordFailure 记录失败调用
func (cb *CircuitBreaker) RecordFailure() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	atomic.AddInt64(&cb.metrics.TotalRequests, 1)
	atomic.AddInt64(&cb.metrics.FailedRequests, 1)
	cb.metrics.LastFailureTime = time.Now()

	if cb.state == CircuitHalfOpen {
		cb.metrics.HalfOpenCalls++
		// 半开状态下失败，立即切换到开启状态
		cb.state = CircuitOpen
		cb.metrics.StateChanges++
		cb.metrics.LastStateChange = time.Now()
		if cb.logger != nil {
			cb.logger.Warn("Circuit breaker opening due to failure in half-open state")
		}
		return
	}

	if cb.state == CircuitClosed {
		cb.checkAndTripCircuit()
	}
}

// checkAndTripCircuit 检查并触发熔断
func (cb *CircuitBreaker) checkAndTripCircuit() {
	now := time.Now()

	// 重置时间窗口
	if now.Sub(cb.windowStart) > cb.windowDuration {
		cb.windowStart = now
		cb.metrics.TotalRequests = 0
		cb.metrics.FailedRequests = 0
		cb.metrics.SuccessRequests = 0
	}

	totalRequests := atomic.LoadInt64(&cb.metrics.TotalRequests)
	failedRequests := atomic.LoadInt64(&cb.metrics.FailedRequests)

	// 检查是否达到最小请求阈值
	if totalRequests < int64(cb.config.MinRequestThreshold) {
		return
	}

	// 检查失败次数阈值
	if failedRequests >= int64(cb.config.FailureThreshold) {
		cb.tripCircuit()
		return
	}

	// 检查失败率阈值
	failureRate := float64(failedRequests) / float64(totalRequests)
	if failureRate >= cb.config.FailureRate {
		cb.tripCircuit()
	}
}

// tripCircuit 触发熔断
func (cb *CircuitBreaker) tripCircuit() {
	if cb.state != CircuitOpen {
		cb.state = CircuitOpen
		cb.metrics.StateChanges++
		cb.metrics.LastStateChange = time.Now()
		if cb.logger != nil {
			cb.logger.Warnf("Circuit breaker opened due to high failure rate: %d failures out of %d requests",
				cb.metrics.FailedRequests, cb.metrics.TotalRequests)
		}
	}
}

// GetState 获取当前状态
func (cb *CircuitBreaker) GetState() CircuitBreakerState {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	return cb.state
}

// GetMetrics 获取熔断器指标
func (cb *CircuitBreaker) GetMetrics() *CircuitBreakerMetrics {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	// 返回指标的副本
	return &CircuitBreakerMetrics{
		TotalRequests:   atomic.LoadInt64(&cb.metrics.TotalRequests),
		FailedRequests:  atomic.LoadInt64(&cb.metrics.FailedRequests),
		SuccessRequests: atomic.LoadInt64(&cb.metrics.SuccessRequests),
		LastFailureTime: cb.metrics.LastFailureTime,
		LastSuccessTime: cb.metrics.LastSuccessTime,
		StateChanges:    cb.metrics.StateChanges,
		LastStateChange: cb.metrics.LastStateChange,
		HalfOpenCalls:   cb.metrics.HalfOpenCalls,
	}
}

// Reset 重置熔断器
func (cb *CircuitBreaker) Reset() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.state = CircuitClosed
	cb.metrics = &CircuitBreakerMetrics{}
	cb.windowStart = time.Now()
	if cb.logger != nil {
		cb.logger.Info("Circuit breaker reset to closed state")
	}
}

// String 返回熔断器状态字符串
func (cb *CircuitBreaker) String() string {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	switch cb.state {
	case CircuitClosed:
		return "CLOSED"
	case CircuitOpen:
		return "OPEN"
	case CircuitHalfOpen:
		return "HALF_OPEN"
	default:
		return "UNKNOWN"
	}
}

// updateCircuitBreaker 更新连接的熔断器状态
func (pc *PoolConnection) updateCircuitBreaker(success bool) {
	if pc.circuitBreaker == nil {
		pc.circuitBreaker = NewCircuitBreaker(nil, pc.logger)
	}

	if success {
		pc.circuitBreaker.RecordSuccess()
		pc.circuitState = pc.circuitBreaker.GetState()
	} else {
		pc.circuitBreaker.RecordFailure()
		pc.circuitState = pc.circuitBreaker.GetState()
	}
}
