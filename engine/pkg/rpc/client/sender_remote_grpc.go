// Package client
// @Title  title
// @Description  desc
// @Author  yr  2024/12/3
// @Update  yr  2024/12/3
package client

import (
	"context"
	"runtime"
	"sync/atomic"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/rpc/message/msgenvelope"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type grpcSender struct {
	conns      []*grpc.ClientConn
	rpcClients []actor.GrpcListenerClient
	i          atomic.Int64
}

func newGrpcClient(addr string) inf.IRpcSender {
	var clients []actor.GrpcListenerClient
	var conns []*grpc.ClientConn
	cpuNum := runtime.NumCPU()
	connNum := cpuNum / 2
	if connNum < 1 {
		connNum = 1
	}

	for i := 0; i < connNum; i++ {
		conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			getClientLogger().Errorf("grpcSender newGrpcClient error: %v", err)
			for _, opened := range conns {
				_ = opened.Close()
			}
			return nil
		}
		conns = append(conns, conn)
		clients = append(clients, actor.NewGrpcListenerClient(conn))
	}

	return &grpcSender{
		conns:      conns,
		rpcClients: clients,
	}
}

func (rc *grpcSender) Close() {
	for _, conn := range rc.conns {
		_ = conn.Close()
	}
	rc.conns = nil
	rc.rpcClients = nil
}

func (rc *grpcSender) send(ctx context.Context, envelope inf.IEnvelope) error {
	if rc.IsClosed() {
		return def.ErrRPCHadClosed
	}

	// 构建发送消息
	msg, err := envelope.ToProtoMsg(ctx)
	if err != nil {
		getClientLogger().WithContext(ctx).Errorf("serialize message[%+v] is error: %s", envelope, err)
		return def.ErrMsgSerializeFailed
	}
	defer msgenvelope.ReleaseMessage(msg)

	rpcClient := rc.rpcClients[rc.i.Add(1)%int64(len(rc.rpcClients))]

	if _, err := rpcClient.RPCCall(ctx, msg); err != nil {
		getClientLogger().WithContext(ctx).Errorf("send message[%+v] to %s is error: %s", envelope,
			envelope.GetMeta().GetReceiverPid().GetServiceUid(), err)
		return def.ErrRPCCallFailed
	}

	//log.SysLogger.WithContext(ctx).Infof("send message[%+v] to %s success", envelope, envelope.GetReceiverPid().GetServiceUid())
	// 这里仅仅代表消息发送成功(不代表对方已经处理完成,处理全是异步的,会在回复消息中通知处理结果)
	return nil
}

func (rc *grpcSender) DeliverRequest(ctx context.Context, _ inf.IRpcDispatcher, envelope inf.IEnvelope) error {
	defer envelope.Release()
	return rc.send(ctx, envelope)
}

func (rc *grpcSender) DeliverResponse(ctx context.Context, _ inf.IRpcDispatcher, envelope inf.IEnvelope) error {
	defer envelope.Release()
	return rc.send(ctx, envelope)
}

func (rc *grpcSender) IsClosed() bool {
	return rc.rpcClients == nil || len(rc.rpcClients) == 0
}
