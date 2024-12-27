// Package inf
// @Title  title
// @Description  desc
// @Author  yr  2024/11/26
// @Update  yr  2024/11/26
package inf

import "github.com/njtc406/emberengine/engine/config"

//type IRemoteServer interface {
//	Init(conf *config.RPCServer, cliFactory IRpcSenderFactory) IRemoteServer
//	Serve() error
//	Close()
//	GetAddress() string
//}

type IRemoteServer interface {
	Init(IRpcSenderFactory)
	Serve(*config.RPCServer) error
	Close()
}
