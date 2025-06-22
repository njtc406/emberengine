// Package client
// @Title  title
// @Description  desc
// @Author  yr  2024/12/3
// @Update  yr  2024/12/3
package client

import (
	"fmt"
	"github.com/nats-io/nats.go"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"google.golang.org/protobuf/proto"
)

type natsSender struct {
	conn *nats.Conn
}

func newNatsClient(addr string) inf.IRpcSender {
	var opts []nats.Option
	opts = append(opts, nats.MaxReconnects(def.NatsDefaultMaxReconnects))
	opts = append(opts, nats.ReconnectWait(def.NatsDefaultReconnectWait))
	opts = append(opts, nats.PingInterval(def.NatsDefaultPingInterval))
	opts = append(opts, nats.MaxPingsOutstanding(def.NatsDefaultPingMaxOutstanding))
	opts = append(opts, nats.ReconnectBufSize(def.NatsDefaultReconnectBufSize))
	opts = append(opts, nats.Timeout(def.NatsDefaultTimeout))

	conn, err := nats.Connect(addr, opts...)
	if err != nil {
		log.SysLogger.Errorf("nats client connect error: %s", err)
		return nil
	}

	sender := &natsSender{
		conn: conn,
	}

	log.SysLogger.Infof("nats client connect success:%s", addr)

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

	data, err := proto.Marshal(msg)
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
