// Package remote
// @Title  title
// @Description  desc
// @Author  yr  2024/12/18
// @Update  yr  2024/12/18
package remote

import (
	"github.com/njtc406/emberengine/engine/pkg/config"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/rpc/remote/pool"
)

func NewRemote() *Remote {
	return &Remote{}
}

type Remote struct {
	*log.Logger // 嵌入 Logger，替代 log.SysLogger
	conf        *config.RPCServer
	svr         inf.IRemoteServer
}

func (r *Remote) Init(conf *config.RPCServer, cliFactory inf.IRpcSenderFactory, logger *log.Logger) *Remote {
	r.Logger = logger
	r.conf = conf
	r.svr = pool.CreateServer(conf.Type)
	if r.svr == nil {
		r.Panicf("rpc server type %s not support", conf.Type)
		return nil
	}
	r.svr.Init(cliFactory)
	return r
}

func (r *Remote) Serve(nodeUid string) {
	go func() {
		if err := r.svr.Serve(r.conf, nodeUid); err != nil {
			r.Warnf("rpc serve stop: %v", err)
		}
	}()
}

func (r *Remote) Close() {
	r.svr.Close()
}

func (r *Remote) GetAddress() string {
	return r.conf.Addr
}
