package emberctx

import "context"

type contextKey struct{}

var emberHeaderKey = &contextKey{}

// WithHeader 设置整个 header map（会覆盖旧值）
func WithHeader(ctx context.Context, headers map[string]string) context.Context {
	return context.WithValue(ctx, emberHeaderKey, headers)
}

// GetHeader 获取 header map（不可修改原 map）
func GetHeader(ctx context.Context) map[string]string {
	if ctx == nil {
		return nil
	}
	if v, ok := ctx.Value(emberHeaderKey).(map[string]string); ok {
		return v
	}
	return nil
}

// AddHeader 添加单个 header，如果 header 不存在会自动初始化
func AddHeader(ctx context.Context, key, value string) context.Context {
	headers := GetHeader(ctx)
	newHeaders := make(map[string]string)
	for k, v := range headers {
		newHeaders[k] = v
	}
	newHeaders[key] = value
	return WithHeader(ctx, newHeaders)
}

func AddHeaders(ctx context.Context, headers map[string]string) context.Context {
	oldHeaders := GetHeader(ctx)
	newHeaders := make(map[string]string)
	for k, v := range oldHeaders {
		newHeaders[k] = v
	}
	for k, v := range headers {
		newHeaders[k] = v
	}
	return WithHeader(ctx, newHeaders)
}

func GetHeaderValue(ctx context.Context, key string) string {
	headers := GetHeader(ctx)
	if headers == nil {
		return ""
	}
	return headers[key]
}
