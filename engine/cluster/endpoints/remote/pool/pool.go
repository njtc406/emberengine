// Package pool
// @Title  title
// @Description  desc
// @Author  yr  2024/12/18
// @Update  yr  2024/12/18
package pool

import (
	"github.com/njtc406/emberengine/engine/cluster/endpoints/remote/gr"
	"github.com/njtc406/emberengine/engine/cluster/endpoints/remote/rx"
	"github.com/njtc406/emberengine/engine/def"
	"github.com/njtc406/emberengine/engine/inf"
)

type ListenerCreator func(cliFactory inf.IRpcSenderFactory) interface{}

type creator struct {
	listenerCreator ListenerCreator
	server          inf.IRemoteServer
}

var remoteMap = map[string]inf.IRemoteServer{
	def.RpcTypeRpcx: rx.NewRpcxServer(),
	def.RpcTypeGrpc: gr.NewGrpcServer(),
}

func Register(tp string, server inf.IRemoteServer) {
	remoteMap[tp] = server
}

func GetServer(tp string) inf.IRemoteServer {
	if srv, ok := remoteMap[tp]; ok {
		return srv
	}
	return nil
}
