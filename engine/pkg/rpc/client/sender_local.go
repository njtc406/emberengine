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
	closed int32
}

func newLClient(_ string) inf.IRpcSender {
	return &localSender{}
}

func (lc *localSender) Close() {
	atomic.StoreInt32(&lc.closed, 1)
}

func (lc *localSender) Deliver(ctx context.Context, dispatcher inf.IRpcDispatcher, envelope inf.IEnvelope) error {
	if lc == nil || lc.IsClosed() {
		return def.ErrServiceIsClosedOrExited
	}
	if envelope == nil {
		return nil
	}

	data := envelope.GetData()
	if data != nil && data.IsReply() {
		// 本地回复：这里使用的是新创建的 respEnv，处理完后需要释放
		defer envelope.Release()
		state := monitor.GetRpcMonitor().Remove(envelope.GetMeta().GetReqId())
		if state == nil {
			return def.ErrEnvelopeNotFound
		}
		state.SetResult(data.GetResponse(), data.GetError())
		state.Complete()
		return nil
	}

	// 本地请求：投递到对端 mailbox。
	// envelope 的最终 Release 由对端 mailbox 统一处理。
	rpcJob := job.NewRpcJob()
	rpcJob.SetContext(ctx)
	rpcJob.SetPayload(envelope)
	rpcJob.SetPriority(envelope.GetPriority())
	rpcJob.SetDispatcherKey(envelope.GetDispatchKey())
	if err := dispatcher.PostJob(rpcJob); err != nil {
		// PostJob 失败说明未能把 envelope 交给对端 mailbox，当前方需要负责回收。
		rpcJob.Release()
		return err
	}
	return nil
}

func (lc *localSender) IsClosed() bool {
	return atomic.LoadInt32(&lc.closed) == 1
}
