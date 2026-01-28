// Package event
// @Title  Event Throttling and Rate Limiting System
// @Description  Provides comprehensive throttling mechanisms to prevent event storms
// @Author  AI Assistant  2025/8/28
// @Update  AI Assistant  2025/8/28
package event

import (
	"sync"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/def"
)

// ThrottleStrategy 限流策略
type ThrottleStrategy int

const (
	StrategyTokenBucket   ThrottleStrategy = iota // 令牌桶算法
	StrategyLeakyBucket                           // 漏桶算法
	StrategySlidingWindow                         // 滑动窗口算法
	StrategyFixedWindow                           // 固定窗口算法
)

// RateLimiter 速率限制器接口
type RateLimiter interface {
	Allow(eventType def.EventType) bool
	AllowN(eventType def.EventType, n int) bool
	Reserve(eventType def.EventType) Reservation
	Wait(eventType def.EventType) error
	GetStats(eventType def.EventType) *LimiterStats
}

// Reservation 预约结构
type Reservation struct {
	eventType def.EventType
	delay     time.Duration
	granted   bool
}

// LimiterStats 限制器统计信息
type LimiterStats struct {
	TotalRequests   int64     `json:"total_requests"`
	AllowedRequests int64     `json:"allowed_requests"`
	DroppedRequests int64     `json:"dropped_requests"`
	CurrentRate     float64   `json:"current_rate"`
	LastUpdate      time.Time `json:"last_update"`
}

// TokenBucketLimiter 令牌桶限制器
type TokenBucketLimiter struct {
	capacity   int64         // 桶容量
	tokens     int64         // 当前令牌数
	rate       int64         // 令牌生成速率 (tokens/second)
	lastRefill int64         // 上次填充时间 (纳秒)
	mu         sync.RWMutex  // 读写锁
	stats      *LimiterStats // 统计信息
}

// NewTokenBucketLimiter 创建令牌桶限制器
func NewTokenBucketLimiter(capacity, rate int64) *TokenBucketLimiter {
	now := time.Now()
	return &TokenBucketLimiter{
		capacity:   capacity,
		tokens:     capacity,
		rate:       rate,
		lastRefill: now.UnixNano(),
		stats: &LimiterStats{
			LastUpdate: now,
		},
	}
}

// Allow 检查是否允许单个事件
func (tbl *TokenBucketLimiter) Allow(eventType def.EventType) bool {
	return tbl.AllowN(eventType, 1)
}

// AllowN 检查是否允许N个事件
func (tbl *TokenBucketLimiter) AllowN(eventType def.EventType, n int) bool {
	tbl.mu.Lock()
	defer tbl.mu.Unlock()

	tbl.refill()

	tbl.stats.TotalRequests += int64(n)

	if tbl.tokens >= int64(n) {
		tbl.tokens -= int64(n)
		tbl.stats.AllowedRequests += int64(n)
		tbl.updateRate()
		return true
	}

	tbl.stats.DroppedRequests += int64(n)
	tbl.updateRate()
	return false
}

// Reserve 预约令牌
func (tbl *TokenBucketLimiter) Reserve(eventType def.EventType) Reservation {
	tbl.mu.Lock()
	defer tbl.mu.Unlock()

	tbl.refill()

	if tbl.tokens >= 1 {
		tbl.tokens--
		tbl.stats.AllowedRequests++
		return Reservation{
			eventType: eventType,
			delay:     0,
			granted:   true,
		}
	}

	// 计算需要等待的时间
	delay := time.Duration((1e9)/tbl.rate) * time.Nanosecond
	tbl.stats.DroppedRequests++

	return Reservation{
		eventType: eventType,
		delay:     delay,
		granted:   false,
	}
}

// Wait 等待令牌可用
func (tbl *TokenBucketLimiter) Wait(eventType def.EventType) error {
	reservation := tbl.Reserve(eventType)
	if reservation.granted {
		return nil
	}

	time.Sleep(reservation.delay)
	return nil
}

