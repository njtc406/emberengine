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
)

// ============================================================================
// 中间件上下文实现
// ============================================================================

// MiddlewareContext 是 IMiddlewareContext 的默认实现
type MiddlewareContext struct {
	ctx         context.Context
	evt         inf.IEvent
	serviceName string
	startTime   time.Time
	executed    atomic.Int32
	data        map[string]interface{}
	mu          sync.RWMutex
}

// NewMiddlewareContext 创建中间件上下文
func NewMiddlewareContext(ctx context.Context, evt inf.IEvent, serviceName string) *MiddlewareContext {
	return &MiddlewareContext{
		ctx:         ctx,
		evt:         evt,
		serviceName: serviceName,
		startTime:   time.Now(),
		data:        make(map[string]interface{}, 8), // 增加初始容量以减少扩容
	}
}

func (c *MiddlewareContext) Context() context.Context {
	return c.ctx
}

func (c *MiddlewareContext) Event() inf.IEvent {
	return c.evt
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

// Reset 重置上下文以便复用（对象池场景）
func (c *MiddlewareContext) Reset(ctx context.Context, evt inf.IEvent, serviceName string) {
	c.ctx = ctx
	c.evt = evt
	c.serviceName = serviceName
	c.startTime = time.Now()
	c.executed.Store(0)
	c.mu.Lock()
	for k := range c.data {
		delete(c.data, k)
	}
	c.mu.Unlock()
}

// ============================================================================
// 中间件链实现
// ============================================================================

// MiddlewareChain 是 IMiddlewareChain 的默认实现
type MiddlewareChain struct {
	middlewares []inf.IMailboxMiddleware
	mu          sync.RWMutex
}

// NewMiddlewareChain 创建中间件链
func NewMiddlewareChain(middlewares ...inf.IMailboxMiddleware) *MiddlewareChain {
	return &MiddlewareChain{
		middlewares: middlewares,
	}
}

// Add 添加中间件
func (c *MiddlewareChain) Add(middleware inf.IMailboxMiddleware) {
	c.mu.Lock()
	c.middlewares = append(c.middlewares, middleware)
	c.mu.Unlock()
}

// Remove 按名称移除中间件
func (c *MiddlewareChain) Remove(name string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i, m := range c.middlewares {
		if m.Name() == name {
			c.middlewares = append(c.middlewares[:i], c.middlewares[i+1:]...)
			return true
		}
	}
	return false
}

// ExecuteOnReceive 执行所有中间件的 OnReceive
func (c *MiddlewareChain) ExecuteOnReceive(ctx context.Context, evt inf.IEvent, serviceName string) (dto.MiddlewareResult, inf.IMiddlewareContext) {
	mctx := NewMiddlewareContext(ctx, evt, serviceName)

	c.mu.RLock()
	middlewares := c.middlewares
	c.mu.RUnlock()

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
	c.mu.RLock()
	middlewares := c.middlewares
	c.mu.RUnlock()
	// 仅对执行过 OnReceive 的中间件执行 OnComplete（洋葱模型）。
	end := len(middlewares)
	if mc, ok := mctx.(*MiddlewareContext); ok {
		if n := int(mc.executed.Load()); n >= 0 && n <= len(middlewares) {
			end = n
		}
	}
	for i := end - 1; i >= 0; i-- {
		middlewares[i].OnComplete(mctx, err, panicVal)
	}
}

// Start 启动所有中间件
func (c *MiddlewareChain) Start() {
	c.mu.RLock()
	middlewares := c.middlewares
	c.mu.RUnlock()

	for _, m := range middlewares {
		m.OnStart()
	}
}

// Stop 停止所有中间件
func (c *MiddlewareChain) Stop() {
	c.mu.RLock()
	middlewares := c.middlewares
	c.mu.RUnlock()

	// 逆序停止
	for i := len(middlewares) - 1; i >= 0; i-- {
		middlewares[i].OnStop()
	}
}

// Middlewares 获取中间件列表（用于调试）
func (c *MiddlewareChain) Middlewares() []inf.IMailboxMiddleware {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]inf.IMailboxMiddleware, len(c.middlewares))
	copy(result, c.middlewares)
	return result
}
