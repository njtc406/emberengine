// Package mailbox
// 模块名: RW 读写分离控制器
// 功能描述: 从 WorkerPool 中提取的读写分离状态管理，负责 RW 模式的开关、锁、信号量和可观测性指标
// 作者:  yr  2025/7/19 0019
// 最后更新:  yr  2025/7/19 0019
package mailbox

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/asynclib"
	"github.com/njtc406/emberengine/engine/pkg/utils/idle"
	"github.com/panjf2000/ants/v2"
)

// RWController 管理 RW 读写分离的全部状态。
// 由 WorkerPool 持有，Worker 通过引用访问。
type RWController struct {
	enabled        atomic.Bool   // 是否启用 RW 模式（atomic：支持运行时动态开关）
	mu             sync.RWMutex  // 全 Service 共享读写锁，所有 Worker 引用
	writeRequested atomic.Int32  // 正在等待写锁的 Writer 计数，读路径检查 >0 时让步避免写饥饿
	readSem        chan struct{} // 全 Service 读并发信号量（nil = 不限制）
	stopTimeout    time.Duration // Stop 时等待 in-flight 读 goroutine 的最大时间
	maxJobExecTime time.Duration // Job 执行硬超时看门狗阈值（0=禁用）

	// 可观测性指标
	readTotal         atomic.Int64 // 累计读操作数
	writeTotal        atomic.Int64 // 累计写操作数
	drainDiscardTotal atomic.Int64 // Drain 阶段丢弃的 Job 数（per-job）
	unsafeDrainEvents atomic.Int64 // unsafe drain 事件计数（事件级，StopTimeout 超时进入不安全 drain）
	longJobTotal      atomic.Int64 // watchdog 触发次数（Job 执行超过 maxJobExecTime）

	// 读 goroutine 池（per-WorkerPool 独立池，资源隔离）
	readPool *asynclib.Pool // 仅在 EnableRWMode 时初始化，可为 nil
}

// newRWController 根据配置创建 RWController。
func newRWController(conf *config.MailboxConf) (*RWController, error) {
	rw := &RWController{
		stopTimeout: conf.StopTimeout,
	}

	rw.enabled.Store(conf.EnableRWMode)

	if conf.EnableRWMode && conf.MaxConcurrentReads > 0 {
		rw.readSem = make(chan struct{}, conf.MaxConcurrentReads)
	}
	if rw.enabled.Load() && rw.stopTimeout <= 0 {
		rw.stopTimeout = 10 * time.Second
	}
	if conf.MaxJobExecutionTime > 0 {
		rw.maxJobExecTime = conf.MaxJobExecutionTime
	}

	// 读 goroutine 池初始化
	if conf.EnableRWMode && conf.ReadPoolSize > 0 {
		rp, err := asynclib.NewPool(conf.ReadPoolSize, ants.WithNonblocking(true))
		if err != nil {
			return nil, err
		}
		rw.readPool = rp
	}

	return rw, nil
}

// IsEnabled 返回当前 RW 模式是否启用
func (rw *RWController) IsEnabled() bool {
	return rw.enabled.Load()
}

// EnabledPtr 返回 enabled 的指针，供 MethodMgr 等外部组件引用。
func (rw *RWController) EnabledPtr() *atomic.Bool {
	return &rw.enabled
}

// Disable 关闭 RW 模式（§10.14 安全协议）。
// 通过 rwMu.Lock() + RLock-after-check 协议保证切换窗口无数据竞争。
func (rw *RWController) Disable() error {
	if !rw.enabled.Load() {
		return nil
	}
	deadline := time.Now().Add(rw.stopTimeout)
	bo := idle.NewSpinBackoff(5 * time.Millisecond)
	for !rw.mu.TryLock() {
		if time.Now().After(deadline) {
			return ErrRWDisableTimeout
		}
		bo.Backoff()
	}
	// 持有 WLock 期间翻转标志——此刻无任何 goroutine 访问共享状态
	rw.enabled.Store(false)
	rw.mu.Unlock()
	return nil
}

// Enable 运行时启用 RW 模式。
// 调用前应确保 readSem 和 readPool 已初始化（由 WorkerPool.SetRWEnabled 协调）。
func (rw *RWController) Enable() {
	rw.enabled.Store(true)
}

// EnsureReadResources 确保读信号量和读池已初始化（运行时启用 RW 时调用）。
func (rw *RWController) EnsureReadResources(conf *config.MailboxConf, logger log.ILoggerX) {
	if rw.readSem == nil {
		maxReads := conf.MaxConcurrentReads
		if maxReads <= 0 {
			maxReads = runtime.NumCPU() * 4
			if maxReads > 64 {
				maxReads = 64
			}
		}
		rw.readSem = make(chan struct{}, maxReads)
		conf.MaxConcurrentReads = maxReads
	}
	if rw.readPool == nil {
		poolSize := conf.ReadPoolSize
		if poolSize <= 0 {
			poolSize = conf.MaxConcurrentReads
		}
		if poolSize > 0 {
			if rp, err := asynclib.NewPool(poolSize, ants.WithNonblocking(true)); err == nil {
				rw.readPool = rp
			} else if logger != nil {
				logger.Warnf("Failed to create read pool (size=%d): %v, will fallback to go func()", poolSize, err)
			}
		}
	}
}
