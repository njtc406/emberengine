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

func newLClient(_ string) inf.IRpcSenderHandler {
	return &localSender{}
}

func (lc *localSender) Close() {
	atomic.StoreInt32(&lc.closed, 1)
}

func (lc *localSender) SendRequest(sender inf.IRpcSender, envelope inf.IEnvelope) error {
	if lc.IsClosed() {
		return def.ServiceNotFound
	}

	return sender.PostMessage(envelope)
}

func (lc *localSender) SendResponse(sender inf.IRpcSender, envelope inf.IEnvelope) error {
	monitor.GetRpcMonitor().Remove(envelope.GetReqId()) // 回复时先移除监控,防止超时
	if lc.IsClosed() {
		envelope.SetError(def.ServiceNotFound)
		envelope.Done()
		return def.ServiceNotFound
	}

	if envelope.NeedCallback() {
		// 本地调用的回复消息,直接发送到对应service的邮箱处理
		return sender.PostMessage(envelope)
	} else {
		// 同步调用,直接设置调用结束
		envelope.Done()
	}
	return nil
}

func (lc *localSender) SendRequestAndRelease(sender inf.IRpcSender, envelope inf.IEnvelope) error {
	// 本地调用envelope在接收者处理后释放
	return lc.SendRequest(sender, envelope)
}

func (lc *localSender) IsClosed() bool {
	return atomic.LoadInt32(&lc.closed) == 1
}
