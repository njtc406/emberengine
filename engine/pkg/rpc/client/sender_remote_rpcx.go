// Package client
// @Title  远程服务的Client
// @Description  远程服务的Client
// @Author  yr  2024/9/3 下午4:26
// @Update  yr  2024/9/3 下午4:26
package client

import (
	"context"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/rpc/message/msgenvelope"

	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/smallnest/rpcx/client"
	"github.com/smallnest/rpcx/protocol"
	"github.com/smallnest/rpcx/share"
)

// 使用rpcx框架点对点直接调用, 这个相对于nats有一个优势, 就是可以知道消息是否被对方接收
// TODO 需要做成配置
type rpcxSender struct {
	rpcClients []client.XClient
	i          atomic.Uint64
}

func newRpcxClient(addr string) inf.IRpcSender {
	d, _ := client.NewPeer2PeerDiscovery("tcp@"+addr, "")
	// 如果调用失败,会自动重试3次
	var clients []client.XClient
	cpuNum := runtime.NumCPU()
	connNum := cpuNum / 2
	if connNum < 1 {
		connNum = 1
	}
	for i := 0; i < connNum; i++ {
		rpcClient := client.NewXClient("RpcxListener", client.Failfast, client.RoundRobin, d, client.Option{
			Retries:             3, // 重试3次
			RPCPath:             share.DefaultRPCPath,
			ConnectTimeout:      time.Second,            // 连接超时
			SerializeType:       protocol.MsgPack,       // 序列化方式
			CompressType:        protocol.None,          // 压缩方式
			BackupLatency:       100 * time.Millisecond, // 延迟时间(上一个请求在这个时间内没有回复,则会发送第二次请求) 这个需要考虑一下
			MaxWaitForHeartbeat: 30 * time.Second,       // 心跳时间
			TCPKeepAlivePeriod:  time.Minute,            // tcp keepalive
			BidirectionalBlock:  false,                  // 是否允许双向阻塞(true代表发送过去的消息必须消费之后才会再次发送,否则通道阻塞)
			TimeToDisallow:      time.Minute,
		})
		clients = append(clients, rpcClient)
	}

	remoteClient := &rpcxSender{
		rpcClients: clients,
	}

	log.SysLogger.Debugf("create remote client success : %s", addr)
	return remoteClient
}

func (rc *rpcxSender) Close() {
	if rc.IsClosed() {
		return
	}

	for _, rpcClient := range rc.rpcClients {
		_ = rpcClient.Close()
	}

	//log.SysLogger.Debugf("############################close remote rpcx client success : %s", rc.IRpcDispatcher.GetPid().String())
	rc.rpcClients = nil
}

func (rc *rpcxSender) send(ctx context.Context, dispatcher inf.IRpcDispatcher, envelope inf.IEnvelope) error {
	if rc.IsClosed() {
		return def.ErrRPCHadClosed
	}

	_, ok := ctx.Deadline()
	if !ok {
		newCtx, cancel := context.WithTimeout(ctx, config.GetDefaultRpcTimeout())
		defer cancel()
		ctx = newCtx
	}

	// 构建发送消息
	msg, err := envelope.ToProtoMsg(ctx)
	if err != nil {
		log.SysLogger.WithContext(ctx).Errorf("serialize message[%+v] is error: %s", envelope, err)
		return def.ErrMsgSerializeFailed
	}
	defer msgenvelope.ReleaseMessage(msg) // 发送后立即释放

	// 均衡负载
	rpcClient := rc.rpcClients[rc.i.Add(1)%uint64(len(rc.rpcClients))]

	call, err := rpcClient.Go(ctx, "RPCCall", msg, nil, make(chan *client.Call, 1))
	if err != nil {
		log.SysLogger.WithContext(ctx).Errorf("send message[%+v] to %s is error: %s", envelope, dispatcher.GetPid().GetServiceUid(), err)
		return def.ErrRPCCallFailed
	}
	select {
	case <-call.Done:
		if call.Error != nil {
			log.SysLogger.WithContext(ctx).Errorf("send message[%+v] to %s is error: %s", envelope, dispatcher.GetPid().GetServiceUid(), call.Error)
			return def.ErrRPCCallFailed
		}
	case <-ctx.Done():
		log.SysLogger.WithContext(ctx).Errorf("send message[%+v] to %s is timeout", envelope, dispatcher.GetPid().GetServiceUid())
		return def.ErrRPCCallFailed
	}

	//log.SysLogger.WithContext(ctx).Infof("send message[%+v] to %s success", envelope, dispatcher.GetPid().GetServiceUid())
	// 这里仅仅代表消息发送成功(不代表对方已经处理完成,处理全是异步的,会在回复消息中通知处理结果)
	return nil
}

func (rc *rpcxSender) Deliver(ctx context.Context, dispatcher inf.IRpcDispatcher, envelope inf.IEnvelope) error {
	defer envelope.Release()
	return rc.send(ctx, dispatcher, envelope)
}

func (rc *rpcxSender) IsClosed() bool {
	return rc.rpcClients == nil || len(rc.rpcClients) == 0
}
