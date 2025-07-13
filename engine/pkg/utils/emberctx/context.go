package emberctx

import (
	"context"
	"github.com/google/uuid"
	"github.com/njtc406/emberengine/engine/pkg/def"
)

type contextKey struct{}

var emberHeaderKey = &contextKey{}

// WithHeader 设置整个 header map（会覆盖旧值）
func WithHeader(ctx context.Context, headers map[string]string) context.Context {
	return context.WithValue(ctx, emberHeaderKey, headers)
}

func getHeader(ctx context.Context) map[string]string {
	if v, ok := ctx.Value(emberHeaderKey).(map[string]string); ok {
		return v
	}
	return nil
}

// GetHeader 获取 header map（不可修改原 map）
func GetHeader(ctx context.Context) map[string]string {
	if ctx == nil {
		return nil
	}

	headers := getHeader(ctx)
	if headers == nil {
		return nil
	}

	// 返回一个副本以防止外部修改
	copied := make(map[string]string, len(headers))
	for k, val := range headers {
		copied[k] = val
	}
	return copied
}

// AddHeader 添加单个 header，如果 header 不存在会自动初始化 (not goroutine safe)
func AddHeader(ctx context.Context, key, value string) context.Context {
	headers := getHeader(ctx)
	if headers == nil {
		headers = make(map[string]string)
	}

	headers[key] = value

	return WithHeader(ctx, headers)
}

// AddHeaders 添加多个 header
func AddHeaders(ctx context.Context, newHeaders map[string]string) context.Context {
	if len(newHeaders) == 0 {
		return ctx
	}

	headers := getHeader(ctx)
	if headers == nil {
		headers = make(map[string]string)
	}

	// 直接修改,否则并发可能出现覆盖,上层使用需要加锁来做
	for k, v := range newHeaders {
		headers[k] = v
	}

	return WithHeader(ctx, headers)
}

func GetHeaderValue(ctx context.Context, key string) string {
	headers := getHeader(ctx)
	if headers == nil {
		return ""
	}
	return headers[key]
}

type Option func(ctx context.Context) context.Context

func WithKV(key, value string) Option {
	return func(ctx context.Context) context.Context {
		return AddHeader(ctx, key, value)
	}
}

func NewCtx(ctx context.Context, options ...Option) context.Context {
	if GetHeaderValue(ctx, def.DefaultTraceIdKey) == "" {
		ctx = AddHeaders(ctx, map[string]string{
			def.DefaultTraceIdKey: uuid.NewString(),
		})
	}

	for _, option := range options {
		ctx = option(ctx)
	}
	return ctx
}
