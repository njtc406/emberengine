// Package client
// @Title  title
// @Description  desc
// @Author  yr  2024/12/3
// @Update  yr  2024/12/3
package client

import (
	"context"
	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type grpcSender struct {
	conn      *grpc.ClientConn
	rpcClient actor.GrpcListenerClient
}

func newGrpcClient(addr string) inf.IRpcSender {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.SysLogger.Panicf("grpcxSender newGrpcClient error: %v", err)
	}
	cli := actor.NewGrpcListenerClient(conn)
	return &grpcSender{
		conn: conn,
		//IRpcDispatcher: sender,
		rpcClient: cli,
	}
}

func (rc *grpcSender) Close() {
	_ = rc.conn.Close()
	rc.rpcClient = nil
}

func (rc *grpcSender) send(envelope inf.IEnvelope) error {
	if rc.rpcClient == nil {
		return def.ErrRPCHadClosed
	}
	// 这里仅仅代表消息发送成功
	ctx := envelope.GetContext()
	_, ok := ctx.Deadline()
	if !ok {
		newCtx, cancel := context.WithTimeout(ctx, def.DefaultRpcTimeout)
		defer cancel()
		ctx = newCtx
	}

	// 构建发送消息
	msg := envelope.ToProtoMsg()
	if msg == nil {
		return def.ErrMsgSerializeFailed
	}

	if _, err := rc.rpcClient.RPCCall(ctx, msg); err != nil {
		log.SysLogger.WithContext(ctx).Errorf("send message[%+v] to %s is error: %s", envelope, envelope.GetMeta().GetReceiverPid().GetServiceUid(), err)
		return def.ErrRPCCallFailed
	}

	//log.SysLogger.WithContext(ctx).Infof("send message[%+v] to %s success", envelope, envelope.GetReceiverPid().GetServiceUid())

	return nil
}

func (rc *grpcSender) SendRequest(_ inf.IRpcDispatcher, envelope inf.IEnvelope) error {
	// 这里不能释放envelope,因为调用方需要使用
	return rc.send(envelope)
}

func (rc *grpcSender) SendRequestAndRelease(_ inf.IRpcDispatcher, envelope inf.IEnvelope) error {
	defer envelope.Release()
	return rc.send(envelope)
}

func (rc *grpcSender) SendResponse(_ inf.IRpcDispatcher, envelope inf.IEnvelope) error {
	defer envelope.Release()
	return rc.send(envelope)
}

func (rc *grpcSender) IsClosed() bool {
	return rc.rpcClient == nil
}
