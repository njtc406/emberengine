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
	logger                      *log.Logger
	lastSlowConsumerLogUnixNano atomic.Int64
	slowConsumerSuppressed      atomic.Uint64
}

var natsConfProvider *config.NatsConf

func SetNatsConf(conf *config.NatsConf) {
	natsConfProvider = conf
}

func getNatsConf() *config.NatsConf {
	return natsConfProvider
}

func NewNatsServer() inf.IRemoteServer {
	return &natsServer{}
}

func (s *natsServer) SetLogger(logger *log.Logger) {
	if logger != nil {
		s.logger = logger
		if s.listener != nil {
			s.listener.logger = logger
		}
	}
}

func (s *natsServer) Init(sf inf.IRpcSenderFactory) {
	s.listener = &NatsListener{
		cliFactory: sf,
		logger:     s.logger,
	}
}

func (s *natsServer) Serve(conf *config.RPCServer, nodeUid string) error {
	s.logger.Infof("nats server listening at: %s", conf.Addr)

	natsConf := getNatsConf()

	var opts []nats.Option
	maxReconnects := def.NatsDefaultMaxReconnects
	if natsConf != nil && natsConf.MaxReconnects > 0 {
		maxReconnects = natsConf.MaxReconnects
	}
	opts = append(opts, nats.MaxReconnects(maxReconnects))

	reconnectWait := def.NatsDefaultReconnectWait
	if natsConf != nil && natsConf.ReconnectWait > 0 {
		reconnectWait = natsConf.ReconnectWait
	}
	opts = append(opts, nats.ReconnectWait(reconnectWait))

	pingInterval := def.NatsDefaultPingInterval
	if natsConf != nil && natsConf.PingInterval > 0 {
		pingInterval = natsConf.PingInterval
	}
	opts = append(opts, nats.PingInterval(pingInterval))

	pingMaxOutstanding := def.NatsDefaultPingMaxOutstanding
	if natsConf != nil && natsConf.PingMaxOutstanding > 0 {
		pingMaxOutstanding = natsConf.PingMaxOutstanding
	}
	opts = append(opts, nats.MaxPingsOutstanding(pingMaxOutstanding))

	reconnectBufSize := def.NatsDefaultReconnectBufSize
	if natsConf != nil && natsConf.ReconnectBufSize > 0 {
		reconnectBufSize = natsConf.ReconnectBufSize
	}
	opts = append(opts, nats.ReconnectBufSize(reconnectBufSize))

	timeout := def.NatsDefaultTimeout
	if natsConf != nil && natsConf.Timeout > 0 {
		timeout = natsConf.Timeout
	}
	opts = append(opts, nats.Timeout(timeout))

	// 认证配置（可选）：复用 NodeConf.EventBusConf.NatsConf
	if natsConf != nil {
		if natsConf.Token != "" {
			opts = append(opts, nats.Token(natsConf.Token))
		} else if natsConf.UserName != "" {
			opts = append(opts, nats.UserInfo(natsConf.UserName, natsConf.Password))
		}
	}
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
						s.logger.Errorf("nats async error: subject=%s err=%v (suppressed=%d in last second)", sub.Subject, err, suppressed)
					} else {
						s.logger.Errorf("nats async error: subject=%s err=%v", sub.Subject, err)
					}
					return
				}
				if suppressed > 0 {
					s.logger.Errorf("nats async error: err=%v (suppressed=%d in last second)", err, suppressed)
					return
				}
			}
		}
		if sub != nil {
			s.logger.Errorf("nats async error: subject=%s err=%v", sub.Subject, err)
			return
		}
		s.logger.Errorf("nats async error: err=%v", err)
	}))
	opts = append(opts, nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
		s.logger.Errorf("nats disconnected: %v", err)
	}))
	opts = append(opts, nats.ReconnectHandler(func(_ *nats.Conn) {
		s.logger.Infof("nats reconnected")
	}))
	opts = append(opts, nats.ClosedHandler(func(_ *nats.Conn) {
		s.logger.Infof("nats connection closed")
	}))

	if conf.CAs != "" {
		opts = append(opts, nats.RootCAs(conf.CAs))
	}

	if conf.Cert != "" && conf.CertKey != "" {
		opts = append(opts, nats.ClientCert(conf.Cert, conf.CertKey))
	}

	conn, err := nats.Connect(conf.Addr, opts...)
	if err != nil {
		s.logger.Errorf("nats server connect error: %s", err)
		return err
	}
	s.server = conn

	subscription, err := s.server.Subscribe(def.NatsDefaultTopic+nodeUid, s.listener.Handle)
	if err != nil {
		s.logger.Errorf("nats server subscribe error: %s", err)
		return err
	}
	// 提升异步订阅在高突发场景下的缓冲能力，减少 slow consumer 丢消息风险。
	msgLimit := def.NatsDefaultSubPendingMsgLimit
	bytesLimit := def.NatsDefaultSubPendingBytesLimit
	if natsConf != nil {
		if natsConf.SubPendingMsgLimit > 0 {
			msgLimit = natsConf.SubPendingMsgLimit
		}
		if natsConf.SubPendingBytesLimit > 0 {
			bytesLimit = natsConf.SubPendingBytesLimit
		}
	}
	_ = subscription.SetPendingLimits(msgLimit, bytesLimit)
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
