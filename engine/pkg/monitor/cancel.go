// Package monitor
// @Title  title
// @Description  desc
// @Author  yr  2024/11/7
// @Update  yr  2024/11/7
package monitor

import (
	"github.com/njtc406/emberengine/engine/pkg/dto"
)

type RpcCancel struct {
	CallSeq uint64
}

func (rc *RpcCancel) CancelRpc() {
	envelope := GetRpcMonitor().Remove(rc.CallSeq)
	if envelope != nil {
		if envelope.GetData().IsReply() {
			// 已经回复回来了,可能已经放入了待执行队列,重置callback,防止继续执行
			envelope.GetMeta().SetCallback(nil)
		}
		envelope.Release() //取消成功,释放资源
	}
}

func NewRpcCancel(seq uint64) dto.CancelRpc {
	cancel := &RpcCancel{CallSeq: seq}
	return cancel.CancelRpc
}
