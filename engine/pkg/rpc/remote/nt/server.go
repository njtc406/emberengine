// Package nt
// @Title  title
// @Description  desc
// @Author  yr  2025/4/15
// @Update  yr  2025/4/15
package nt

import (
	"fmt"
	"github.com/nats-io/nats.go"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
)

type natsServer struct {
	listener     *NatsListener
	server       *nats.Conn
	subscription *nats.Subscription
}

func NewNatsServer() inf.IRemoteServer {
	return &natsServer{}
}

func (s *natsServer) Init(sf inf.IRpcSenderFactory) {
	s.listener = &NatsListener{
		cliFactory: sf,
	}
}

func (s *natsServer) Serve(conf *config.RPCServer, nodeUid string) error {
	log.SysLogger.Infof("nats server listening at: %s", conf.Addr)

	var opts []nats.Option
	opts = append(opts, nats.MaxReconnects(def.NatsDefaultMaxReconnects))
	opts = append(opts, nats.ReconnectWait(def.NatsDefaultReconnectWait))
	opts = append(opts, nats.PingInterval(def.NatsDefaultPingInterval))
	opts = append(opts, nats.MaxPingsOutstanding(def.NatsDefaultPingMaxOutstanding))
	opts = append(opts, nats.ReconnectBufSize(def.NatsDefaultReconnectBufSize))
	opts = append(opts, nats.Timeout(def.NatsDefaultTimeout))

	if conf.CAs != "" {
		opts = append(opts, nats.RootCAs(conf.CAs))
	}

	if conf.Cert != "" && conf.CertKey != "" {
		opts = append(opts, nats.ClientCert(conf.Cert, conf.CertKey))
	}

	conn, err := nats.Connect(conf.Addr, opts...)
	if err != nil {
		log.SysLogger.Errorf("nats server connect error: %s", err)
		return err
	}
	s.server = conn

	subscription, err := s.server.Subscribe(fmt.Sprintf(def.NatsDefaultTopic, nodeUid), s.listener.Handle)
	if err != nil {
		log.SysLogger.Errorf("nats server subscribe error: %s", err)
		return err
	}
	s.subscription = subscription
	return nil
}

func (s *natsServer) Close() {
	if s.server == nil {
		return
	}
	_ = s.subscription.Unsubscribe()
	s.server.Close()
	s.server = nil
	s.subscription = nil
}
