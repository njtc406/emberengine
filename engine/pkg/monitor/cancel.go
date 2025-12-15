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
	state := GetRpcMonitor().Remove(rc.CallSeq)
	if state != nil {
		// 取消后不再触发回调
		state.callbacks = nil
		state.cbParams = nil
		state.Release()
	}
}

func NewRpcCancel(seq uint64) dto.CancelRpc {
	cancel := &RpcCancel{CallSeq: seq}
	return cancel.CancelRpc
}
