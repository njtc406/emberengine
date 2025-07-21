// Package interfaces
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/7/13 0013 22:08
// 最后更新:  yr  2025/7/13 0013 22:08
package interfaces

import "context"

type IContext interface {
	context.Context
	IReset

	GetContext() context.Context

	SetHeaders(headers map[string]any)
	SetHeadersWithMap(headers map[string]string)
	SetHeader(key string, value any)

	GetHeader(key string) any
	GetHeaders() map[string]any
	ToHeaders() map[string]string

	GetTranceId() string
	GetDispatcherKey() string
	GetPriority() int32
	GetType() int32
}
