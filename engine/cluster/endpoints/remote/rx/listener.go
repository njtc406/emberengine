// Package rx
// @Title  rpcx的服务端监听器
// @Description  desc
// @Author  yr  2024/11/8
// @Update  yr  2024/11/8
package rx

import (
	"context"
	"github.com/njtc406/emberengine/engine/actor"
	"github.com/njtc406/emberengine/engine/cluster/endpoints/remote/handler"
	"github.com/njtc406/emberengine/engine/dto"
	"github.com/njtc406/emberengine/engine/inf"
)

type RpcxListener struct {
	cliFactory inf.IRpcSenderFactory
}

func (rm *RpcxListener) RPCCall(_ context.Context, req *actor.Message, _ *dto.RPCResponse) error {
	//log.SysLogger.Debugf("rpcx call: %+v", req)
	return handler.RpcMessageHandler(rm.cliFactory, req)
}
