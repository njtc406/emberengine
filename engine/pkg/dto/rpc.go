// Package dto
// @Title  title
// @Description  desc
// @Author  yr  2024/11/8
// @Update  yr  2024/11/8
package dto

import (
	"context"
	"github.com/njtc406/logrus"
)

// CancelRpc 异步调用时的取消函数,可用于取消回调(请注意,一旦调用发送后是无法取消的,只能取消回调)
type CancelRpc func()

func EmptyCancelRpc() {} // 空的取消函数

type CompletionFunc func(data interface{}, err error, params ...interface{}) // 异步回调函数

type AsyncCallParams struct {
	Params []interface{}
}

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

func (header Headers) ToFields() logrus.Fields {
	f := make(logrus.Fields, len(header))
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
	NotRecycle     bool // 不回收bus
}

func (o *BusOption) Reset() {
	o.Ctx = nil
	o.Method = ""
	o.In = nil
	o.Out = nil
	o.Callbacks = nil
	o.CallbackParams = nil
	o.NotRecycle = false
}

func NewBusOption(builders ...BusOptionBuilder) *BusOption {
	option := &BusOption{}
	for _, builder := range builders {
		builder(option)
	}
	return option
}

type BusOptionBuilder func(option *BusOption)

func WithContext(ctx context.Context) BusOptionBuilder {
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
