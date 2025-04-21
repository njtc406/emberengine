// Package interfaces
// @Title  title
// @Description  desc
// @Author  yr  2024/11/26
// @Update  yr  2024/11/26
package interfaces

import (
	"github.com/njtc406/emberengine/engine/pkg/config"
)

//type IRemoteServer interface {
//	Init(conf *config.RPCServer, cliFactory IRpcSenderFactory) IRemoteServer
//	Serve() error
//	Close()
//	GetAddress() string
//}

type IRemoteServer interface {
	Init(IRpcSenderFactory)
	Serve(*config.RPCServer, string) error
	Close()
}
