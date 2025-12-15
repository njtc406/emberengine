// Package nt
// @Title  title
// @Description  desc
// @Author  yr  2025/4/15
// @Update  yr  2025/4/15
package nt

import (
	"strings"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
)

type natsServer struct {
	listener                    *NatsListener
	server                      *nats.Conn
	subscription                *nats.Subscription
	lastSlowConsumerLogUnixNano atomic.Int64
	slowConsumerSuppressed      atomic.Uint64
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
	opts = append(opts, nats.ErrorHandler(func(_ *nats.Conn, sub *nats.Subscription, err error) {
		if err != nil {
			// NOTE: slow consumer 在压测时可能非常频繁，如果每次都 Errorf 会导致 IO/锁/控制台写入
			// 反过来把接收端拖慢，从而进一步触发 slow consumer，形成“自激振荡”。
			// 这里做限频：每秒最多打一条，并附带抑制计数。
			errStr := err.Error()
			if err == nats.ErrSlowConsumer || strings.Contains(errStr, "slow consumer") {
				now := time.Now().UnixNano()
				last := s.lastSlowConsumerLogUnixNano.Load()
				if now-last < int64(time.Second) {
					s.slowConsumerSuppressed.Add(1)
					return
				}
				if !s.lastSlowConsumerLogUnixNano.CompareAndSwap(last, now) {
					s.slowConsumerSuppressed.Add(1)
					return
				}
				suppressed := s.slowConsumerSuppressed.Swap(0)
				if sub != nil {
					if suppressed > 0 {
						log.SysLogger.Errorf("nats async error: subject=%s err=%v (suppressed=%d in last second)", sub.Subject, err, suppressed)
					} else {
						log.SysLogger.Errorf("nats async error: subject=%s err=%v", sub.Subject, err)
					}
					return
				}
				if suppressed > 0 {
					log.SysLogger.Errorf("nats async error: err=%v (suppressed=%d in last second)", err, suppressed)
					return
				}
			}
		}
		if sub != nil {
			log.SysLogger.Errorf("nats async error: subject=%s err=%v", sub.Subject, err)
			return
		}
		log.SysLogger.Errorf("nats async error: err=%v", err)
	}))
	opts = append(opts, nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
		log.SysLogger.Errorf("nats disconnected: %v", err)
	}))
	opts = append(opts, nats.ReconnectHandler(func(_ *nats.Conn) {
		log.SysLogger.Infof("nats reconnected")
	}))
	opts = append(opts, nats.ClosedHandler(func(_ *nats.Conn) {
		log.SysLogger.Infof("nats connection closed")
	}))

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

	subscription, err := s.server.Subscribe(def.NatsDefaultTopic+nodeUid, s.listener.Handle)
	if err != nil {
		log.SysLogger.Errorf("nats server subscribe error: %s", err)
		return err
	}
	// 提升异步订阅在高突发场景下的缓冲能力，减少 slow consumer 丢消息风险。
	_ = subscription.SetPendingLimits(def.NatsDefaultSubPendingMsgLimit, def.NatsDefaultSubPendingBytesLimit)
	s.subscription = subscription
	return nil
}

func (s *natsServer) Close() {
	if s.server == nil {
		return
	}
	// Drain to let in-flight subscription callbacks finish before closing.
	// This helps ensure pooled objects created in handlers are returned before shutdown stats are printed.
	if s.subscription != nil {
		_ = s.subscription.Drain()
	}
	_ = s.server.Drain()
	s.server.Close()
	s.server = nil
	s.subscription = nil
}
