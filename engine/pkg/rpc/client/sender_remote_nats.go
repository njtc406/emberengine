// Package client
// @Title  title
// @Description  desc
// @Author  yr  2024/12/3
// @Update  yr  2024/12/3
package client

import (
	"context"
	"os"
	"strconv"
	"sync/atomic"

	"github.com/nats-io/nats.go"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/rpc/message/msgenvelope"
	"github.com/njtc406/emberengine/engine/pkg/utils/codec"
	"github.com/njtc406/emberengine/engine/pkg/utils/diag"
)

type natsSender struct {
	conns []*nats.Conn
	next  uint32
}

func newNatsClient(addr string, logger log.ILoggerX, natsConf *config.NatsConf) inf.IRpcSender {
	maxReconnects := def.NatsDefaultMaxReconnects
	if natsConf != nil && natsConf.MaxReconnects > 0 {
		maxReconnects = natsConf.MaxReconnects
	}

	pingInterval := def.NatsDefaultPingInterval
	if natsConf != nil && natsConf.PingInterval > 0 {
		pingInterval = natsConf.PingInterval
	}

	pingMaxOutstanding := def.NatsDefaultPingMaxOutstanding
	if natsConf != nil && natsConf.PingMaxOutstanding > 0 {
		pingMaxOutstanding = natsConf.PingMaxOutstanding
	}

	reconnectBufSize := def.NatsDefaultReconnectBufSize
	if natsConf != nil && natsConf.ReconnectBufSize > 0 {
		reconnectBufSize = natsConf.ReconnectBufSize
	}

	timeout := def.NatsDefaultTimeout
	if natsConf != nil && natsConf.Timeout > 0 {
		timeout = natsConf.Timeout
	}

	reconnectWait := def.NatsDefaultReconnectWait
	if natsConf != nil && natsConf.ReconnectWait > 0 {
		reconnectWait = natsConf.ReconnectWait
	}

	opts := []nats.Option{
		nats.MaxReconnects(maxReconnects),
		nats.ReconnectWait(reconnectWait),
		nats.PingInterval(pingInterval),
		nats.MaxPingsOutstanding(pingMaxOutstanding),
		nats.ReconnectBufSize(reconnectBufSize),
		nats.Timeout(timeout),
		nats.ErrorHandler(func(_ *nats.Conn, sub *nats.Subscription, err error) {
			if logger == nil {
				return
			}
			if sub != nil {
				logger.Errorf("nats async error: subject=%s err=%v", sub.Subject, err)
				return
			}
			logger.Errorf("nats async error: err=%v", err)
		}),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			if logger != nil {
				logger.Errorf("nats disconnected: %v", err)
			}
		}),
		nats.ReconnectHandler(func(_ *nats.Conn) {
			if logger != nil {
				logger.Infof("nats reconnected")
			}
		}),
		nats.ClosedHandler(func(_ *nats.Conn) {
			if logger != nil {
				logger.Infof("nats connection closed")
			}
		}),
		//nats.NoEcho(),
		//nats.Compression(false),
	}

	// 认证配置（可选）：复用 NodeConf.EventBusConf.NatsConf
	if natsConf != nil {
		if natsConf.Token != "" {
			opts = append(opts, nats.Token(natsConf.Token))
		} else if natsConf.UserName != "" {
			opts = append(opts, nats.UserInfo(natsConf.UserName, natsConf.Password))
		}
	}

	poolSize := 1
	if v := os.Getenv("EMBER_NATS_SENDER_POOL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			poolSize = n
		}
	}

	conns := make([]*nats.Conn, 0, poolSize)
	for i := 0; i < poolSize; i++ {
		conn, err := nats.Connect(addr, opts...)
		if err != nil {
			if logger != nil {
				logger.Errorf("nats client connect error: %s", err)
			}
			for _, c := range conns {
				c.Close()
			}
			return nil
		}
		conns = append(conns, conn)
	}

	sender := &natsSender{conns: conns}
	if diag.Enabled() {
		if logger != nil {
			if poolSize > 1 {
				logger.Infof("nats client connect success:%s (pool=%d)", addr, poolSize)
			} else {
				logger.Debugf("nats client connect success:%s", addr)
			}
		}
	}
	return sender
}

func (rc *natsSender) Close() {
	if len(rc.conns) == 0 {
		return
	}
	for _, c := range rc.conns {
		if c != nil {
			c.Close()
		}
	}
	rc.conns = nil
}

func (rc *natsSender) pickConn() *nats.Conn {
	if len(rc.conns) == 0 {
		return nil
	}
	if len(rc.conns) == 1 {
		return rc.conns[0]
	}
	idx := int(atomic.AddUint32(&rc.next, 1)-1) % len(rc.conns)
	conn := rc.conns[idx]
	if conn != nil && !conn.IsClosed() {
		return conn
	}
	// fallback: find any usable conn
	for _, c := range rc.conns {
		if c != nil && !c.IsClosed() {
			return c
		}
	}
	return conn
}

func (rc *natsSender) send(ctx context.Context, envelope inf.IEnvelope) error {
	conn := rc.pickConn()
	if conn == nil {
		return def.ErrRPCHadClosed
	}
	meta := envelope.GetMeta()
	if meta == nil || meta.GetReceiverPid() == nil {
		return def.ErrServiceNotFound
	}

	// 构建发送消息
	msg, err := envelope.ToProtoMsg(ctx)
	if err != nil {
		// nats sender 不持有 logger，序列化失败由上层错误链路处理
		return def.ErrMsgSerializeFailed
	}
	defer msgenvelope.ReleaseMessage(msg)

	data, err := codec.Encode(def.ProtoBuf, msg)
	if err != nil {
		// nats sender 不持有 logger，编码失败由上层错误链路处理
		return def.ErrMsgSerializeFailed
	}

	return conn.Publish(def.NatsDefaultTopic+meta.GetReceiverPid().GetNodeUid(), data)
}

func (rc *natsSender) DeliverRequest(ctx context.Context, _ inf.IRpcDispatcher, envelope inf.IEnvelope) error {
	defer envelope.Release()
	return rc.send(ctx, envelope)
}

func (rc *natsSender) DeliverResponse(ctx context.Context, _ inf.IRpcDispatcher, envelope inf.IEnvelope) error {
	defer envelope.Release()
	return rc.send(ctx, envelope)
}

func (rc *natsSender) IsClosed() bool {
	if len(rc.conns) == 0 {
		return true
	}
	for _, c := range rc.conns {
		if c != nil && !c.IsClosed() {
			return false
		}
	}
	return true
}
