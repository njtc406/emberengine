// Package pool
// @Title  连接池管理器
// @Description  动态扩缩容连接池管理,支持健康监控和智能路由
// @Author  yr  2025/1/20
// @Update  yr  2025/1/20
package pool

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/log"

	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
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

// PoolMetrics 连接池指标
type PoolMetrics struct {
	TotalConnections   int32     `json:"total_connections"`
	ActiveConnections  int32     `json:"active_connections"`
	IdleConnections    int32     `json:"idle_connections"`
	UnhealthyConns     int32     `json:"unhealthy_connections"`
	TotalRequests      int64     `json:"total_requests"`
	SuccessfulRequests int64     `json:"successful_requests"`
	FailedRequests     int64     `json:"failed_requests"`
	AvgResponseTime    int64     `json:"avg_response_time_ns"`
	SuccessRate        float64   `json:"success_rate"`
	LastScaleTime      time.Time `json:"last_scale_time"`
	ScaleOperations    int64     `json:"scale_operations"`
}

// ConnectionPool 增强连接池
type ConnectionPool struct {
	config  *PoolConfig
	address string
	rpcType string
	creator func(addr string) inf.IRpcSender
	logger  log.ILoggerX

	connections map[string]*PoolConnection
	connMutex   sync.RWMutex

	metrics *PoolMetrics

	// 负载均衡
	roundRobin int64

	// 扩缩容控制
	lastScaleUp   time.Time
	lastScaleDown time.Time
	scaleMutex    sync.Mutex

	// 健康检查
	healthTicker *time.Ticker
	stopHealth   chan struct{}

	// 生命周期管理
	cleanupTicker *time.Ticker
	stopCleanup   chan struct{}

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewConnectionPool 创建新连接池
func NewConnectionPool(address, rpcType string, creator func(addr string) inf.IRpcSender, config *PoolConfig, logger log.ILoggerX) *ConnectionPool {
	if config == nil {
		config = DefaultPoolConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	pool := &ConnectionPool{
		config:      config,
		address:     address,
		rpcType:     rpcType,
		creator:     creator,
		logger:      logger,
		connections: make(map[string]*PoolConnection),
		metrics:     &PoolMetrics{},
		stopHealth:  make(chan struct{}),
		stopCleanup: make(chan struct{}),
		ctx:         ctx,
		cancel:      cancel,
	}

	return pool
}

// Start 启动连接池
func (cp *ConnectionPool) Start() error {
	// 创建初始连接
	for i := 0; i < cp.config.InitialConnections; i++ {
		if err := cp.createConnection(); err != nil {
			if cp.logger != nil {
				cp.logger.Errorf("Failed to create initial connection %d: %v", i, err)
			}
			// 继续创建其他连接
		}
	}

	// 启动健康检查
	cp.healthTicker = time.NewTicker(cp.config.HealthCheckInterval)
	cp.wg.Add(1)
	go cp.healthCheckLoop()

	// 启动连接清理
	cp.cleanupTicker = time.NewTicker(time.Minute) // 每分钟清理一次
	cp.wg.Add(1)
	go cp.cleanupLoop()

	if cp.logger != nil {
		cp.logger.Infof("Connection pool started for %s:%s with %d initial connections",
			cp.address, cp.rpcType, len(cp.connections))
	}

	return nil
}

// Stop 停止连接池
func (cp *ConnectionPool) Stop() {
	cp.cancel()

	if cp.healthTicker != nil {
		cp.healthTicker.Stop()
	}
	if cp.cleanupTicker != nil {
		cp.cleanupTicker.Stop()
	}

	close(cp.stopHealth)
	close(cp.stopCleanup)

	cp.wg.Wait()

	// 关闭所有连接
	cp.connMutex.Lock()
	for _, conn := range cp.connections {
		conn.Sender.Close()
	}
	cp.connections = make(map[string]*PoolConnection)
	cp.connMutex.Unlock()

	if cp.logger != nil {
		cp.logger.Infof("Connection pool stopped for %s:%s", cp.address, cp.rpcType)
	}
}

// GetConnection 获取连接
func (cp *ConnectionPool) GetConnection() (*PoolConnection, error) {
	cp.connMutex.RLock()

	// 找到健康的连接
	var healthyConns []*PoolConnection
	for _, conn := range cp.connections {
		if conn.IsHealthy() {
			healthyConns = append(healthyConns, conn)
		}
	}
	cp.connMutex.RUnlock()

	if len(healthyConns) == 0 {
		// 没有健康连接，尝试创建新连接
		if err := cp.scaleUp(1); err != nil {
			return nil, fmt.Errorf("no healthy connections available and failed to create new one: %w", err)
		}
		// 重新获取
		return cp.GetConnection()
	}

	// 负载均衡选择连接
	conn := cp.selectConnection(healthyConns)

	// 检查是否需要扩容
	cp.checkAndScale()

	return conn, nil
}

// createConnection 创建新连接
func (cp *ConnectionPool) createConnection() error {
	cp.connMutex.Lock()
	defer cp.connMutex.Unlock()

	if len(cp.connections) >= cp.config.MaxConnections {
		return fmt.Errorf("max connections limit reached: %d", cp.config.MaxConnections)
	}

	// 创建连接ID
	connID := fmt.Sprintf("%s_%s_%d_%d", cp.address, cp.rpcType, time.Now().UnixNano(), len(cp.connections))

	// 创建发送器
	sender := cp.creator(cp.address)
	if sender == nil {
		return fmt.Errorf("failed to create sender for %s", cp.address)
	}

	// 创建连接对象
	conn := NewPoolConnection(connID, sender, cp.logger)
	cp.connections[connID] = conn

	atomic.AddInt32(&cp.metrics.TotalConnections, 1)
	atomic.AddInt32(&cp.metrics.IdleConnections, 1)

	if cp.logger != nil {
		cp.logger.Debugf("Created new connection %s for %s:%s", connID, cp.address, cp.rpcType)
	}
	return nil
}

// selectConnection 负载均衡选择连接
func (cp *ConnectionPool) selectConnection(healthyConns []*PoolConnection) *PoolConnection {
	if len(healthyConns) == 0 {
		return nil
	}

	switch cp.config.LoadBalanceStrategy {
	case "round_robin":
		index := atomic.AddInt64(&cp.roundRobin, 1) % int64(len(healthyConns))
		return healthyConns[index]
	case "least_connections":
		// 选择请求数最少的连接
		var selected *PoolConnection
		minRequests := int64(^uint64(0) >> 1) // max int64
		for _, conn := range healthyConns {
			if conn.Metrics.TotalRequests < minRequests {
				minRequests = conn.Metrics.TotalRequests
				selected = conn
			}
		}
		return selected
	case "fastest_response":
		// 选择响应时间最快的连接
		var selected *PoolConnection
		minResponseTime := int64(^uint64(0) >> 1) // max int64
		for _, conn := range healthyConns {
			if conn.Metrics.AvgResponseTime < minResponseTime && conn.Metrics.TotalRequests > 0 {
				minResponseTime = conn.Metrics.AvgResponseTime
				selected = conn
			}
		}
		if selected != nil {
			return selected
		}
		// 如果没有找到，fallback到round robin
		fallthrough
	default:
		index := atomic.AddInt64(&cp.roundRobin, 1) % int64(len(healthyConns))
		return healthyConns[index]
	}
}

// checkAndScale 检查并执行扩缩容
func (cp *ConnectionPool) checkAndScale() {
	cp.scaleMutex.Lock()
	defer cp.scaleMutex.Unlock()

	now := time.Now()
	cp.connMutex.RLock()
	totalConns := len(cp.connections)
	activeConns := 0
	for _, conn := range cp.connections {
		if conn.State == StateActive {
			activeConns++
		}
	}
	cp.connMutex.RUnlock()

	if totalConns == 0 {
		return
	}

	// 计算负载率
	loadRate := float64(activeConns) / float64(totalConns)

	// 扩容检查
	if loadRate > cp.config.ScaleUpThreshold &&
		totalConns < cp.config.MaxConnections &&
		now.Sub(cp.lastScaleUp) > cp.config.ScaleUpCooldown {

		scaleCount := min(cp.config.MaxConnections-totalConns, max(1, totalConns/4))
		if err := cp.scaleUp(scaleCount); err == nil {
			cp.lastScaleUp = now
			atomic.AddInt64(&cp.metrics.ScaleOperations, 1)
			cp.metrics.LastScaleTime = now
			if cp.logger != nil {
				cp.logger.Infof("Scaled up connection pool for %s:%s by %d connections (load: %.2f)",
					cp.address, cp.rpcType, scaleCount, loadRate)
			}
		}
	}

	// 缩容检查
	if loadRate < cp.config.ScaleDownThreshold &&
		totalConns > cp.config.MinConnections &&
		now.Sub(cp.lastScaleDown) > cp.config.ScaleDownCooldown {

		scaleCount := min(totalConns-cp.config.MinConnections, max(1, totalConns/4))
		if err := cp.scaleDown(scaleCount); err == nil {
			cp.lastScaleDown = now
			atomic.AddInt64(&cp.metrics.ScaleOperations, 1)
			cp.metrics.LastScaleTime = now
			if cp.logger != nil {
				cp.logger.Infof("Scaled down connection pool for %s:%s by %d connections (load: %.2f)",
					cp.address, cp.rpcType, scaleCount, loadRate)
			}
		}
	}
}

// scaleUp 扩容
func (cp *ConnectionPool) scaleUp(count int) error {
	for i := 0; i < count; i++ {
		if err := cp.createConnection(); err != nil {
			return fmt.Errorf("failed to create connection %d/%d: %w", i+1, count, err)
		}
	}
	return nil
}

// scaleDown 缩容
func (cp *ConnectionPool) scaleDown(count int) error {
	cp.connMutex.Lock()
	defer cp.connMutex.Unlock()

	// 找到可以移除的连接(优先移除空闲时间最长的)
	var candidates []*PoolConnection
	for _, conn := range cp.connections {
		if conn.State == StateIdle {
			candidates = append(candidates, conn)
		}
	}

	// 按空闲时间排序，移除空闲时间最长的
	if len(candidates) == 0 {
		return fmt.Errorf("no idle connections to remove")
	}

	// 简单按时间排序
	for i := 0; i < len(candidates)-1; i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[i].LastUsed.After(candidates[j].LastUsed) {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	removeCount := min(count, len(candidates))
	for i := 0; i < removeCount; i++ {
		conn := candidates[i]
		conn.Sender.Close()
		delete(cp.connections, conn.ID)
		atomic.AddInt32(&cp.metrics.TotalConnections, -1)
		if conn.State == StateIdle {
			atomic.AddInt32(&cp.metrics.IdleConnections, -1)
		}
	}

	return nil
}

// healthCheckLoop 健康检查循环
func (cp *ConnectionPool) healthCheckLoop() {
	defer cp.wg.Done()

	for {
		select {
		case <-cp.ctx.Done():
			return
		case <-cp.stopHealth:
			return
		case <-cp.healthTicker.C:
			cp.performHealthCheck()
		}
	}
}

// performHealthCheck 执行健康检查
func (cp *ConnectionPool) performHealthCheck() {
	cp.connMutex.RLock()
	connections := make([]*PoolConnection, 0, len(cp.connections))
	for _, conn := range cp.connections {
		connections = append(connections, conn)
	}
	cp.connMutex.RUnlock()

	for _, conn := range connections {
		cp.healthCheckConnection(conn)
	}

	// 更新池指标
	cp.updatePoolMetrics()
}

// healthCheckConnection 检查单个连接健康状态
func (cp *ConnectionPool) healthCheckConnection(conn *PoolConnection) {
	conn.mu.Lock()
	defer conn.mu.Unlock()

	atomic.AddInt64(&conn.Metrics.HealthCheckCount, 1)

	// 检查连接是否已关闭
	if conn.Sender.IsClosed() {
		conn.State = StateClosed
		return
	}

	// 检查连续失败次数
	if atomic.LoadInt64(&conn.Metrics.ConsecutiveFails) > cp.config.MaxConsecutiveFails {
		conn.State = StateUnhealthy
		if cp.logger != nil {
			cp.logger.Warnf("Connection %s marked as unhealthy due to consecutive failures: %d",
				conn.ID, conn.Metrics.ConsecutiveFails)
		}
		return
	}

	// 检查空闲时间
	if time.Since(conn.LastUsed) > cp.config.MaxIdleTime {
		conn.State = StateIdle
	} else if conn.State != StateActive {
		conn.State = StateIdle
	}

	// 检查连接年龄
	if time.Since(conn.Metrics.CreateTime) > cp.config.MaxConnectionAge {
		if cp.logger != nil {
			cp.logger.Infof("Connection %s exceeded max age, marking for replacement", conn.ID)
		}
		// 可以在这里实现连接替换逻辑
	}
}

// cleanupLoop 清理循环
func (cp *ConnectionPool) cleanupLoop() {
	defer cp.wg.Done()

	for {
		select {
		case <-cp.ctx.Done():
			return
		case <-cp.stopCleanup:
			return
		case <-cp.cleanupTicker.C:
			cp.cleanupConnections()
		}
	}
}

// cleanupConnections 清理不健康的连接
func (cp *ConnectionPool) cleanupConnections() {
	cp.connMutex.Lock()
	defer cp.connMutex.Unlock()

	var toRemove []string
	for id, conn := range cp.connections {
		if conn.State == StateClosed ||
			(conn.State == StateUnhealthy && time.Since(conn.LastUsed) > time.Minute*5) {
			toRemove = append(toRemove, id)
		}
	}

	for _, id := range toRemove {
		conn := cp.connections[id]
		conn.Sender.Close()
		delete(cp.connections, id)
		atomic.AddInt32(&cp.metrics.TotalConnections, -1)
		if cp.logger != nil {
			cp.logger.Debugf("Cleaned up connection %s", id)
		}
	}

	// 确保最小连接数
	if len(cp.connections) < cp.config.MinConnections {
		needed := cp.config.MinConnections - len(cp.connections)
		for i := 0; i < needed; i++ {
			if err := cp.createConnection(); err != nil {
				if cp.logger != nil {
					cp.logger.Errorf("Failed to create replacement connection: %v", err)
				}
				break
			}
		}
	}
}

// updatePoolMetrics 更新池指标
func (cp *ConnectionPool) updatePoolMetrics() {
	cp.connMutex.RLock()
	defer cp.connMutex.RUnlock()

	var totalRequests, successRequests, failedRequests int64
	var totalResponseTime int64
	var activeConns, idleConns, unhealthyConns int32

	for _, conn := range cp.connections {
		totalRequests += atomic.LoadInt64(&conn.Metrics.TotalRequests)
		successRequests += atomic.LoadInt64(&conn.Metrics.SuccessRequests)
		failedRequests += atomic.LoadInt64(&conn.Metrics.FailedRequests)
		totalResponseTime += atomic.LoadInt64(&conn.Metrics.AvgResponseTime)

		switch conn.State {
		case StateActive:
			activeConns++
		case StateIdle:
			idleConns++
		case StateUnhealthy:
			unhealthyConns++
		}
	}

	cp.metrics.TotalConnections = int32(len(cp.connections))
	cp.metrics.ActiveConnections = activeConns
	cp.metrics.IdleConnections = idleConns
	cp.metrics.UnhealthyConns = unhealthyConns
	cp.metrics.TotalRequests = totalRequests
	cp.metrics.SuccessfulRequests = successRequests
	cp.metrics.FailedRequests = failedRequests

	if len(cp.connections) > 0 {
		cp.metrics.AvgResponseTime = totalResponseTime / int64(len(cp.connections))
	}

	if totalRequests > 0 {
		cp.metrics.SuccessRate = float64(successRequests) / float64(totalRequests)
	}
}

// GetMetrics 获取连接池指标
func (cp *ConnectionPool) GetMetrics() *PoolMetrics {
	cp.updatePoolMetrics()
	return cp.metrics
}

// 辅助函数
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
