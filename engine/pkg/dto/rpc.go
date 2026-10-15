// Package dto
// @Title  title
// @Description  desc
// @Author  yr  2024/11/8
// @Update  yr  2024/11/8
package dto

import (
	"context"

	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/log"
)

// CancelRpc 异步调用时的取消函数,可用于取消回调(请注意,一旦调用发送后是无法取消的,只能取消回调)
type CancelRpc func()

func EmptyCancelRpc() {} // 空的取消函数

type CompletionFunc func(ctx context.Context, data interface{}, err error, params ...interface{}) // 异步回调函数

type CompletionFuncs []CompletionFunc

func (f CompletionFuncs) DoCallback(ctx context.Context, data interface{}, err error, params ...interface{}) {
	for _, callback := range f {
		callback(ctx, data, err, params...)
	}
}

type AsyncCallParams struct {
	Params []interface{}
}

// CallMode 调用模式常量
const (
	// CallModeAny 任意一个节点调用成功即返回
	CallModeAny int32 = iota
	// CallModeAll 所有节点调用完成后才返回(收集所有结果)
	CallModeAll
)

type Headers map[string]string

func (header Headers) Get(key string) string {
	if v, ok := header[key]; ok {
		return v
	}
	return ""
}

func (header Headers) Set(key string, value string) {
	header[key] = value
}

func (header Headers) Keys() []string {
	keys := make([]string, 0, len(header))
	for k := range header {
		keys = append(keys, k)
	}
	return keys
}

func (header Headers) Length() int {
	return len(header)
}

// ToMap 将Header转换为map,生成新的map
func (header Headers) ToMap() map[string]string {
	mp := make(map[string]string)
	for k, v := range header {
		mp[k] = v
	}
	return mp
}

func (header Headers) ToFields() log.Fields {
	f := make(log.Fields, len(header))
	for k, v := range header {
		f[k] = v
	}
	return f
}

type RPCResponse struct{}

type BusOption struct {
	Ctx            context.Context
	Method         string
	In             interface{}
	Out            interface{}
	Callbacks      []CompletionFunc
	CallbackParams *AsyncCallParams
	NotRecycle     bool  // 不回收bus
	CallMode       int32 // 调用模式: CallModeAny(任意返回即返回) 或 CallModeAll(所有返回后才返回)
	Priority       def.Priority
	DispatchKey    string // 分发key,用于将job分发给不同的worker
	IdempotencyKey string // 业务幂等键
}

func (o *BusOption) Reset() {
	o.Ctx = nil
	o.Method = ""
	o.In = nil
	o.Out = nil
	o.Callbacks = nil
	o.CallbackParams = nil
	o.NotRecycle = false
	o.CallMode = CallModeAny // 默认为任意模式
	o.IdempotencyKey = ""
}

func NewBusOption(builders ...BusOptionBuilder) *BusOption {
	option := &BusOption{}
	for _, builder := range builders {
		builder(option)
	}
	return option
}

type BusOptionBuilder func(option *BusOption)

func WithCtx(ctx context.Context) BusOptionBuilder {
	return func(opt *BusOption) { opt.Ctx = ctx }
}
func WithMethod(method string) BusOptionBuilder {
	return func(opt *BusOption) { opt.Method = method }
}
func WithIn(in interface{}) BusOptionBuilder {
	return func(opt *BusOption) { opt.In = in }
}
func WithOut(out interface{}) BusOptionBuilder {
	return func(opt *BusOption) { opt.Out = out }
}

func WithCallbacks(callbacks ...CompletionFunc) BusOptionBuilder {
	return func(opt *BusOption) { opt.Callbacks = callbacks }
}

func WithCallbackParams(params *AsyncCallParams) BusOptionBuilder {
	return func(opt *BusOption) { opt.CallbackParams = params }
}

func WithNotRecycle() BusOptionBuilder {
	return func(opt *BusOption) { opt.NotRecycle = true }
}

// WithCallMode 设置调用模式
func WithCallMode(mode int32) BusOptionBuilder {
	return func(opt *BusOption) { opt.CallMode = mode }
}

// WithCallModeAny 设置为任意模式(任意一个节点调用成功即返回)
func WithCallModeAny() BusOptionBuilder {
	return func(opt *BusOption) { opt.CallMode = CallModeAny }
}

// WithCallModeAll 设置为全部模式(所有节点调用完成后才返回)
func WithCallModeAll() BusOptionBuilder {
	return func(opt *BusOption) { opt.CallMode = CallModeAll }
}

func WithPriority(priority def.Priority) BusOptionBuilder {
	return func(opt *BusOption) { opt.Priority = priority }
}

func WithDispatchKey(key string) BusOptionBuilder {
	return func(opt *BusOption) { opt.DispatchKey = key }
}

func WithIdempotencyKey(key string) BusOptionBuilder {
	return func(opt *BusOption) { opt.IdempotencyKey = key }
}
