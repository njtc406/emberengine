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
		// 返回一个副本以防止外部修改
		copied := make(map[string]string, len(v))
		for k, val := range v {
			copied[k] = val
		}
		return copied
	}
	return nil
}

// AddHeader 添加单个 header，如果 header 不存在会自动初始化
func AddHeader(ctx context.Context, key, value string) context.Context {
	headers := GetHeader(ctx)
	if headers == nil {
		headers = make(map[string]string)
	}

	// 创建一个新 map 而不是直接修改
	newHeaders := make(map[string]string, len(headers)+1)
	for k, v := range headers {
		newHeaders[k] = v
	}
	newHeaders[key] = value

	return WithHeader(ctx, newHeaders)
}

func AddHeaders(ctx context.Context, newHeaders map[string]string) context.Context {
	if len(newHeaders) == 0 {
		return ctx
	}

	headers := GetHeader(ctx)
	if headers == nil {
		headers = make(map[string]string)
	}

	// 创建一个新 map 而不是直接修改
	merged := make(map[string]string, len(headers)+len(newHeaders))
	for k, v := range headers {
		merged[k] = v
	}
	for k, v := range newHeaders {
		merged[k] = v
	}

	return WithHeader(ctx, merged)
}

func GetHeaderValue(ctx context.Context, key string) string {
	headers := GetHeader(ctx)
	if headers == nil {
		return ""
	}
	return headers[key]
}
