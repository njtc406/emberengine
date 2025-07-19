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

	SetHeaders(headers map[string]string)
	SetHeader(key string, value any)

	GetHeader(key string) string
	GetHeaders() map[string]string
	GetContext() context.Context

	GetTranceId() string
	GetDispatcherKey() string
	GetPriority() int32
	GetType() int32
}
