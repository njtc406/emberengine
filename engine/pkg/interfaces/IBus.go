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
	CallWithOpt(opts ...dto.BusOptionBuilder) error
	// TODO 这个接口后续来实现
	//CallAll(ctx context.Context, method string, in interface{}, out []interface{}) error

	// AsyncCall 异步调用服务
	AsyncCall(ctx context.Context, method string, in interface{}, params *dto.AsyncCallParams, callbacks ...dto.CompletionFunc) (dto.CancelRpc, error)
	AsyncCallWithOpt(opts ...dto.BusOptionBuilder) (dto.CancelRpc, error)

	// Send 无返回调用
	Send(ctx context.Context, method string, in interface{}) error
	SendWithOpt(opts ...dto.BusOptionBuilder) error

	// TODO IBus可能需要增加一个可复用的接口,就是当使用了call之后,之前select出来的这些IBus不会被释放,
	// 后续可以接着call、send什么的,节约资源,但是需要提供一个手动释放的接口
	// 考虑直接做成参数
	// BusOptionBuilder这个考虑一下要不要增加这个

	Release()
}

/*
统一一下风格
Call(ctx, method, in, out)
CallWithOption(opts ...BusOptionBuilder)

AsyncCall(ctx, method, in, params, callbacks...)
AsyncCallWithOption(opts ...BusOptionBuilder)

Send(ctx, method, in)
SendWithOption(opts ...BusOptionBuilder)

internal的接口
doCall(ctx, data, out) error
doCallWithRecycle(ctx, data, out, recycle bool) error

doAsyncCall(ctx, data, param, callbacks...) (uint64, error)
doAsyncCallWithRecycle(ctx, data, recycle bool, param, callbacks...) (uint64, error)

doSend(ctx, data) error
doSendWithRecycle(ctx, data, recycle bool) error
*/
