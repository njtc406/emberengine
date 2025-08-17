// Package ws
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/8/17 0017 0:49
// 最后更新:  yr  2025/8/17 0017 0:49
package ws

import (
	"github.com/njtc406/emberengine/engine/pkg/core"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/sysService/gate/limiter"
)

type MsgRouter func(s inf.ISession, packInfo inf.IMessagePack)

type Handler struct {
	core.Module
	sessionMgr inf.ISessionManager
	limiter    *limiter.RateLimiter
	processor  *Processor
	msgRouter  MsgRouter
}

func NewHandler() *Handler {
	return &Handler{ // TODO 之后修改为配置文件
		limiter:   limiter.NewRateLimiter(20),
		processor: NewProcessor(false, false),
	}
}

func (h *Handler) Init(sessionMgr inf.ISessionManager) {
	h.sessionMgr = sessionMgr
}

func (h *Handler) OnConnect(s inf.ISession) error {
	return nil
}

func (h *Handler) OnDisconnect(s inf.ISession) {
	h.sessionMgr.Kick(s.GetSessionId())
}

func (h *Handler) OnMessage(s inf.ISession, msg []byte) {
	// 处理消息
	if !h.limiter.Allow() {
		// 限流!直接踢下线
		h.GetLogger().Errorf("user[%s] connId[%d] reach msg limit, kick out", s.GetUid(), s.GetSessionId())
		// TODO 考虑做成hook函数,由业务来决定
		h.sessionMgr.Kick(s.GetSessionId())
		return
	}

	// 解析消息
	packInfo, err := h.processor.Unmarshal(msg)
	if err != nil {
		// 解析失败!直接踢下线
		// TODO 考虑做成hook函数,由业务来决定
		h.sessionMgr.Kick(s.GetSessionId())
		return
	}
	defer pbPackPool.Put(packInfo.(*PBRawPackInfo))

	h.msgRouter(s, packInfo)
}

func (h *Handler) OnClose(s inf.ISession) {
	h.sessionMgr.Kick(s.GetSessionId())
}

func (h *Handler) SetMsgRouter(router MsgRouter) {
	h.msgRouter = router
}

func (h *Handler) GetProcessor() inf.IMessageProcessor {
	return h.processor
}
