package idle

import (
	"sync"
	"sync/atomic"
	"time"
)

// Controller 混合策略（fast-path sleep/backoff + long-idle cond park）
type Controller struct {
	// 连续空闲次数阈值：超过此值才考虑进入 cond park
	condThreshold int32

	// 启用 cond park（如果 false 则只使用 backoff sleep）
	enableCond bool

	// 当前连续空闲次数
	idleCount int32

	// 是否存在工作（由生产者先设置），fast-path 谓词
	hasWork atomic.Bool

	// 底层退避策略
	bo *ExponentialBackoff

	mu       sync.Mutex
	cond     *sync.Cond
	notified bool
}

// NewController
func NewController(enableCond bool, backoffBaseDelay, backoffMaxDelay time.Duration, condThreshold, backoffMaxRetries int) *Controller {
	if backoffBaseDelay <= 0 {
		backoffBaseDelay = 1
	}
	if condThreshold <= 0 {
		condThreshold = 1
	}
	c := &Controller{
		condThreshold: int32(condThreshold),
		enableCond:    enableCond,
		bo:            NewExponentialBackoff(backoffBaseDelay, backoffMaxDelay, backoffMaxRetries),
	}
	c.cond = sync.NewCond(&c.mu)
	return c
}

// Reset 在成功处理到任务时调用
func (c *Controller) Reset() {
	if c == nil {
		return
	}
	atomic.StoreInt32(&c.idleCount, 0)
	c.hasWork.Store(false)
	c.bo.Reset()
}

// Idle 轮询无任务时调用
func (c *Controller) Idle() {
	if c == nil {
		return
	}

	// fast-path: 如果有工作标识，直接返回（避免锁）
	if c.hasWork.Load() {
		// note: 不在这里调用 Reset(); 消费端会在处理消息时调用 Reset
		return
	}

	// 增加空闲计数
	count := atomic.AddInt32(&c.idleCount, 1)

	// 低空闲阶段：使用 backoff sleep（保持活跃）
	if count < c.condThreshold {
		time.Sleep(c.bo.NextDelay())
		return
	}

	// 到达 condThreshold，如果未启用 cond，则继续 backoff
	if !c.enableCond {
		time.Sleep(c.bo.NextDelay())
		return
	}

	// cond 路径：在锁内检查 hasWork 谓词以避免丢失唤醒/虚假唤醒
	c.mu.Lock()
	// 如果已经被唤醒（hasWork），直接消费
	if c.hasWork.Load() {
		// 清除 idleCount（由 Wake 已经做过，但再保险）
		atomic.StoreInt32(&c.idleCount, 0)
		c.mu.Unlock()
		return
	}
	// 如果 already notified, 也直接返回
	if c.notified {
		c.notified = false
		atomic.StoreInt32(&c.idleCount, 0)
		c.mu.Unlock()
		return
	}
	// 等待被通知（循环防虚假唤醒）
	for !c.notified && !c.hasWork.Load() {
		c.cond.Wait()
	}
	// consume notification
	c.notified = false
	atomic.StoreInt32(&c.idleCount, 0)
	c.mu.Unlock()
}

// Wake 有新任务到来时调用（producer 在 push 后调用）
func (c *Controller) Wake() {
	if c == nil {
		return
	}
	// 先设置 hasWork，使 Idle 在进入 wait 前能看到
	c.hasWork.Store(true)

	// 重置 idleCount 与 backoff（保证被唤醒后不进入长 backoff）
	atomic.StoreInt32(&c.idleCount, 0)
	c.bo.Reset()

	if !c.enableCond {
		return
	}

	// 在锁内设置 notified 并 Broadcast，避免丢失唤醒
	c.mu.Lock()
	c.notified = true
	c.cond.Broadcast()
	c.mu.Unlock()
}

// AdaptiveController 自适应空闲控制器
type AdaptiveController struct {
	maxIdleBeforeCond int32 // 超过这个连续空闲次数启用 cond park
	enableCond        bool

	idleCount int32
	bo        *ExponentialBackoff

	mu   sync.Mutex
	cond *sync.Cond
}

// NewAdaptiveController 创建自适应空闲控制器
func NewAdaptiveController(enableCond bool, baseDelay, maxDelay time.Duration, maxIdleBeforeCond, maxRetries int) *AdaptiveController {
	if baseDelay <= 0 {
		baseDelay = 1 * time.Microsecond
	}
	if maxIdleBeforeCond <= 0 {
		maxIdleBeforeCond = 1
	}

	c := &AdaptiveController{
		maxIdleBeforeCond: int32(maxIdleBeforeCond),
		enableCond:        enableCond,
		bo:                NewExponentialBackoff(baseDelay, maxDelay, maxRetries),
	}
	c.cond = sync.NewCond(&c.mu)
	return c
}

// Reset 成功处理到任务时调用
func (c *AdaptiveController) Reset() {
	atomic.StoreInt32(&c.idleCount, 0)
	c.bo.Reset()
}

// Idle 空闲轮询
func (c *AdaptiveController) Idle() {
	count := atomic.AddInt32(&c.idleCount, 1)

	// 仍在高频阶段，只用 backoff sleep
	if count < c.maxIdleBeforeCond || !c.enableCond {
		time.Sleep(c.bo.NextDelay())
		return
	}

	// 超过阈值，启用 cond park
	c.mu.Lock()
	c.cond.Wait()
	c.mu.Unlock()

	// 被唤醒后重置 idleCount
	c.Reset()
}

// Wake 外部唤醒
func (c *AdaptiveController) Wake() {
	if !c.enableCond {
		c.Reset()
		return
	}
	c.cond.Broadcast()
	c.Reset()
}
