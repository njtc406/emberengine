// Package mailbox
// @Title  中间件链
// @Description  实现洋葱模型的中间件链
// @Author  yr  2025/2/8
// @Update  yr  2025/2/8
package mailbox

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
)

// ============================================================================
// 中间件上下文实现
// ============================================================================

// MiddlewareContext 是 IMiddlewareContext 的默认实现。
//
// 并发契约：
//   - OnReceive 阶段：在调用方调用 PostJob 的 goroutine 内串行执行；
//   - OnComplete 阶段：在 worker goroutine 中串行执行；
//   - 跨阶段之间的 happens-before 由 mpsc 队列的 atomic 操作建立。
//
// 因此 data map 不存在并发访问场景，顶层不再为它加 RWMutex——
// 面向热路径上的 N 个中间件可以省下 2N 次 atomic 读写开销。
// 若业务中间件确有跨 goroutine 共享需求，请自行使用 sync.Map 封装。
type MiddlewareContext struct {
	ctx                context.Context
	job                inf.IMailboxJob
	serviceName        string
	startTime          time.Time
	executed           atomic.Int32
	middlewareSnapshot []inf.IMailboxMiddleware // OnReceive 时快照
	data               map[string]interface{}
}

// NewMiddlewareContext 创建中间件上下文
func NewMiddlewareContext(ctx context.Context, job inf.IMailboxJob, serviceName string) *MiddlewareContext {
	return &MiddlewareContext{
		ctx:         ctx,
		job:         job,
		serviceName: serviceName,
		startTime:   time.Now(),
		data:        make(map[string]interface{}, 8), // 增加初始容量以减少扩容
	}
}

func (c *MiddlewareContext) Context() context.Context {
	return c.ctx
}

func (c *MiddlewareContext) Job() inf.IMailboxJob {
	return c.job
}

func (c *MiddlewareContext) ServiceName() string {
	return c.serviceName
}

func (c *MiddlewareContext) Set(key string, value interface{}) {
	c.data[key] = value
}

func (c *MiddlewareContext) Get(key string) (interface{}, bool) {
	v, ok := c.data[key]
	return v, ok
}

