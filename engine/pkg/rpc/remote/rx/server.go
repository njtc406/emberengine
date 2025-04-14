// Package rx
// @Title  title
// @Description  desc
// @Author  yr  2024/12/18
// @Update  yr  2024/12/18
package rx

import (
	"github.com/njtc406/emberengine/engine/pkg/config"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"github.com/smallnest/rpcx/server"
)

type rpcxServer struct {
	svr      *server.Server
	listener *RpcxListener
}

func NewRpcxServer() inf.IRemoteServer {
	return &rpcxServer{}
}

func (rs *rpcxServer) Init(sf inf.IRpcSenderFactory) {
	rs.listener = &RpcxListener{
		cliFactory: sf,
	}
	rs.svr = server.NewServer()
}

func (rs *rpcxServer) Serve(conf *config.RPCServer) error {
	// 注册rpc监听服务
	if err := rs.svr.RegisterName("RpcxListener", rs.listener, ""); err != nil {
		return err
	}
	log.SysLogger.Infof("rpcx server listening at: %s", conf.Addr)
	return rs.svr.Serve(conf.Protoc, conf.Addr)
}

func (rs *rpcxServer) Close() {
	if rs.svr == nil {
		return
	}
	rs.svr.Close()
}
