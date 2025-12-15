// Package client
// @Title  本地服务的Client
// @Description  本地服务的Client,调用时直接使用rpcHandler发往对应的service
// @Author  yr  2024/9/3 下午4:26
// @Update  yr  2024/9/3 下午4:26
package client

import (
	"context"
	"sync/atomic"

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
		if envelope != nil {
			envelope.Release()
		}
		return def.ErrServiceIsClosedOrExited
	}
	if envelope == nil {
		return nil
	}

	data := envelope.GetData()
	if data != nil && data.IsReply() {
		// 本地回复：reply 复用的是“当前正在处理的请求 envelope”，
		// envelope 的最终释放由对端 mailbox 的 InvokeMessage 统一负责。
		// 这里提前 Release 会导致对象过早回到池里，被并发复用后出现 meta/data=nil 等异常。
		state := monitor.GetRpcMonitor().Remove(envelope.GetMeta().GetReqId())
		if state == nil {
			return def.ErrEnvelopeNotFound
		}
		state.SetResult(data.GetResponse(), data.GetError())
		state.Complete()
		envelope.Release()
		return nil
	}

	// 本地请求：投递到对端 mailbox。
	// envelope 的最终 Release 由对端 mailbox 统一处理。
	if err := dispatcher.PostMessage(ctx, envelope); err != nil {
		// PostMessage 失败说明未能把 envelope 交给对端 mailbox，当前方需要负责回收。
		envelope.Release()
		return err
	}
	return nil
}

func (lc *localSender) IsClosed() bool {
	return atomic.LoadInt32(&lc.closed) == 1
}
