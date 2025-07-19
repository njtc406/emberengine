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
	"gorm.io/gorm/utils"
	"strconv"
)

type XContext struct {
	context.Context
}

func New(ctx context.Context) XContext {
	if ctx == nil {
		ctx = emberctx.NewCtx(context.Background())
	}
	return XContext{
		Context: ctx,
	}
}

func (x *XContext) Reset() {
	x.Context = nil
}

func (x *XContext) SetHeaders(headers map[string]string) {
	x.Context = emberctx.AddHeaders(x.Context, headers)
}

func (x *XContext) SetHeader(key string, value any) {
	x.Context = emberctx.AddHeader(x.Context, key, utils.ToString(value))
}

func (x *XContext) GetHeader(key string) string {
	if x.Context == nil {
		return ""
	}
	return emberctx.GetHeaderValue(x.Context, key)
}

func (x *XContext) GetHeaders() map[string]string {
	if x.Context == nil {
		return nil
	}
	return emberctx.GetHeader(x.Context)
}

func (x *XContext) GetContext() context.Context {
	return x.Context
}

func (x *XContext) GetTranceId() string {
	return emberctx.GetHeaderValue(x.Context, def.DefaultTraceIdKey)
}

func (x *XContext) GetDispatcherKey() string {
	key := emberctx.GetHeaderValue(x.Context, def.DefaultDispatcherKey)
	if key == "" {
		key = def.PriorityUserStr
	}
	return key
}

func (x *XContext) GetPriority() int32 {
	priority := emberctx.GetHeaderValue(x.Context, def.DefaultPriorityKey)
	if priority != "" {
		priorityInt, err := strconv.Atoi(priority)
		if err == nil {
			return int32(priorityInt)
		}
	}
	return def.PriorityUser
}

func (x *XContext) GetType() int32 {
	priority := emberctx.GetHeaderValue(x.Context, def.DefaultTypeKey)
	if priority != "" {
		priorityInt, err := strconv.Atoi(priority)
		if err == nil {
			return int32(priorityInt)
		}
	}
	return -3000
}

func (x *XContext) Clone() *XContext {
	return &XContext{
		Context: x.Context,
	}
}
