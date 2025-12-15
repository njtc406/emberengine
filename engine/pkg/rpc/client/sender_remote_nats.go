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

func newNatsClient(addr string) inf.IRpcSender {
	opts := []nats.Option{
		nats.MaxReconnects(def.NatsDefaultMaxReconnects),
		nats.PingInterval(def.NatsDefaultPingInterval),
		nats.MaxPingsOutstanding(def.NatsDefaultPingMaxOutstanding),
		nats.ReconnectBufSize(def.NatsDefaultReconnectBufSize),
		nats.Timeout(def.NatsDefaultTimeout),
		nats.ErrorHandler(func(_ *nats.Conn, sub *nats.Subscription, err error) {
			if sub != nil {
				log.SysLogger.Errorf("nats async error: subject=%s err=%v", sub.Subject, err)
				return
			}
			log.SysLogger.Errorf("nats async error: err=%v", err)
		}),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			log.SysLogger.Errorf("nats disconnected: %v", err)
		}),
		nats.ReconnectHandler(func(_ *nats.Conn) {
			log.SysLogger.Infof("nats reconnected")
		}),
		nats.ClosedHandler(func(_ *nats.Conn) {
			log.SysLogger.Infof("nats connection closed")
		}),
		//nats.NoEcho(),
		//nats.Compression(false),
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
			log.SysLogger.Errorf("nats client connect error: %s", err)
			for _, c := range conns {
				c.Close()
			}
			return nil
		}
		conns = append(conns, conn)
	}

	sender := &natsSender{conns: conns}
	if diag.Enabled() {
		if poolSize > 1 {
			log.SysLogger.Infof("nats client connect success:%s (pool=%d)", addr, poolSize)
		} else {
			log.SysLogger.Debugf("nats client connect success:%s", addr)
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
		log.SysLogger.WithContext(ctx).Errorf("serialize message[%+v] is error: %s", envelope, err)
		return def.ErrMsgSerializeFailed
	}
	defer msgenvelope.ReleaseMessage(msg)

	data, err := codec.Encode(def.ProtoBuf, msg)
	if err != nil {
		log.SysLogger.WithContext(ctx).Errorf("encode message[%+v] is error: %s", envelope, err)
		return def.ErrMsgSerializeFailed
	}

	return conn.Publish(def.NatsDefaultTopic+meta.GetReceiverPid().GetNodeUid(), data)
}

func (rc *natsSender) Deliver(ctx context.Context, _ inf.IRpcDispatcher, envelope inf.IEnvelope) error {
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
