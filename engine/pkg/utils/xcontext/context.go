// Package xcontext
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/7/13 0013 22:11
// 最后更新:  yr  2025/7/13 0013 22:11
package xcontext

import (
	"context"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/utils/emberctx"
	"github.com/njtc406/emberengine/engine/pkg/utils/util"
	"time"
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

func (x *XContext) SetHeaders(headers map[string]any) {
	x.Context = emberctx.AddHeaders(x.Context, headers)
}

func (x *XContext) SetHeadersWithMap(headers map[string]string) {
	m := make(map[string]any, len(headers))
	for k, v := range headers {
		m[k] = v
	}
	x.Context = emberctx.WithHeader(x.Context, m)
}

func (x *XContext) SetHeader(key string, value any) {
	x.Context = emberctx.AddHeader(x.Context, key, value)
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
	headers := x.GetHeaders()
	ret := make(map[string]string)
	for k, v := range headers {
		ret[k] = util.ToString(v)
	}
	return ret
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
		key = def.PriorityUserStr
	}
	return key
}

func (x *XContext) GetPriority() int32 {
	priority, ok := emberctx.GetHeaderValue(x.Context, def.DefaultPriorityKey).(int32)
	if ok {
		return priority
	} else {
		priority, ok := emberctx.GetHeaderValue(x.Context, def.DefaultPriorityKey).(string)
		if ok {
			return util.ToIntT[int32](priority)
		}
	}
	return def.PriorityUser
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
