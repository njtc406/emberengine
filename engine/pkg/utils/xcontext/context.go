// Package xcontext
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/7/13 0013 22:11
// 最后更新:  yr  2025/7/13 0013 22:11
package xcontext

import (
	"context"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/emberctx"
	"github.com/njtc406/emberengine/engine/pkg/utils/util"
)

type XContext struct {
	context.Context
}

func New(ctx context.Context) XContext {
	if ctx == nil {
		ctx = emberctx.NewCtx(nil)
	}
	return XContext{
		Context: ctx,
	}
}

func NewWithCloneCtx(ctx context.Context) XContext {
	if ctx == nil {
		newCtx := emberctx.NewCtx(nil)
		return XContext{
			Context: newCtx,
		}
	}

	headers := emberctx.GetHeader(ctx)
	newCtx := emberctx.NewCtx(context.Background())
	emberctx.AddHeaders(newCtx, headers)
	return XContext{
		Context: newCtx,
	}
}

func NewWithTimeout(ctx context.Context, timeout time.Duration) (*XContext, context.CancelFunc) {
	if ctx == nil {
		ctx = emberctx.NewCtx(nil)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	return &XContext{
		Context: ctx,
	}, cancel
}

func NewWithCancel(ctx context.Context) (*XContext, context.CancelFunc) {
	if ctx == nil {
		ctx = emberctx.NewCtx(nil)
	}
	ctx, cancel := context.WithCancel(ctx)
	return &XContext{
		Context: ctx,
	}, cancel
}

func (x *XContext) Reset() {
	x.Context = nil
}

func (x *XContext) SetHeaders(headers map[string]any) inf.IContext {
	x.Context = emberctx.AddHeaders(x.Context, headers)
	return x
}

func (x *XContext) SetHeadersWithMap(headers map[string]string) inf.IContext {
	m := make(map[string]any, len(headers))
	for k, v := range headers {
		m[k] = v
	}
	x.Context = emberctx.AddHeaders(x.Context, m)
	return x
}

func (x *XContext) AddHeader(key string, value any) inf.IContext {
	x.Context = emberctx.AddHeader(x.Context, key, value)
	return x
}

func (x *XContext) AddHeaders(headers map[string]any) inf.IContext {
	x.Context = emberctx.AddHeaders(x.Context, headers)
	return x
}

func (x *XContext) GetHeader(key string) any {
	if x.Context == nil {
		return ""
	}
	return emberctx.GetHeaderValue(x.Context, key)
}

func (x *XContext) GetHeaders() map[string]any {
	if x.Context == nil {
		return nil
	}
	return emberctx.GetHeader(x.Context)
}

func (x *XContext) ToHeaders() map[string]string {
	if x.Context == nil {
		return map[string]string{}
	}
	// 热路径：避免 GetHeaders() 的 map 拷贝，直接一次性转换。
	headers := emberctx.ToHeadersFast(x.Context)
	if headers == nil {
		return map[string]string{}
	}
	return headers
}

func (x *XContext) GetContext() context.Context {
	return x.Context
}

func (x *XContext) GetTranceId() string {
	val, ok := emberctx.GetHeaderValue(x.Context, def.DefaultTraceIdKey).(string)
	if !ok {
		return ""
	}
	return val
}

func (x *XContext) GetDispatcherKey() string {
	key, ok := emberctx.GetHeaderValue(x.Context, def.DefaultDispatcherKey).(string)
	if !ok || key == "" {
		key = def.PriorityNormalStr
	}
	return key
}

func (x *XContext) GetPriority() def.Priority {
	priority, ok := emberctx.GetHeaderValue(x.Context, def.DefaultPriorityKey).(def.Priority)
	if ok {
		return priority
	} else {
		priority, ok := emberctx.GetHeaderValue(x.Context, def.DefaultPriorityKey).(string)
		if ok {
			return util.ToIntT[def.Priority](priority)
		}
	}
	return def.PriorityNormal
}

func (x *XContext) GetType() int32 {
	tp, ok := emberctx.GetHeaderValue(x.Context, def.DefaultTypeKey).(int32)
	if ok {
		return tp
	} else {
		tp, ok := emberctx.GetHeaderValue(x.Context, def.DefaultTypeKey).(string)
		if ok {
			return util.ToIntT[int32](tp)
		}
	}
	return -3000
}

func (x *XContext) Clone() *XContext {
	return &XContext{
		Context: x.Context,
	}
}

// ContextFactory 高性能 context 工厂，用于批量创建相似 context 的场景。
// 适用于：固定 dispatcher key + 每请求独立 traceID 的高并发压测场景。
//
// 用法：
//
//	factory := xcontext.NewFactory(map[string]any{
//	    def.DefaultDispatcherKey: "worker-1",
//	})
//	for i := 0; i < total; i++ {
//	    ctx := factory.NewContext()  // 每次都有新 traceID，但复用 base headers
//	    doRPC(ctx)
//	}
type ContextFactory struct {
	baseHeaders map[string]any
}

// NewFactory 创建 context 工厂，baseHeaders 是每次创建时都会包含的固定 header。
func NewFactory(baseHeaders map[string]any) *ContextFactory {
	if baseHeaders == nil {
		baseHeaders = make(map[string]any)
	}
	return &ContextFactory{baseHeaders: baseHeaders}
}

// NewContext 创建新 context，包含 base headers + 新生成的 traceID。
// 该方法是并发安全的（Copy-on-Write）。
func (f *ContextFactory) NewContext() XContext {
	// 预分配容量：base headers + traceID
	headers := make(map[string]any, len(f.baseHeaders)+1)
	for k, v := range f.baseHeaders {
		headers[k] = v
	}
	headers[def.DefaultTraceIdKey] = emberctx.NewTraceID()

	ctx := emberctx.WithHeader(context.Background(), headers)
	return XContext{Context: ctx}
}

// NewContextWithoutTrace 创建新 context，只包含 base headers，不生成 traceID。
// 用于不需要追踪的高吞吐场景，避免 time.Now() 开销。
func (f *ContextFactory) NewContextWithoutTrace() XContext {
	headers := make(map[string]any, len(f.baseHeaders))
	for k, v := range f.baseHeaders {
		headers[k] = v
	}

	ctx := emberctx.WithHeader(context.Background(), headers)
	return XContext{Context: ctx}
}
