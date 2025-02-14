// Package client
// @Title  title
// @Description  desc
// @Author  yr  2024/12/3
// @Update  yr  2024/12/3
package client

import (
	"context"
	"github.com/njtc406/emberengine/engine/internal/message/msgenvelope"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type grpcSender struct {
	inf.IRpcSender
	conn      *grpc.ClientConn
	rpcClient actor.GrpcListenerClient
}

func newGrpcClient(sender inf.IRpcSender) inf.IRpcSenderHandler {
	pid := sender.GetPid()
	conn, err := grpc.NewClient(pid.GetAddress(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.SysLogger.Panicf("grpcxSender newGrpcClient error: %v", err)
	}
	cli := actor.NewGrpcListenerClient(conn)
	return &grpcSender{
		conn:       conn,
		IRpcSender: sender,
		rpcClient:  cli,
	}
}

func (rc *grpcSender) Close() {
	_ = rc.conn.Close()
	rc.rpcClient = nil
	//log.SysLogger.Debugf("close remote grpc client success : %s", rc.IRpcSender.GetPid().String())
}

func (rc *grpcSender) send(envelope inf.IEnvelope) error {
	if rc.rpcClient == nil {
		return def.RPCHadClosed
	}
	// 这里仅仅代表消息发送成功
	timeout := envelope.GetTimeout()
	if envelope.GetTimeout() == 0 {
		timeout = time.Millisecond * 500
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// 构建发送消息
	msg := envelope.ToProtoMsg()
	if msg == nil {
		return def.MsgSerializeFailed
	}

	if _, err := rc.rpcClient.RPCCall(ctx, msg); err != nil {
		log.SysLogger.Errorf("send message[%+v] to %s is error: %s", envelope, rc.IRpcSender.GetPid().GetServiceUid(), err)
		return def.RPCCallFailed
	}

	return nil
}

func (rc *grpcSender) SendRequest(envelope inf.IEnvelope) error {
	// 这里不能释放envelope,因为调用方需要使用
	return rc.send(envelope)
}

func (rc *grpcSender) SendRequestAndRelease(envelope inf.IEnvelope) error {
	defer msgenvelope.ReleaseMsgEnvelope(envelope)
	return rc.send(envelope)
}

func (rc *grpcSender) SendResponse(envelope inf.IEnvelope) error {
	defer msgenvelope.ReleaseMsgEnvelope(envelope)
	return rc.send(envelope)
}

func (rc *grpcSender) IsClosed() bool {
	return rc.rpcClient == nil
}
