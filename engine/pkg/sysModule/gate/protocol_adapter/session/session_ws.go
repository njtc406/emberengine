// Package sess
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/8/17 0017 0:25
// 最后更新:  yr  2025/8/17 0017 0:25
package session

import (
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/mpsc"
)

type WSSession struct {
	BaseSession
}

func NewWSSession(id uint64, conn inf.IConn, uid int64, logger log.ILoggerX) *WSSession {
	return &WSSession{
		BaseSession: BaseSession{
			id:           id,
			conn:         conn,
			logger:       logger,
			uid:          uid,
			msgCh:        mpsc.New[[]byte](),
			sendStrategy: 0,
			batchSize:    0,
		},
	}
}