func (c *MiddlewareContext) GetString(key string) string {
	if v, ok := c.Get(key); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func (c *MiddlewareContext) GetInt(key string) int {
	if v, ok := c.Get(key); ok {
		if i, ok := v.(int); ok {
			return i
		}
	}
	return 0
}

func (c *MiddlewareContext) GetBool(key string) bool {
	if v, ok := c.Get(key); ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

func (c *MiddlewareContext) StartTime() time.Time {
	return c.startTime
}

func (c *MiddlewareContext) Elapsed() time.Duration {
	return time.Since(c.startTime)
}

// ============================================================================
// 中间件链实现
// ============================================================================

// MiddlewareChainOption 配置选项，仅在 NewMiddlewareChain 构造时使用。
type MiddlewareChainOption func(*MiddlewareChain)

// WithPanicHandler 设置中间件 panic 恢复回调。
// 必须在构造时传入，构造完成后不可变更（消除运行时 data race 风险）。
func WithPanicHandler(handler func(phase, middleware string, mctx inf.IMiddlewareContext, panicVal interface{})) MiddlewareChainOption {
	return func(c *MiddlewareChain) {
		c.panicHandler = handler
	}
}

// MiddlewareChain 是 IMiddlewareChain 的默认实现
// 使用 atomic.Pointer + Copy-on-Write 实现中间件列表管理，
// 消除热路径上的 RWMutex 开销。
type MiddlewareChain struct {
	mws          atomic.Pointer[[]inf.IMailboxMiddleware] // COW 列表
	mu           sync.Mutex                               // 仅保护写操作（Add/Remove）
	ctxPool      pool.IPool[*MiddlewareContext]           // 实例级池，per-Service 独立
	panicHandler func(phase, middleware string, mctx inf.IMiddlewareContext, panicVal interface{})
}

type frameworkCleanupMiddleware interface {
	OnFrameworkCleanup(mctx inf.IMiddlewareContext, err error, panicVal interface{})
}

// NewMiddlewareChain 创建中间件链。
// opts 用于传入构造期配置（如 WithPanicHandler），构造完成后不可变更。
func NewMiddlewareChain(middlewares []inf.IMailboxMiddleware, opts ...MiddlewareChainOption) *MiddlewareChain {
	c := &MiddlewareChain{}
	for _, opt := range opts {
		opt(c)
	}
	mws := make([]inf.IMailboxMiddleware, len(middlewares))
	copy(mws, middlewares)
	c.mws.Store(&mws)
	c.ctxPool = pool.NewSyncPoolWrapper[*MiddlewareContext](
		func() *MiddlewareContext {
			return &MiddlewareContext{
				data: make(map[string]interface{}, 8),
			}
		},
		pool.NewNoStatsRecorder(),
		pool.WithReset[*MiddlewareContext](func(mc *MiddlewareContext) {
			mc.ctx = nil
			mc.job = nil
			mc.serviceName = ""
			mc.executed.Store(0)
			mc.middlewareSnapshot = nil
			mc.startTime = time.Time{}
			// sync.Pool Put/Get 已提供 happens-before，无需 mutex。
			// 如果 map 扩容过大，直接重建以释放底层哈希桶内存
			const maxRetainKeys = 64
			if len(mc.data) > maxRetainKeys {
				mc.data = make(map[string]interface{}, 8)
			} else {
				for k := range mc.data {
					delete(mc.data, k)
				}
			}
		}),
	)
	return c
}

func (c *MiddlewareChain) handlePanic(phase string, middleware inf.IMailboxMiddleware, mctx inf.IMiddlewareContext, panicVal interface{}) {
	if c.panicHandler == nil {
		return
	}
	name := "<nil>"
	if middleware != nil {
		name = middleware.Name()
	}
	c.panicHandler(phase, name, mctx, panicVal)
}

// Add 添加中间件（COW：创建新切片存储）
func (c *MiddlewareChain) Add(middleware inf.IMailboxMiddleware) {
	c.mu.Lock()
	defer c.mu.Unlock()
	old := *c.mws.Load()
	newSlice := make([]inf.IMailboxMiddleware, len(old)+1)
	copy(newSlice, old)
	newSlice[len(old)] = middleware
	c.mws.Store(&newSlice)
}

// Remove 按名称移除中间件（COW：创建新切片存储）
func (c *MiddlewareChain) Remove(name string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	old := *c.mws.Load()
	for i, m := range old {
		if m.Name() == name {
			newSlice := make([]inf.IMailboxMiddleware, 0, len(old)-1)
			newSlice = append(newSlice, old[:i]...)
			newSlice = append(newSlice, old[i+1:]...)
			c.mws.Store(&newSlice)
			return true
		}
	}
	return false
}

// ExecuteOnReceive 执行所有中间件的 OnReceive
func (c *MiddlewareChain) ExecuteOnReceive(job inf.IMailboxJob, serviceName string) (dto.MiddlewareResult, inf.IMiddlewareContext) {
	middlewares := *c.mws.Load() // 无锁读取

	// 快速路径：无中间件时不创建 Context
	if len(middlewares) == 0 {
		return dto.Continue(), nil
	}

	mctx := c.ctxPool.Get()
	mctx.ctx = job.GetContext()
	mctx.job = job
	mctx.serviceName = serviceName
	mctx.startTime = time.Now()

	// 保存快照，保证 OnComplete 使用同一份中间件列表
	mctx.middlewareSnapshot = middlewares

	for i, m := range middlewares {
		result, panicVal := c.safeOnReceive(m, mctx)
		if panicVal != nil {
			mctx.executed.Store(int32(i + 1))
			return dto.Reject(fmt.Errorf("mailbox middleware %s OnReceive panic: %v", m.Name(), panicVal)), mctx
		}
		switch result.Action {
		case def.ActionReject:
			mctx.executed.Store(int32(i + 1))
			return result, mctx
		case def.ActionSkip:
			// 仅执行到当前中间件为止（包含当前），跳过剩余中间件。
			mctx.executed.Store(int32(i + 1))
			return dto.Continue(), mctx
		case def.ActionContinue:
			continue
		}
	}
	mctx.executed.Store(int32(len(middlewares)))
	return dto.Continue(), mctx
}

func (c *MiddlewareChain) safeOnReceive(m inf.IMailboxMiddleware, mctx inf.IMiddlewareContext) (result dto.MiddlewareResult, panicVal interface{}) {
	defer func() {
		if r := recover(); r != nil {
			panicVal = r
			c.handlePanic("OnReceive", m, mctx, r)
			result = dto.Reject(fmt.Errorf("mailbox middleware panic: %v", r))
		}
	}()
	return m.OnReceive(mctx), nil
}

// ExecuteOnComplete 执行所有中间件的 OnComplete（逆序）
func (c *MiddlewareChain) ExecuteOnComplete(mctx inf.IMiddlewareContext, err error, panicVal interface{}) {
	if mctx == nil {
		return // 快速路径：无中间件时 mctx 为 nil
	}
	var middlewares []inf.IMailboxMiddleware
	mc, ok := mctx.(*MiddlewareContext)
	if ok && mc.middlewareSnapshot != nil {
		// 使用 OnReceive 时的同一份快照
		middlewares = mc.middlewareSnapshot
	} else {
		middlewares = *c.mws.Load()
	}
	// 仅对执行过 OnReceive 的中间件执行 OnComplete（洋葱模型）。
	end := len(middlewares)
	if ok {
		if n := int(mc.executed.Load()); n >= 0 && n <= len(middlewares) {
			end = n
		}
	}
	for i := end - 1; i >= 0; i-- {
		c.safeOnComplete(middlewares[i], mctx, err, panicVal)
	}

	// 归还到池
	if ok {
		c.ctxPool.Put(mc)
	}
}

func (c *MiddlewareChain) safeOnComplete(m inf.IMailboxMiddleware, mctx inf.IMiddlewareContext, err error, panicVal interface{}) {
	defer func() {
		if r := recover(); r != nil {
			c.handlePanic("OnComplete", m, mctx, r)
		}
	}()
	m.OnComplete(mctx, err, panicVal)
}

// ExecuteFrameworkCleanup 只执行中间件声明的框架资源清理，不调用普通 OnComplete。
// 用于 rwUnsafe drain 路径：避免业务中间件回写共享状态，同时释放 Sentinel entry 等框架资源。
func (c *MiddlewareChain) ExecuteFrameworkCleanup(mctx inf.IMiddlewareContext, err error, panicVal interface{}) {
	if mctx == nil {
		return
	}
	var middlewares []inf.IMailboxMiddleware
	mc, ok := mctx.(*MiddlewareContext)
	if ok && mc.middlewareSnapshot != nil {
		middlewares = mc.middlewareSnapshot
	} else {
		middlewares = *c.mws.Load()
	}
	end := len(middlewares)
	if ok {
		if n := int(mc.executed.Load()); n >= 0 && n <= len(middlewares) {
			end = n
		}
	}
	for i := end - 1; i >= 0; i-- {
		cleanup, cleanupOK := middlewares[i].(frameworkCleanupMiddleware)
		if !cleanupOK {
			continue
		}
		c.safeFrameworkCleanup(cleanup, middlewares[i], mctx, err, panicVal)
	}
	if ok {
		c.ctxPool.Put(mc)
	}
}

func (c *MiddlewareChain) safeFrameworkCleanup(cleanup frameworkCleanupMiddleware, m inf.IMailboxMiddleware, mctx inf.IMiddlewareContext, err error, panicVal interface{}) {
	defer func() {
		if r := recover(); r != nil {
			c.handlePanic("OnFrameworkCleanup", m, mctx, r)
		}
	}()
	cleanup.OnFrameworkCleanup(mctx, err, panicVal)
}

// Start 启动所有中间件
func (c *MiddlewareChain) Start() {
	middlewares := *c.mws.Load()
	for _, m := range middlewares {
		c.safeOnStart(m)
	}
}

// Stop 停止所有中间件
func (c *MiddlewareChain) Stop() {
	middlewares := *c.mws.Load()
	// 逆序停止
	for i := len(middlewares) - 1; i >= 0; i-- {
		c.safeOnStop(middlewares[i])
	}
}

func (c *MiddlewareChain) safeOnStart(m inf.IMailboxMiddleware) {
	defer func() {
		if r := recover(); r != nil {
			c.handlePanic("OnStart", m, nil, r)
		}
	}()
	m.OnStart()
}

func (c *MiddlewareChain) safeOnStop(m inf.IMailboxMiddleware) {
	defer func() {
		if r := recover(); r != nil {
			c.handlePanic("OnStop", m, nil, r)
		}
	}()
	m.OnStop()
}

// Middlewares 获取中间件列表（用于调试）
func (c *MiddlewareChain) Middlewares() []inf.IMailboxMiddleware {
	old := *c.mws.Load()
	result := make([]inf.IMailboxMiddleware, len(old))
	copy(result, old)
	return result
}
