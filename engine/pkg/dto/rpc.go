// Package dto
// @Title  title
// @Description  desc
// @Author  yr  2024/11/8
// @Update  yr  2024/11/8
package dto

import "github.com/njtc406/logrus"

// CancelRpc 异步调用时的取消函数,可用于取消回调(请注意,一旦调用发送后是无法取消的,只能取消回调)
type CancelRpc func()

func EmptyCancelRpc() {} // 空的取消函数

type CompletionFunc func(params []interface{}, data interface{}, err error) // 异步回调函数

type AsyncCallParams struct {
	Params []interface{}
}

type Header map[string]string

func (header Header) Get(key string) string {
	if v, ok := header[key]; ok {
		return v
	}
	return ""
}

func (header Header) Set(key string, value string) {
	header[key] = value
}

func (header Header) Keys() []string {
	keys := make([]string, 0, len(header))
	for k := range header {
		keys = append(keys, k)
	}
	return keys
}

func (header Header) Length() int {
	return len(header)
}

// ToMap 将Header转换为map,生成新的map
func (header Header) ToMap() map[string]string {
	mp := make(map[string]string)
	for k, v := range header {
		mp[k] = v
	}
	return mp
}

func (header Header) ToFields() logrus.Fields {
	f := make(logrus.Fields, len(header))
	for k, v := range header {
		f[k] = v
	}
	return f
}

type RPCResponse struct{}
