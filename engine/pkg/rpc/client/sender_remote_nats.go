// Package client
// @Title  title
// @Description  desc
// @Author  yr  2024/12/3
// @Update  yr  2024/12/3
package client

import (
	"fmt"
	"github.com/nats-io/nats.go"
	"github.com/njtc406/emberengine/engine/internal/message/msgenvelope"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/codec"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
)

type natsSender struct {
	conn *nats.Conn
}

func newNatsClient(addr string) inf.IRpcSender {
	opts := []nats.Option{
		nats.MaxReconnects(def.NatsDefaultMaxReconnects),
		nats.PingInterval(def.NatsDefaultPingInterval),
		nats.MaxPingsOutstanding(def.NatsDefaultPingMaxOutstanding),
		nats.ReconnectBufSize(def.NatsDefaultReconnectBufSize),
		nats.Timeout(def.NatsDefaultTimeout),
		//nats.NoEcho(),
		//nats.Compression(false),
	}

	conn, err := nats.Connect(addr, opts...)
	if err != nil {
		log.SysLogger.Errorf("nats client connect error: %s", err)
		return nil
	}

	sender := &natsSender{
		conn: conn,
	}

	log.SysLogger.Debugf("nats client connect success:%s", addr)

	return sender
}

func (rc *natsSender) Close() {
	if rc.conn == nil {
		return
	}
	rc.conn.Close()
	rc.conn = nil
}

func (rc *natsSender) send(envelope inf.IEnvelope) error {
	if rc.conn == nil {
		return def.ErrRPCHadClosed
	}

	// 构建发送消息
	msg := envelope.ToProtoMsg()
	if msg == nil {
		return def.ErrMsgSerializeFailed
	}
	defer msgenvelope.ReleaseMessage(msg)

	data, _, err := codec.Encode(def.ProtoBuf, msg)
	//data, err := proto.Marshal(msg)
	if err != nil {
		return def.ErrMsgSerializeFailed
	}

	return rc.conn.Publish(fmt.Sprintf(def.NatsDefaultTopic, envelope.GetMeta().GetReceiverPid().GetNodeUid()), data)
}

func (rc *natsSender) SendRequest(_ inf.IRpcDispatcher, envelope inf.IEnvelope) error {
	// 这里不能释放envelope,因为调用方需要使用
	return rc.send(envelope)
}

func (rc *natsSender) SendRequestAndRelease(_ inf.IRpcDispatcher, envelope inf.IEnvelope) error {
	defer envelope.Release()
	return rc.send(envelope)
}

func (rc *natsSender) SendResponse(_ inf.IRpcDispatcher, envelope inf.IEnvelope) error {
	defer envelope.Release()
	return rc.send(envelope)
}

func (rc *natsSender) IsClosed() bool {
	return rc.conn == nil || rc.conn.IsClosed()
}
