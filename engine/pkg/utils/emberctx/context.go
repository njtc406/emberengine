package emberctx

import (
	"context"
	"github.com/google/uuid"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/utils/util"
)

type contextKey struct{}

var emberHeaderKey = &contextKey{}

// WithHeader 设置整个 header map（会覆盖旧值）
func WithHeader(ctx context.Context, headers map[string]any) context.Context {
	return context.WithValue(ctx, emberHeaderKey, headers)
}

func getHeader(ctx context.Context) map[string]any {
	if v, ok := ctx.Value(emberHeaderKey).(map[string]any); ok {
		return v
	}
	return nil
}

// GetHeader 获取 header map（不可修改原 map）
func GetHeader(ctx context.Context) map[string]any {
	if ctx == nil {
		return nil
	}

	headers := getHeader(ctx)
	if headers == nil {
		return nil
	}

	// 返回一个副本以防止外部修改
	copied := make(map[string]any, len(headers))
	for k, val := range headers {
		copied[k] = val
	}
	return copied
}

func ToHeaders(ctx context.Context) map[string]string {
	headers := GetHeader(ctx)
	if headers == nil {
		return nil
	}

	// 创建一个副本，防止外部修改
	copied := make(map[string]string, len(headers))
	for k, val := range headers {
		copied[k] = util.ToString(val)
	}
	return copied
}

// AddHeader 添加单个 header，如果 header 不存在会自动初始化 (not goroutine safe)
func AddHeader(ctx context.Context, key string, value any) context.Context {
	headers := getHeader(ctx)
	if headers == nil {
		headers = make(map[string]any)
	}

	headers[key] = value

	return WithHeader(ctx, headers)
}

// AddHeaders 添加多个 header
func AddHeaders(ctx context.Context, newHeaders map[string]any) context.Context {
	if len(newHeaders) == 0 {
		return ctx
	}

	headers := getHeader(ctx)
	if headers == nil {
		headers = make(map[string]any)
	}

	// 直接修改,否则并发可能出现覆盖,上层使用需要加锁来做
	for k, v := range newHeaders {
		headers[k] = v
	}

	return WithHeader(ctx, headers)
}

func GetHeaderValue(ctx context.Context, key string) any {
	headers := getHeader(ctx)
	if headers == nil {
		return nil
	}
	return headers[key]
}

type Option func(ctx context.Context) context.Context

func NewCtx(ctx context.Context, options ...Option) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if GetHeaderValue(ctx, def.DefaultTraceIdKey) == "" {
		ctx = AddHeaders(ctx, map[string]any{
			def.DefaultTraceIdKey: uuid.NewString(),
		})
	}

	for _, option := range options {
		ctx = option(ctx)
	}
	return ctx
}