// GetStats 获取统计信息
func (tbl *TokenBucketLimiter) GetStats(eventType def.EventType) *LimiterStats {
	tbl.mu.RLock()
	defer tbl.mu.RUnlock()

	// 返回统计信息的副本
	return &LimiterStats{
		TotalRequests:   tbl.stats.TotalRequests,
		AllowedRequests: tbl.stats.AllowedRequests,
		DroppedRequests: tbl.stats.DroppedRequests,
		CurrentRate:     tbl.stats.CurrentRate,
		LastUpdate:      tbl.stats.LastUpdate,
	}
}

// refill 填充令牌
func (tbl *TokenBucketLimiter) refill() {
	now := time.Now().UnixNano()
	elapsed := now - tbl.lastRefill

	if elapsed > 0 {
		tokensToAdd := (elapsed * tbl.rate) / 1e9
		tbl.tokens = min(tbl.capacity, tbl.tokens+tokensToAdd)
		tbl.lastRefill = now
	}
}

// updateRate 更新当前速率
func (tbl *TokenBucketLimiter) updateRate() {
	now := time.Now()
	if tbl.stats.LastUpdate.IsZero() {
		tbl.stats.LastUpdate = now
		return
	}

	elapsed := now.Sub(tbl.stats.LastUpdate).Seconds()
	if elapsed > 0 {
		tbl.stats.CurrentRate = float64(tbl.stats.AllowedRequests) / elapsed
	}
	tbl.stats.LastUpdate = now
}

// SlidingWindowLimiter 滑动窗口限制器
type SlidingWindowLimiter struct {
	windowSize time.Duration
	limit      int64
	windows    map[int64]int64 // timestamp -> count
	mu         sync.RWMutex
	stats      *LimiterStats
}

// NewSlidingWindowLimiter 创建滑动窗口限制器
func NewSlidingWindowLimiter(windowSize time.Duration, limit int64) *SlidingWindowLimiter {
	return &SlidingWindowLimiter{
		windowSize: windowSize,
		limit:      limit,
		windows:    make(map[int64]int64),
		stats: &LimiterStats{
			LastUpdate: time.Now(),
		},
	}
}

// Allow 检查是否允许事件
func (swl *SlidingWindowLimiter) Allow(eventType def.EventType) bool {
	return swl.AllowN(eventType, 1)
}

// AllowN 检查是否允许N个事件
func (swl *SlidingWindowLimiter) AllowN(eventType def.EventType, n int) bool {
	swl.mu.Lock()
	defer swl.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-swl.windowSize)

	// 清理过期窗口
	swl.cleanup(windowStart)

	// 计算当前窗口内的总数
	total := swl.getCurrentCount()

	swl.stats.TotalRequests += int64(n)

	if total+int64(n) <= swl.limit {
		// 记录到当前时间窗口
		timestamp := now.Unix()
		swl.windows[timestamp] += int64(n)
		swl.stats.AllowedRequests += int64(n)
		swl.updateRate()
		return true
	}

	swl.stats.DroppedRequests += int64(n)
	swl.updateRate()
	return false
}

// Reserve 预约 (滑动窗口不支持预约)
func (swl *SlidingWindowLimiter) Reserve(eventType def.EventType) Reservation {
	if swl.Allow(eventType) {
		return Reservation{
			eventType: eventType,
			delay:     0,
			granted:   true,
		}
	}

	return Reservation{
		eventType: eventType,
		delay:     swl.windowSize,
		granted:   false,
	}
}

// Wait 等待可用
func (swl *SlidingWindowLimiter) Wait(eventType def.EventType) error {
	reservation := swl.Reserve(eventType)
	if reservation.granted {
		return nil
	}

	time.Sleep(reservation.delay)
	return nil
}

// GetStats 获取统计信息
func (swl *SlidingWindowLimiter) GetStats(eventType def.EventType) *LimiterStats {
	swl.mu.RLock()
	defer swl.mu.RUnlock()

	return &LimiterStats{
		TotalRequests:   swl.stats.TotalRequests,
		AllowedRequests: swl.stats.AllowedRequests,
		DroppedRequests: swl.stats.DroppedRequests,
		CurrentRate:     swl.stats.CurrentRate,
		LastUpdate:      swl.stats.LastUpdate,
	}
}

// cleanup 清理过期窗口
func (swl *SlidingWindowLimiter) cleanup(windowStart time.Time) {
	cutoff := windowStart.Unix()
	for timestamp := range swl.windows {
		if timestamp < cutoff {
			delete(swl.windows, timestamp)
		}
	}
}

