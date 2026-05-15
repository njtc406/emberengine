package pool

import (
	"sync"
	"sync/atomic"
	"time"

	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
)

// ConnectionState 连接状态
type ConnectionState int32

const (
	StateIdle ConnectionState = iota
	StateActive
	StateUnhealthy
	StateClosed
)

// ConnectionMetrics 连接指标
type ConnectionMetrics struct {
	TotalRequests    int64     `json:"total_requests"`
	SuccessRequests  int64     `json:"success_requests"`
	FailedRequests   int64     `json:"failed_requests"`
	AvgResponseTime  int64     `json:"avg_response_time_ns"`
	LastActiveTime   time.Time `json:"last_active_time"`
	CreateTime       time.Time `json:"create_time"`
	HealthCheckCount int64     `json:"health_check_count"`
	ConsecutiveFails int64     `json:"consecutive_fails"`
}

// PoolConnection 增强连接
type PoolConnection struct {
	ID             string             `json:"id"`
	Sender         inf.IRpcSender     `json:"-"`
	State          ConnectionState    `json:"state"`
	Metrics        *ConnectionMetrics `json:"metrics"`
	LastUsed       time.Time          `json:"last_used"`
	logger         log.ILoggerX       `json:"-"`
	mu             sync.RWMutex       `json:"-"`
	circuitState   CircuitBreakerState
	circuitBreaker *CircuitBreaker `json:"-"`
}

// NewPoolConnection 创建新连接
func NewPoolConnection(id string, sender inf.IRpcSender, logger log.ILoggerX) *PoolConnection {
	now := time.Now()
	return &PoolConnection{
		ID:     id,
		Sender: sender,
		State:  StateIdle,
		logger: logger,
		Metrics: &ConnectionMetrics{
			CreateTime:     now,
			LastActiveTime: now,
		},
		LastUsed:     now,
		circuitState: CircuitClosed,
	}
}

// IsHealthy 检查连接是否健康
func (pc *PoolConnection) IsHealthy() bool {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	return pc.State != StateUnhealthy &&
		pc.State != StateClosed &&
		pc.circuitState != CircuitOpen &&
		!pc.Sender.IsClosed()
}

// UpdateMetrics 更新连接指标
func (pc *PoolConnection) UpdateMetrics(success bool, responseTime time.Duration) {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	atomic.AddInt64(&pc.Metrics.TotalRequests, 1)
	if success {
		atomic.AddInt64(&pc.Metrics.SuccessRequests, 1)
		atomic.StoreInt64(&pc.Metrics.ConsecutiveFails, 0)
		pc.updateCircuitBreaker(true)
	} else {
		atomic.AddInt64(&pc.Metrics.FailedRequests, 1)
		atomic.AddInt64(&pc.Metrics.ConsecutiveFails, 1)
		pc.updateCircuitBreaker(false)
	}

	// 更新平均响应时间
	total := atomic.LoadInt64(&pc.Metrics.TotalRequests)
	if total > 0 {
		currentAvg := atomic.LoadInt64(&pc.Metrics.AvgResponseTime)
		newAvg := (currentAvg*(total-1) + responseTime.Nanoseconds()) / total
		atomic.StoreInt64(&pc.Metrics.AvgResponseTime, newAvg)
	}

	pc.Metrics.LastActiveTime = time.Now()
	pc.LastUsed = time.Now()
}

// SetState 设置连接状态
func (pc *PoolConnection) SetState(state ConnectionState) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	pc.State = state
}

// GetState 获取连接状态
func (pc *PoolConnection) GetState() ConnectionState {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	return pc.State
}

// PoolConfig 连接池配置
type PoolConfig struct {
	// 基础配置
	MinConnections     int `json:"min_connections"`     // 最小连接数
	MaxConnections     int `json:"max_connections"`     // 最大连接数
	InitialConnections int `json:"initial_connections"` // 初始连接数

	// 扩缩容配置
	ScaleUpThreshold   float64       `json:"scale_up_threshold"`   // 扩容阈值(请求成功率)
	ScaleDownThreshold float64       `json:"scale_down_threshold"` // 缩容阈值
	ScaleUpCooldown    time.Duration `json:"scale_up_cooldown"`    // 扩容冷却时间
	ScaleDownCooldown  time.Duration `json:"scale_down_cooldown"`  // 缩容冷却时间

	// 健康检查配置
	HealthCheckInterval time.Duration `json:"health_check_interval"` // 健康检查间隔
	HealthCheckTimeout  time.Duration `json:"health_check_timeout"`  // 健康检查超时
	MaxConsecutiveFails int64         `json:"max_consecutive_fails"` // 最大连续失败次数

	// 连接生命周期配置
	MaxIdleTime       time.Duration `json:"max_idle_time"`      // 最大空闲时间
	MaxConnectionAge  time.Duration `json:"max_connection_age"` // 连接最大年龄
	ConnectionTimeout time.Duration `json:"connection_timeout"` // 连接超时

	// 负载均衡配置
	LoadBalanceStrategy string `json:"load_balance_strategy"` // 负载均衡策略
}

// DefaultPoolConfig 默认配置
func DefaultPoolConfig() *PoolConfig {
	return &PoolConfig{
		MinConnections:      2,
		MaxConnections:      20,
		InitialConnections:  4,
		ScaleUpThreshold:    0.8, // 80%负载时扩容
		ScaleDownThreshold:  0.3, // 30%负载时缩容
		ScaleUpCooldown:     30 * time.Second,
		ScaleDownCooldown:   60 * time.Second,
		HealthCheckInterval: 30 * time.Second,
		HealthCheckTimeout:  5 * time.Second,
		MaxConsecutiveFails: 3,
		MaxIdleTime:         5 * time.Minute,
		MaxConnectionAge:    30 * time.Minute,
		ConnectionTimeout:   10 * time.Second,
		LoadBalanceStrategy: "round_robin",
	}
}

// PoolMetrics 连接池指标快照。
//
// 所有字段由 ConnectionPool.updatePoolMetrics() 在 connMutex.RLock 下计算，
// GetMetrics() 返回的是指针——调用方应在读取后尽快复制或序列化，不应跨
// goroutine 长期持有。
//
// 字段单位约定：
//   - 计数类 (TotalRequests 等)：累计值，单调递增
//   - 时间类 (AvgResponseTime)：纳秒
//   - 比率类 (SuccessRate)：0.0 ~ 1.0
type PoolMetrics struct {
	TotalConnections   int32     `json:"total_connections"`     // 当前连接总数（含所有状态）
	ActiveConnections  int32     `json:"active_connections"`    // 处于 StateActive 的连接数
	IdleConnections    int32     `json:"idle_connections"`      // 处于 StateIdle 的连接数
	UnhealthyConns     int32     `json:"unhealthy_connections"` // 处于 StateUnhealthy 的连接数
	TotalRequests      int64     `json:"total_requests"`        // 累计请求总数
	SuccessfulRequests int64     `json:"successful_requests"`   // 累计成功请求数
	FailedRequests     int64     `json:"failed_requests"`       // 累计失败请求数
	AvgResponseTime    int64     `json:"avg_response_time_ns"`  // 所有连接平均响应时间（纳秒）
	SuccessRate        float64   `json:"success_rate"`          // 请求成功率 [0.0, 1.0]
	LastScaleTime      time.Time `json:"last_scale_time"`       // 最近一次扩缩容操作时间
	ScaleOperations    int64     `json:"scale_operations"`      // 累计扩缩容操作次数
}
