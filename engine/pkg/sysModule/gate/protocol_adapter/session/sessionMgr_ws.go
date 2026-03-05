// Package session
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/8/17 0017 0:18
// 最后更新:  yr  2025/8/17 0017 0:18
package session

import (
	"sync/atomic"

	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/shardedlock"
	"github.com/njtc406/emberengine/engine/pkg/utils/syncx"
)

type WebSocketManager struct {
	handler    inf.IAdapterHandler
	seed       atomic.Uint64
	shareLocks *shardedlock.ShardedRWLock
	sessions   syncx.Map[uint64, inf.ISession]
	uidMap     syncx.Map[int64, uint64] // 这是直接使用uid作为玩家索引,如果需要使用roleId做,自行实现
}

func NewWebSocketManager() *WebSocketManager {
	return &WebSocketManager{
		shareLocks: shardedlock.NewShardedRWLock(64),
	}
}

func (m *WebSocketManager) SetHandler(handler inf.IAdapterHandler) {
	m.handler = handler
}

func (m *WebSocketManager) GetHandler() inf.IAdapterHandler {
	return m.handler
}

func (m *WebSocketManager) genSessionID() uint64 {
	return m.seed.Add(1)
}

func (m *WebSocketManager) Bind(uid int64, conn inf.IConn) {
	m.shareLocks.Lock(uid)
	// 先查找一下是否已经绑定了
	if oldSessionId, ok := m.uidMap.Load(uid); ok {
		// TODO 这里可能需要给客户端发送一个消息,所以需要一个hook
		go m.Kick(oldSessionId, "old conn") // 异步踢掉旧的session
	}

	sessionId := m.genSessionID()
	var logger log.ILoggerX
	if provider, ok := m.handler.(interface{ GetLogger() log.ILoggerX }); ok {
		logger = provider.GetLogger()
	}
	session := NewWSSession(sessionId, conn, uid, logger)

	m.sessions.Store(sessionId, session)
	m.uidMap.Store(uid, sessionId)

	m.shareLocks.Unlock(uid)

	if err := m.handler.OnConnect(session); err != nil {
		m.closeSession(session, "onConnect failed")
	}

	session.StartSender() // 启动发送线程
	go m.listen(session)  // 启动接收线程
}

func (m *WebSocketManager) closeSession(session inf.ISession, reason string) {
	if session == nil {
		return
	}
	uid := session.GetUid()
	m.shareLocks.Lock(uid)
	defer m.shareLocks.Unlock(uid)

	sessionId := session.GetSessionId()
	if _, ok := m.sessions.LoadAndDelete(sessionId); !ok {
		return
	}
	m.uidMap.CompareAndDelete(uid, sessionId)

	// TODO 先发送关闭消息(应该需要直接在gate层处理消息的下发,然后异步通知业务连接断开)
	m.handler.OnDisconnect(session, reason)

	session.Close()
	_ = session.GetConn().Close()

	if provider, ok := m.handler.(interface{ GetLogger() log.ILoggerX }); ok {
		if logger := provider.GetLogger(); logger != nil {
			logger.Debugf("user[%d] session %d closed, reason: %s", uid, sessionId, reason)
		}
	}
}

func (m *WebSocketManager) listen(session inf.ISession) {
	defer m.closeSession(session, "read loop exit")

	for !session.IsClosed() {
		msg, err := session.GetConn().ReadMessage()
		if err != nil {
			return
		}
		m.handler.OnMessage(session, msg)
	}
}

func (m *WebSocketManager) Kick(sessionID uint64, reason string) {
	if session, ok := m.sessions.Load(sessionID); ok {
		m.closeSession(session, "kick by sessionId")
	}
}

func (m *WebSocketManager) KickByUid(uid int64) {
	if sessionId, ok := m.uidMap.Load(uid); ok {
		session, ok := m.sessions.Load(sessionId)
		if ok {
			m.closeSession(session, "kick by uid")
		}
	}
}

func (m *WebSocketManager) KickAll() {
	m.sessions.Range(func(_ uint64, v inf.ISession) bool {
		m.closeSession(v, "kick all")
		return true
	})
}

func (m *WebSocketManager) Broadcast(msg []byte) {
	m.sessions.Range(func(_ uint64, v inf.ISession) bool {
		_ = v.GetConn().Send(msg)
		return true
	})
}

func (m *WebSocketManager) GetSession(sessionID uint64) inf.ISession {
	if v, ok := m.sessions.Load(sessionID); ok {
		return v
	}
	return nil
}

func (m *WebSocketManager) GetSessionByUid(uid int64) inf.ISession {
	sessionId, ok := m.uidMap.Load(uid)
	if !ok {
		return nil
	}
	return m.GetSession(sessionId)
}
