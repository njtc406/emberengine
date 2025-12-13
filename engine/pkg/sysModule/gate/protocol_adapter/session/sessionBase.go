// Package session
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/8/17 0017 0:17
// 最后更新:  yr  2025/8/17 0017 0:17
package session

import (
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/mpsc"
	"sync"
	"sync/atomic"
	"time"
)

type BaseSession struct {
	id           uint64
	conn         inf.IConn
	closed       atomic.Bool
	uid          int64
	wg           sync.WaitGroup
	msgCh        *mpsc.Queue[[]byte]
	sendStrategy int // 发送策略(1批量发送 2单条发送)
	batchSize    int // 批量发送的批次大小
}

func (s *BaseSession) GetSessionId() uint64 { return s.id }

func (s *BaseSession) GetConn() inf.IConn { return s.conn }
func (s *BaseSession) Close() {
	s.closed.Swap(true)
	s.wg.Wait() // 等待发送线程退出
}

func (s *BaseSession) IsClosed() bool { return s.closed.Load() }

func (s *BaseSession) GetUid() int64 { return s.uid }

func (s *BaseSession) Send(data []byte) {
	s.msgCh.Push(data)
}

func (s *BaseSession) StartSender() {
	s.wg.Add(1)
	go func() {
		var backoff = 1
		var maxBackoff = 4

		defer func() {
			// 将消息队列中的数据发送给客户端
			for !s.msgCh.Empty() {
				data, ok := s.msgCh.Pop()
				if ok {
					if err := s.conn.Send(data); err != nil {
						log.SysLogger.Errorf("user[%d] conn[%d] send client pkg error: %v", s.uid, s.id, err)
						break // 如果已经断开了,则直接退出(需不需要对比一下error?)
					}
				}
			}

			s.wg.Done()
		}()

		for !s.IsClosed() {
			data, ok := s.msgCh.Pop()
			if ok {
				if s.sendStrategy == 1 {
					// TODO 批量发送
				} else {
					if err := s.conn.Send(data); err != nil {
						log.SysLogger.Errorf("user[%d] conn[%d] send client pkg error: %v", s.uid, s.id, err)
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
