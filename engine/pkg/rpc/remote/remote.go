// Package remote
// @Title  title
// @Description  desc
// @Author  yr  2024/12/18
// @Update  yr  2024/12/18
package remote

import (
	"fmt"

	"github.com/njtc406/emberengine/engine/pkg/config"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/rpc/remote/handler"
	"github.com/njtc406/emberengine/engine/pkg/rpc/remote/pool"
)

func NewRemote() *Remote {
	return &Remote{}
}

type Remote struct {
	log.ILoggerX // 持有 ILoggerX
	conf         *config.RPCServer
	svr          inf.IRemoteServer
}

type loggerAwareRemoteServer interface {
	SetLogger(logger log.ILoggerX)
}

type handlerAwareRemoteServer interface {
	SetHandler(h *handler.Handler)
}

type natsConfAwareRemoteServer interface {
	SetNatsConf(conf *config.NatsConf)
}

func (r *Remote) Init(conf *config.RPCServer, cliFactory inf.IRpcSenderFactory, logger log.ILoggerX, rpcHandler *handler.Handler, natsConf *config.NatsConf) (*Remote, error) {
	r.ILoggerX = logger
	r.conf = conf
	r.svr = pool.CreateServer(conf.Type)
	if r.svr == nil {
		return nil, fmt.Errorf("rpc server type %s not support", conf.Type)
	}
	if aware, ok := r.svr.(loggerAwareRemoteServer); ok {
		aware.SetLogger(logger)
	}
	if aware, ok := r.svr.(handlerAwareRemoteServer); ok {
		aware.SetHandler(rpcHandler)
	}
	if aware, ok := r.svr.(natsConfAwareRemoteServer); ok {
		aware.SetNatsConf(natsConf)
	}
	r.svr.Init(cliFactory)
	return r, nil
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
