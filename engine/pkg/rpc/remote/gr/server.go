// Package gr
// @Title  title
// @Description  desc
// @Author  yr  2024/12/18
// @Update  yr  2024/12/18
package gr

import (
	"fmt"
	"net"

	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/tlsx"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/config"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/rpc/remote/handler"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type grpcServer struct {
	listener *GrpcListener
	server   *grpc.Server
	logger   log.ILoggerX
	handler  *handler.Handler
}

func NewGrpcServer() inf.IRemoteServer {
	return &grpcServer{}
}

func (s *grpcServer) SetLogger(logger log.ILoggerX) {
	if logger != nil {
		s.logger = logger
	}
}

func (s *grpcServer) Init(sf inf.IRpcSenderFactory) {
	s.listener = &GrpcListener{
		cliFactory: sf,
		handler:    s.handler,
	}
}

func (s *grpcServer) SetHandler(h *handler.Handler) {
	s.handler = h
	if s.listener != nil {
		s.listener.handler = h
	}
}

func (s *grpcServer) Serve(conf *config.RPCServer, nodeUid string) error {
	if s.logger != nil {
		s.logger.Infof("grpc server listening at: %s", conf.Addr)
	}

	// 根据配置决定是否启用 TLS
	var opts []grpc.ServerOption
	if conf.Cert != "" && conf.CertKey != "" {
		tlsCfg, err := tlsx.LoadServerTLS(conf.Cert, conf.CertKey, conf.CAs)
		if err != nil {
			return fmt.Errorf("grpcServer.Serve: load TLS: %w", err)
		}
		opts = append(opts, grpc.Creds(credentials.NewTLS(tlsCfg)))
		if s.logger != nil {
			s.logger.Infof("grpc server TLS enabled (cert=%s, ca=%s)", conf.Cert, conf.CAs)
		}
	}
	s.server = grpc.NewServer(opts...)

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
