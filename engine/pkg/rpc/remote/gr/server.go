// Package gr
// @Title  title
// @Description  desc
// @Author  yr  2024/12/18
// @Update  yr  2024/12/18
package gr

import (
	"net"

	"github.com/njtc406/emberengine/engine/pkg/log"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/config"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"google.golang.org/grpc"
)

type grpcServer struct {
	listener *GrpcListener
	server   *grpc.Server
	logger   *log.Logger
}

func NewGrpcServer() inf.IRemoteServer {
	return &grpcServer{}
}

func (s *grpcServer) SetLogger(logger *log.Logger) {
	if logger != nil {
		s.logger = logger
	}
}

func (s *grpcServer) Init(sf inf.IRpcSenderFactory) {
	s.listener = &GrpcListener{
		cliFactory: sf,
	}
	s.server = grpc.NewServer()
}

func (s *grpcServer) Serve(conf *config.RPCServer, nodeUid string) error {
	if s.logger != nil {
		s.logger.Infof("grpc server listening at: %s", conf.Addr)
	}
	lis, err := net.Listen(conf.Protoc, conf.Addr)
	if err != nil {
		return err
	}
	actor.RegisterGrpcListenerServer(s.server, s.listener)
	return s.server.Serve(lis)
}

func (s *grpcServer) Close() {
	if s.server == nil {
		return
	}
	s.server.Stop()
	s.server = nil
}