// getCurrentCount 获取当前窗口内的计数
func (swl *SlidingWindowLimiter) getCurrentCount() int64 {
	var total int64
	for _, count := range swl.windows {
		total += count
	}
	return total
}

// updateRate 更新当前速率
func (swl *SlidingWindowLimiter) updateRate() {
	now := time.Now()
	if swl.stats.LastUpdate.IsZero() {
		swl.stats.LastUpdate = now
		return
	}

	elapsed := now.Sub(swl.stats.LastUpdate).Seconds()
	if elapsed > 0 {
		swl.stats.CurrentRate = float64(swl.stats.AllowedRequests) / elapsed
	}
	swl.stats.LastUpdate = now
}

// ThrottleManager 限流管理器
type ThrottleManager struct {
	limiters map[def.EventType]RateLimiter // 每个事件类型的限制器
	registry *EventRegistry                // 事件注册表
	mu       sync.RWMutex                  // 读写锁
}

// NewThrottleManager 创建限流管理器
func NewThrottleManager(registry *EventRegistry) *ThrottleManager {
	return &ThrottleManager{
		limiters: make(map[def.EventType]RateLimiter),
		registry: registry,
	}
}

// GetOrCreateLimiter 获取或创建限制器
func (tm *ThrottleManager) GetOrCreateLimiter(eventType def.EventType) RateLimiter {
	tm.mu.RLock()
	if limiter, exists := tm.limiters[eventType]; exists {
		tm.mu.RUnlock()
		return limiter
	}
	tm.mu.RUnlock()

	// 需要创建新的限制器
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// 双重检查
	if limiter, exists := tm.limiters[eventType]; exists {
		return limiter
	}

	// 根据事件分类创建适当的限制器
	classification := tm.registry.GetClassification(eventType)
	var limiter RateLimiter

	switch classification.Category {
	case CategorySystemCritical:
		// 系统关键事件使用令牌桶
		limiter = NewTokenBucketLimiter(int64(classification.MaxFrequency*2), int64(classification.MaxFrequency))
	case CategoryMetrics, CategoryStatistics:
		// 指标和统计事件使用滑动窗口
		limiter = NewSlidingWindowLimiter(time.Second, int64(classification.MaxFrequency))
	default:
		// 其他事件使用令牌桶
		limiter = NewTokenBucketLimiter(int64(classification.MaxFrequency), int64(classification.MaxFrequency))
	}

	tm.limiters[eventType] = limiter
	return limiter
}

// Allow 检查是否允许事件
func (tm *ThrottleManager) Allow(eventType def.EventType) bool {
	limiter := tm.GetOrCreateLimiter((eventType))
	return limiter.Allow(eventType)
}

// AllowN 检查是否允许N个事件
func (tm *ThrottleManager) AllowN(eventType def.EventType, n int) bool {
	limiter := tm.GetOrCreateLimiter(eventType)
	return limiter.AllowN(eventType, n)
}

// Wait 等待事件可用
func (tm *ThrottleManager) Wait(eventType def.EventType) error {
	limiter := tm.GetOrCreateLimiter(eventType)
	return limiter.Wait(eventType)
}

// GetStats 获取限流统计信息
func (tm *ThrottleManager) GetStats(eventType def.EventType) *LimiterStats {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if limiter, exists := tm.limiters[eventType]; exists {
		return limiter.GetStats(eventType)
	}

	return &LimiterStats{}
}

// GetAllStats 获取所有限制器的统计信息
func (tm *ThrottleManager) GetAllStats() map[def.EventType]*LimiterStats {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	stats := make(map[def.EventType]*LimiterStats)
	for eventType, limiter := range tm.limiters {
		stats[eventType] = limiter.GetStats(eventType)
	}

	return stats
}

// Reset 重置指定事件类型的限制器
func (tm *ThrottleManager) Reset(eventType def.EventType) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	delete(tm.limiters, eventType)
}

// ResetAll 重置所有限制器
func (tm *ThrottleManager) ResetAll() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.limiters = make(map[def.EventType]RateLimiter)
}

// min 返回较小值
func min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
