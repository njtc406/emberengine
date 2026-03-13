package pool

import (
	"fmt"
	"sync/atomic"
	"time"
)

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

// Stop 停止连接池（幂等，可安全多次调用）
func (cp *ConnectionPool) Stop() {
	cp.stopOnce.Do(func() {
		if cp.cancel != nil {
			cp.cancel()
		}

		if cp.healthTicker != nil {
			cp.healthTicker.Stop()
		}
		if cp.cleanupTicker != nil {
			cp.cleanupTicker.Stop()
		}

		close(cp.stopHealth)
		close(cp.stopCleanup)
	})

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
	const maxRetries = 1
	for attempt := 0; attempt <= maxRetries; attempt++ {
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
			if attempt == maxRetries {
				return nil, fmt.Errorf("no healthy connections available after %d retries", maxRetries)
			}
			// 没有健康连接，尝试创建新连接
			if err := cp.scaleUp(1); err != nil {
				return nil, fmt.Errorf("no healthy connections available and failed to create new one: %w", err)
			}
			continue
		}

		// 负载均衡选择连接
		conn := cp.selectConnection(healthyConns)

		// 检查是否需要扩容
		cp.checkAndScale()

		return conn, nil
	}
	// unreachable, but satisfy compiler
	return nil, fmt.Errorf("no healthy connections available")
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
