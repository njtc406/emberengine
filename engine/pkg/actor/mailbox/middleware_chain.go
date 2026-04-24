// Package mailbox
// @Title  中间件链
// @Description  实现洋葱模型的中间件链
// @Author  yr  2025/2/8
// @Update  yr  2025/2/8
package mailbox

import (
	"context"
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

// MiddlewareContext 是 IMiddlewareContext 的默认实现
type MiddlewareContext struct {
	ctx                context.Context
	job                inf.IMailboxJob
	serviceName        string
	startTime          time.Time
	executed           atomic.Int32
	middlewareSnapshot []inf.IMailboxMiddleware // OnReceive 时快照
	data               map[string]interface{}
	mu                 sync.RWMutex
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
	c.mu.Lock()
	c.data[key] = value
	c.mu.Unlock()
}

func (c *MiddlewareContext) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	v, ok := c.data[key]
	c.mu.RUnlock()
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

// MiddlewareChain 是 IMiddlewareChain 的默认实现
// 使用 atomic.Pointer + Copy-on-Write 实现中间件列表管理，
// 消除热路径上的 RWMutex 开销。
type MiddlewareChain struct {
	mws     atomic.Pointer[[]inf.IMailboxMiddleware] // COW 列表
	mu      sync.Mutex                               // 仅保护写操作（Add/Remove）
	ctxPool pool.IPool[*MiddlewareContext]           // 实例级池，per-Service 独立
}

// NewMiddlewareChain 创建中间件链
func NewMiddlewareChain(middlewares ...inf.IMailboxMiddleware) *MiddlewareChain {
	c := &MiddlewareChain{}
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
			mc.mu.Lock()
			// 如果 map 扩容过大，直接重建以释放底层哈希桶内存
			const maxRetainKeys = 64
			if len(mc.data) > maxRetainKeys {
				mc.data = make(map[string]interface{}, 8)
			} else {
				for k := range mc.data {
					delete(mc.data, k)
				}
			}
			mc.mu.Unlock()
		}),
	)
	return c
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
		result := m.OnReceive(mctx)
		switch result.Action {
		case def.ActionReject:
			mctx.executed.Store(int32(i + 1))
			return result, mctx
		case def.ActionSkip:
			// 仅执行到当前中间件为止（包含当前），跳过后续中间件。
			mctx.executed.Store(int32(i + 1))
			return dto.Continue(), mctx
		case def.ActionContinue:
			continue
		}
	}
	mctx.executed.Store(int32(len(middlewares)))
	return dto.Continue(), mctx
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
		middlewares[i].OnComplete(mctx, err, panicVal)
	}

	// 归还到池
	if ok {
		c.ctxPool.Put(mc)
	}
}

// ReturnContext 仅归还 mctx 到池，不执行任何中间件回调。
// 用于 rwUnsafe drain 路径：需要回收资源但不能安全执行 OnComplete。
func (c *MiddlewareChain) ReturnContext(mctx inf.IMiddlewareContext) {
	if mctx == nil {
		return
	}
	if mc, ok := mctx.(*MiddlewareContext); ok {
		c.ctxPool.Put(mc)
	}
}

// Start 启动所有中间件
func (c *MiddlewareChain) Start() {
	middlewares := *c.mws.Load()
	for _, m := range middlewares {
		m.OnStart()
	}
}

// Stop 停止所有中间件
func (c *MiddlewareChain) Stop() {
	middlewares := *c.mws.Load()
	// 逆序停止
	for i := len(middlewares) - 1; i >= 0; i-- {
		middlewares[i].OnStop()
	}
}

// Middlewares 获取中间件列表（用于调试）
func (c *MiddlewareChain) Middlewares() []inf.IMailboxMiddleware {
	old := *c.mws.Load()
	result := make([]inf.IMailboxMiddleware, len(old))
	copy(result, old)
	return result
}
