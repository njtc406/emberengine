// Package remote
// @Title  title
// @Description  desc
// @Author  yr  2024/12/18
// @Update  yr  2024/12/18
package remote

import (
	"github.com/njtc406/emberengine/engine/pkg/config"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/rpc/remote/pool"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
)

func NewRemote() *Remote {
	return &Remote{}
}

type Remote struct {
	conf *config.RPCServer
	svr  inf.IRemoteServer
}

func (r *Remote) Init(conf *config.RPCServer, cliFactory inf.IRpcSenderFactory) *Remote {
	r.conf = conf
	r.svr = pool.GetServer(conf.Type)
	if r.svr == nil {
		log.SysLogger.Panicf("rpc server type %s not support", conf.Type)
		return nil
	}
	r.svr.Init(cliFactory)
	return r
}

func (r *Remote) Serve() {
	go func() {
		if err := r.svr.Serve(r.conf); err != nil {
			log.SysLogger.Warnf("rpc serve stop: %v", err)
		}
	}()
}

func (r *Remote) Close() {
	r.svr.Close()
}

func (r *Remote) GetAddress() string {
	return r.conf.Addr
}
