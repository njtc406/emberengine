// Package session
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/8/17 0017 0:17
// 最后更新:  yr  2025/8/17 0017 0:17
package session

import (
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/mpsc"
	"sync/atomic"
	"time"
)

type BaseSession struct {
	id           uint64
	conn         inf.IConn
	closed       atomic.Bool
	uid          string
	msgCh        *mpsc.Queue[[]byte]
	sendStrategy int // 发送策略(1批量发送 2单条发送)
	batchSize    int // 批量发送的批次大小
}

func (s *BaseSession) GetSessionId() uint64 { return s.id }

func (s *BaseSession) GetConn() inf.IConn { return s.conn }
func (s *BaseSession) Close()             { s.closed.Swap(true) }

func (s *BaseSession) IsClosed() bool { return s.closed.Load() }

func (s *BaseSession) GetUid() string { return s.uid }

func (s *BaseSession) Send(data []byte) {
	s.msgCh.Push(data)
}

func (s *BaseSession) StartSender() {
	go func() {
		var backoff = 1
		var maxBackoff = 4

		for !s.IsClosed() {
			data, ok := s.msgCh.Pop()
			if ok {
				if s.sendStrategy == 1 {
					// TODO 批量发送
				} else {
					if err := s.conn.Send(data); err != nil {
						log.SysLogger.Errorf("user[%s] conn[%d] send client pkg error: %v", s.uid, s.id, err)
						// TODO 要不要踢连接,可以考虑做成hook函数,由业务来决定
					}
				}
				continue
			}

			// 使用指数退避来减少忙等开销
			if backoff < maxBackoff {
				backoff *= 2
			}
			time.Sleep(time.Microsecond * time.Duration(backoff))

		}
	}()
}
