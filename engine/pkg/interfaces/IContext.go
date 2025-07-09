// Package interfaces
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/7/8 0008 23:13
// 最后更新:  yr  2025/7/8 0008 23:13
package interfaces

import (
	"context"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	"time"
)

type IContext interface {
	context.Context

	WithContext(ctx context.Context) IContext
	WithTimeout(timeout time.Duration) IContext
	WithCancelContext(ctx context.Context, cancel context.CancelFunc) IContext
	SetHeaders(headers dto.Header) IContext
	SetHeader(key string, value string) IContext

	GetHeader(key string) string
	GetHeaders() dto.Header
	GetContext() IContext
	GetBaseContext() context.Context

	GetTranceId() string

	SetDone()
	Reset()
}
