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
	idleCount atomic.Int32

	// 是否存在待处理工作，由 Wake 设置，Idle 看到一次后立即清零（一次性标志）
	hasWork atomic.Bool

	// 底层退避策略
	bo *ExponentialBackoff

	mu       sync.Mutex
	cond     *sync.Cond
	notified bool
}

// NewController 创建一个 Controller。
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
	c.idleCount.Store(0)
	// 由 Idle 在看到 hasWork 时清零，这里不强制修改 hasWork
	c.bo.Reset()
}

// Idle 轮询无任务时调用
func (c *Controller) Idle() {
	if c == nil {
		return
	}

	// fast-path：如有工作标志，立即消费并返回，避免进入退避
	if c.hasWork.Load() {
		// 一次性消费 hasWork 标志，防止长时间忙轮询
		c.hasWork.Store(false)
		c.idleCount.Store(0)
		return
	}

	// 增加空闲计数
	count := c.idleCount.Add(1)

	// 低空闲阶段：使用 backoff sleep（保持活跃）
	if count < c.condThreshold {
		time.Sleep(c.bo.NextDelay())
		return
	}

	// 未启用 cond，则持续 backoff sleep
	if !c.enableCond {
		time.Sleep(c.bo.NextDelay())
		return
	}

	// cond 路径：在锁内检查 hasWork/notified，避免丢失唤醒
	c.mu.Lock()
	defer c.mu.Unlock()

	// 再次检查一次工作标志与通知标志
	if c.hasWork.Load() || c.notified {
		c.hasWork.Store(false)
		c.notified = false
		c.idleCount.Store(0)
		return
	}

	for !c.notified && !c.hasWork.Load() {
		c.cond.Wait()
	}
	// 被唤醒后消费一次通知与工作标志
	c.hasWork.Store(false)
	c.notified = false
	c.idleCount.Store(0)
}

// Wake 有新任务到来时调用（producer 在 push 后调用）
func (c *Controller) Wake() {
	if c == nil {
		return
	}
	// 设置 hasWork，让 Idle 在进入 wait 前后都能感知
	c.hasWork.Store(true)
	c.idleCount.Store(0)
	c.bo.Reset()

	if !c.enableCond {
		return
	}

	c.mu.Lock()
	c.notified = true
	c.cond.Broadcast()
	c.mu.Unlock()
}

// AdaptiveController 自适应空闲控制器
type AdaptiveController struct {
	maxIdleBeforeCond int32 // 超过这个连续空闲次数启用 cond park
	enableCond        bool

	idleCount atomic.Int32
	bo        *ExponentialBackoff

	mu       sync.Mutex
	cond     *sync.Cond
	notified bool // 防止 lost wakeup：Wake 在 Idle 进入 Wait 之前调用时保留信号
}

// NewAdaptiveController 创建自适应空闲控制器
func NewAdaptiveController(enableCond bool, baseDelay, maxDelay time.Duration, maxIdleBeforeCond, maxRetries int) *AdaptiveController {
	if baseDelay <= 0 {
		baseDelay = time.Microsecond
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
	c.idleCount.Store(0)
	c.bo.Reset()
}

// Idle 空闲轮询
func (c *AdaptiveController) Idle() {
	count := c.idleCount.Add(1)

	// 高频阶段：直接重试，不睡眠（符合“前N次空闲不睡眠”设计）
	if count < c.maxIdleBeforeCond {
		return
	}

	// 未启用 cond：使用 backoff sleep
	if !c.enableCond {
		time.Sleep(c.bo.NextDelay())
		return
	}

	// 超过阈值，启用 cond park（在锁内检查 notified，避免丢失唤醒）
	c.mu.Lock()
	for !c.notified {
		c.cond.Wait()
	}
	c.notified = false
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
	c.mu.Lock()
	c.notified = true
	c.cond.Broadcast()
	c.mu.Unlock()
	c.Reset()
}
