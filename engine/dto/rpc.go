// Package dto
// @Title  title
// @Description  desc
// @Author  yr  2024/11/8
// @Update  yr  2024/11/8
package dto

// CancelRpc 异步调用时的取消函数,可用于取消回调(请注意,一旦调用发送后是无法取消的,只能取消回调)
type CancelRpc func()

func EmptyCancelRpc() {} // 空的取消函数

type CompletionFunc func(data interface{}, err error) // 异步回调函数

type Header map[string]string

func (header Header) Get(key string) string {
	return header[key]
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

func (header Header) ToMap() map[string]string {
	mp := make(map[string]string)
	for k, v := range header {
		mp[k] = v
	}
	return mp
}

type RPCResponse struct{}
