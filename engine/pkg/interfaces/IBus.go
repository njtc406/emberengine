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

// 说明: 如果是本地调用, in和out都支持[]interface{}结构,即可以直接传入多个参数,和返回多个值,如果返回的是多个值(除了error还有一个以上的返回)时,那么out就是[]interface{}

type IBus interface {
	// Call 同步调用服务
	Call(ctx context.Context, method string, in, out interface{}) error
	// AsyncCall 异步调用服务
	AsyncCall(ctx context.Context, method string, in interface{}, params *dto.AsyncCallParams, callbacks ...dto.CompletionFunc) (dto.CancelRpc, error)
	// Send 无返回调用
	Send(ctx context.Context, method string, in interface{}) error
	// Cast 广播
	Cast(ctx context.Context, method string, in interface{})

	// TODO 后续如有需要,可以考虑添加单独的多call接口,比如MultiCall,里面分别call所有服务，等待所有服务都返回之后，返回一个结果集，某些特殊场景可能会用到
}
