package idle

import (
	"sync"
	"sync/atomic"
	"time"
)

// Controller 封装了空闲等待策略：可以基于连续空闲次数选择忙等、sleep 或条件唤醒。
//
// 设计上是无锁、可被多个 goroutine 调用，但典型场景是单消费 goroutine 调用。
// 通过外部传入的 backoff 实例来控制基础退避时间，从而保证和原有逻辑兼容。
//
// 使用说明：
//   - 每当一次轮询发现“没有任务可处理”时调用一次 Idle()；
//   - 当有新任务入队时，可以调用 Wake() 来提前唤醒处于条件等待的 goroutine（若启用）。
//
// 当前实现先保留简单的基于 sleep 的退避，并预留 cond 结构，后续如有需要可在不改调用方
// 接口的情况下扩展为条件变量唤醒。

type Controller struct {
	// 连续空闲次数阈值，低于该值时仅做轻量 sleep/忙等，超过后使用指数退避。
	maxIdleBeforeBackoff int32

	// 是否启用条件变量（当前仅预留，暂不对外使用，避免引入复杂竞态）。
	enableCond bool

	// 当前连续空闲次数
	idleCount int32

	// 底层退避策略，与当前 mailbox 使用的 backoff 保持一致
	bo *ExponentialBackoff

	mu   sync.Mutex
	cond *sync.Cond
}

// NewController 创建一个空闲控制器。
// maxIdleBeforeBackoff <= 0 时会被归一化为 1。
func NewController(backoffBaseDelay, backoffMaxDelay time.Duration, maxIdleBeforeBackoff, backoffMaxRetries int) *Controller {
	if backoffBaseDelay <= 0 {
		backoffBaseDelay = 1
	}
	if maxIdleBeforeBackoff <= 0 {
		maxIdleBeforeBackoff = 1
	}
	c := &Controller{
		bo:                   NewExponentialBackoff(backoffBaseDelay, backoffMaxDelay, backoffMaxRetries),
		maxIdleBeforeBackoff: int32(maxIdleBeforeBackoff),
	}
	// 目前先不启用 cond，保持行为和原逻辑尽量接近（仅基于 sleep 的退避）。
	return c
}

// Reset 在成功处理到任务时调用，用于重置空闲计数和退避状态。
func (c *Controller) Reset() {
	if c == nil {
		return
	}
	atomic.StoreInt32(&c.idleCount, 0)
	c.bo.Reset()
}

// Idle 在一次“没有任务可处理”的轮询后调用。
// 内部根据连续空闲次数选择：
//   - 在未达到阈值前，直接按 backoff.NextDelay() sleep；
//   - 超过阈值后，继续使用 backoff.NextDelay()，并预留后续扩展为 cond.Wait 的空间。
//
// 该函数会阻塞当前 goroutine 一段时间，用于减少忙等开销。
func (c *Controller) Idle() {
	if c == nil {
		return
	}
	count := atomic.AddInt32(&c.idleCount, 1)
	if count < c.maxIdleBeforeBackoff {
		// 低空闲次数：保持较积极的处理，使用当前 backoff 延迟即可。
		time.Sleep(c.bo.NextDelay())
		return
	}

	// 高空闲次数：同样使用 backoff 延迟，未来可在此处切换为 cond.Wait 等更复杂策略。
	time.Sleep(c.bo.NextDelay())
}

// Wake 预留接口：将来启用条件变量时，用于从外部唤醒等待中的 goroutine。
// 当前实现只重置 idle 计数，不做实际唤醒逻辑，以避免引入竞态。
func (c *Controller) Wake() {
	if c == nil {
		return
	}
	atomic.StoreInt32(&c.idleCount, 0)
	// 未来如启用 cond，可在此处加广播逻辑
}
