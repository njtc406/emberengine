// Package client
// @Title  本地服务的Client
// @Description  本地服务的Client,调用时直接使用rpcHandler发往对应的service
// @Author  yr  2024/9/3 下午4:26
// @Update  yr  2024/9/3 下午4:26
package client

import (
	"context"
	"sync/atomic"

	"github.com/njtc406/emberengine/engine/pkg/actor/mailbox/job"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/monitor"
)

// localSender 本地服务的Client
type localSender struct {
	closed     int32
	rpcMonitor *monitor.RpcMonitor
}

func newLClient(_ string, rm *monitor.RpcMonitor) inf.IRpcSender {
	return &localSender{rpcMonitor: rm}
}

func (lc *localSender) Close() {
	atomic.StoreInt32(&lc.closed, 1)
}

// DeliverRequest 将请求投递到目标服务的 mailbox。
func (lc *localSender) DeliverRequest(ctx context.Context, dispatcher inf.IRpcDispatcher, envelope inf.IEnvelope) error {
	if lc == nil || lc.IsClosed() {
		return def.ErrServiceIsClosedOrExited
	}
	if envelope == nil {
		return nil
	}

	// 本地请求：投递到对端 mailbox。
	// envelope 的最终 Release 由对端 mailbox 统一处理。
	rpcJob := job.NewRpcJob()
	rpcJob.SetContext(ctx)
	rpcJob.SetPayload(envelope)
	rpcJob.SetPriority(envelope.GetPriority())
	rpcJob.SetDispatcherKey(envelope.GetDispatchKey())
	rpcJob.SetDeadline(envelope.GetMeta().GetDeadline())
	// 【ADR-4】PostJob 拥有 Job 所有权：err 时 dispatcher 已内化 Release+OnJobDiscarded。
	if err := dispatcher.PostJob(rpcJob); err != nil {
		return err
	}
	return nil
}

// DeliverResponse 处理回复信息：唤醒同步 Call 等待方，或投递异步回调到调用方 mailbox。
func (lc *localSender) DeliverResponse(ctx context.Context, dispatcher inf.IRpcDispatcher, envelope inf.IEnvelope) error {
	if lc == nil || lc.IsClosed() {
		return def.ErrServiceIsClosedOrExited
	}
	if envelope == nil {
		return nil
	}

	meta := envelope.GetMeta()
	data := envelope.GetData()
	if meta == nil || data == nil {
		return def.ErrRpcMsgMetaOrDataIsNil
	}

	// 移除 monitor 监听
	rm := lc.rpcMonitor
	if rm == nil {
		envelope.Release()
		return nil
	}
	state := rm.Remove(meta.GetReqId())
	if state != nil {
		if state.NeedCallback() {
			// 异步回调：将 callback 信息写入 envelope meta，投递到调用方 mailbox 执行
			meta.SetCallbacks(state.GetCallbacks())
			rpcJob := job.NewRpcJob()
			rpcJob.SetContext(ctx)
			rpcJob.SetPayload(envelope)
			rpcJob.SetPriority(envelope.GetPriority())
			rpcJob.SetDispatcherKey(envelope.GetDispatchKey())
			rpcJob.SetDeadline(meta.GetDeadline())
			// 【ADR-4】PostJob 拥有 Job 所有权
			if err := dispatcher.PostJob(rpcJob); err != nil {
				return err
			}
			return nil
		}

		// 同步 Call：直接设置结果并唤醒等待方
		state.SetResult(data.GetResponse(), data.GetError())
		state.Complete()
	}

	// state 为 nil 说明已超时被清除，直接释放回复 envelope
	envelope.Release()
	return nil
}

func (lc *localSender) IsClosed() bool {
	return atomic.LoadInt32(&lc.closed) == 1
}
