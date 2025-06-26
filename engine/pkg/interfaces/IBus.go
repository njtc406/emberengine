// Package interfaces
// @Title  title
// @Description  desc
// @Author  yr  2024/11/12
// @Update  yr  2024/11/12
package interfaces

import (
	"context"
	"github.com/njtc406/emberengine/engine/pkg/dto"
)

// 重要说明: 如果是本地调用, in和out都支持[]interface{}结构,即可以直接传入多个参数,和返回多个值,如果返回的是多个值(除了error还有一个以上的返回)时,那么out就是[]interface{}

type IBus interface {
	// Call 同步调用服务
	Call(ctx context.Context, method string, in, out interface{}) error

	// AsyncCall 异步调用服务
	AsyncCall(ctx context.Context, method string, in interface{}, params *dto.AsyncCallParams, callbacks ...dto.CompletionFunc) (dto.CancelRpc, error)

	// Send 无返回调用
	Send(ctx context.Context, method string, in interface{}) error
}
