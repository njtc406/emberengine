// Package client
// @Title  本地服务的Client
// @Description  本地服务的Client,调用时直接使用rpcHandler发往对应的service
// @Author  yr  2024/9/3 下午4:26
// @Update  yr  2024/9/3 下午4:26
package client

import (
	"github.com/njtc406/emberengine/engine/internal/monitor"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"sync/atomic"
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

func (lc *localSender) SendRequest(dispatcher inf.IRpcDispatcher, envelope inf.IEnvelope) error {
	if lc == nil || lc.IsClosed() {
		return def.ErrServiceIsClosedOrExited
	}

	// clone一个envelope,保持envelope本身的归属性,不然释放很麻烦
	return dispatcher.PostMessage(envelope.Clone())
}

func (lc *localSender) SendResponse(dispatcher inf.IRpcDispatcher, envelope inf.IEnvelope) error {
	defer envelope.Release()
	originEnvelope := monitor.GetRpcMonitor().Remove(envelope.GetMeta().GetReqId()) // 回复时先移除监控,防止超时
	if originEnvelope == nil {
		return def.ErrEnvelopeNotFound
	}

	if lc == nil || lc.IsClosed() {
		originEnvelope.Release() // 调用者已经下线,丢弃回复
		return def.ErrServiceIsClosedOrExited
	}

	if originEnvelope.GetMeta().NeedCallback() {
		// 本地调用的回复消息,直接发送到对应service的邮箱处理
		return dispatcher.PostMessage(originEnvelope)
	} else {
		// 同步调用,直接设置调用结束
		originEnvelope.SetDone()
	}
	return nil
}

func (lc *localSender) SendRequestAndRelease(dispatcher inf.IRpcDispatcher, envelope inf.IEnvelope) error {
	defer envelope.Release()
	return lc.SendRequest(dispatcher, envelope)
}

func (lc *localSender) IsClosed() bool {
	return atomic.LoadInt32(&lc.closed) == 1
}
