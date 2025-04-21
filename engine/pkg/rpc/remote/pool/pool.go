// Package pool
// @Title  title
// @Description  desc
// @Author  yr  2024/12/18
// @Update  yr  2024/12/18
package pool

import (
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/rpc/remote/gr"
	"github.com/njtc406/emberengine/engine/pkg/rpc/remote/nt"
	"github.com/njtc406/emberengine/engine/pkg/rpc/remote/rx"
)

type ListenerCreator func(cliFactory inf.IRpcSenderFactory) interface{}

type creator struct {
	listenerCreator ListenerCreator
	server          inf.IRemoteServer
}

var remoteMap = map[string]inf.IRemoteServer{
	def.RpcTypeRpcx: rx.NewRpcxServer(),
	def.RpcTypeGrpc: gr.NewGrpcServer(),
	def.RpcTypeNats: nt.NewNatsServer(),
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
