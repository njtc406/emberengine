package emberctx

import (
	"context"
	"reflect"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/utils/util"
)

type contextKey struct{}

var emberHeaderKey = &contextKey{}

var traceSeq atomic.Uint64

// NewTraceID 生成新的 traceID，格式为 hex(unixNano)-hex(seq)。
// 该函数是高性能的，避免了 uuid 的随机数开销。
func NewTraceID() string {
	// 在 hot path 里避免 uuid.NewString() 的随机数开销。
	// 这里用 (unixNano)-(seq) 的十六进制串，唯一性对链路追踪足够。
	// 为减少临时分配，使用栈上的固定缓冲。
	n := uint64(time.Now().UnixNano())
	s := traceSeq.Add(1)
	var tmp [33]byte
	buf := tmp[:0]
	buf = strconv.AppendUint(buf, n, 16)
	buf = append(buf, '-')
	buf = strconv.AppendUint(buf, s, 16)
	return string(buf)
}

// WithHeader 设置整个 header map（会覆盖旧值）
func WithHeader(ctx context.Context, headers map[string]any) context.Context {
	if isNilContext(ctx) {
		ctx = context.Background()
	}
	return context.WithValue(ctx, emberHeaderKey, headers)
}

func getHeader(ctx context.Context) map[string]any {
	if isNilContext(ctx) {
		return nil
	}
	if v, ok := ctx.Value(emberHeaderKey).(map[string]any); ok {
		return v
	}
	return nil
}

func isNilContext(ctx context.Context) bool {
	if ctx == nil {
		return true
	}
	// 防御“typed nil interface”：interface != nil 但底层指针为 nil。
	v := reflect.ValueOf(ctx)
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Slice, reflect.Map, reflect.Func, reflect.Chan:
		return v.IsNil()
	default:
		return false
	}
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

// GetHeaderRef 返回底层 header map 的引用（不会拷贝）。
// 注意：返回值必须被视为只读；不要修改它，否则可能引发数据竞争或污染其他派生 context。
func GetHeaderRef(ctx context.Context) map[string]any {
	return getHeader(ctx)
}

// ToHeadersFast 将 header 转换为 map[string]string（仅做一次分配），并避免额外的 map[string]any 拷贝。
// 注意：该函数依赖 GetHeaderRef 的“只读”约定。
func ToHeadersFast(ctx context.Context) map[string]string {
	headers := getHeader(ctx)
	if headers == nil {
		return nil
	}

	converted := make(map[string]string, len(headers))
	for k, val := range headers {
		converted[k] = util.ToString(val)
	}
	return converted
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

// AddHeader 添加单个 header，使用 Copy-on-Write 保证线程安全。
// 每次调用都会创建新的 header map，原 context 的 header 不受影响。
func AddHeader(ctx context.Context, key string, value any) context.Context {
	if isNilContext(ctx) {
		ctx = context.Background()
	}
	oldHeaders := getHeader(ctx)

	// Copy-on-Write: 创建新 map，避免污染原 context
	newHeaders := make(map[string]any, len(oldHeaders)+1)
	for k, v := range oldHeaders {
		newHeaders[k] = v
	}
	newHeaders[key] = value

	return WithHeader(ctx, newHeaders)
}

// AddHeaders 添加多个 header，使用 Copy-on-Write 保证线程安全。
func AddHeaders(ctx context.Context, newHeaders map[string]any) context.Context {
	if len(newHeaders) == 0 {
		return ctx
	}
	if isNilContext(ctx) {
		ctx = context.Background()
	}

	oldHeaders := getHeader(ctx)

	// Copy-on-Write: 创建新 map
	merged := make(map[string]any, len(oldHeaders)+len(newHeaders))
	for k, v := range oldHeaders {
		merged[k] = v
	}
	for k, v := range newHeaders {
		merged[k] = v
	}

	return WithHeader(ctx, merged)
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
	traceId := GetHeaderValue(ctx, def.DefaultTraceIdKey)
	if traceId == nil || traceId == "" {
		ctx = AddHeader(ctx, def.DefaultTraceIdKey, NewTraceID())
	}

	for _, option := range options {
		ctx = option(ctx)
	}
	return ctx
}
