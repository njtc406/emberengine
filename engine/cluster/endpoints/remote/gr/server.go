// Package gr
// @Title  title
// @Description  desc
// @Author  yr  2024/12/18
// @Update  yr  2024/12/18
package gr

import (
	"github.com/njtc406/emberengine/engine/actor"
	"github.com/njtc406/emberengine/engine/config"
	"github.com/njtc406/emberengine/engine/inf"
	"github.com/njtc406/emberengine/engine/utils/log"
	"google.golang.org/grpc"
	"net"
)

type grpcServer struct {
	listener *GrpcListener
	server   *grpc.Server
}

func NewGrpcServer() inf.IRemoteServer {
	return &grpcServer{}
}

func (s *grpcServer) Init(sf inf.IRpcSenderFactory) {
	s.listener = &GrpcListener{
		cliFactory: sf,
	}
	s.server = grpc.NewServer()
}

func (s *grpcServer) Serve(conf *config.RPCServer) error {
	log.SysLogger.Infof("grpc server listening at: %s", conf.Addr)
	lis, err := net.Listen(conf.Protoc, conf.Addr)
	if err != nil {
		return err
	}
	actor.RegisterGrpcListenerServer(s.server, s.listener)
	return s.server.Serve(lis)
}

func (s *grpcServer) Close() {
	s.server.Stop()
}
