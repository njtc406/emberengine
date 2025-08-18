// Package gate
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/8/17 0017 2:04
// 最后更新:  yr  2025/8/17 0017 2:04
package gate

import (
	"fmt"
	gate_proto "github.com/njtc406/emberengine/engine/pkg/sysModule/gate/proto"
)

func (g *Gate) RpcSendMsgToClientBySessionId(msg *gate_proto.Message) error {
	sessionMgr := g.adapter.GetSessionMgr()
	session := sessionMgr.GetSession(msg.SessionId)
	if session == nil {
		return fmt.Errorf("session not found")
	}

	processor := sessionMgr.GetHandler().GetProcessor()
	data, err := processor.Marshal(msg.Id, msg.Data)
	if err != nil {
		return err
	}

	session.Send(data)
	return nil
}

func (g *Gate) RpcSendMsgToClientByUid(msg *gate_proto.Message) error {
	sessionMgr := g.adapter.GetSessionMgr()
	session := sessionMgr.GetSessionByUid(msg.Uid)
	if session == nil {
		return fmt.Errorf("session not found")
	}

	processor := sessionMgr.GetHandler().GetProcessor()
	data, err := processor.Marshal(msg.Id, msg.Data)
	if err != nil {
		return err
	}
	session.Send(data)
	return nil
}

func (g *Gate) RpcSendMsgToClientsByUid(msg *gate_proto.Message) error {
	if len(msg.Uids) == 0 {
		return fmt.Errorf("uids is empty")
	}
	sessionMgr := g.adapter.GetSessionMgr()
	processor := sessionMgr.GetHandler().GetProcessor()
	data, err := processor.Marshal(msg.Id, msg.Data)
	if err != nil {
		return err
	}

	for _, uid := range msg.Uids {
		session := sessionMgr.GetSessionByUid(uid)
		if session == nil {
			g.GetLogger().Errorf("RpcSendMsgToClientsByUid:role[%s] msg:%d marshal error: %s", uid, msg.Id, err)
			continue
		}
		session.Send(data)
	}
	return nil
}

func (g *Gate) RpcBroadcast(msg *gate_proto.Message) error {
	sessionMgr := g.adapter.GetSessionMgr()
	processor := sessionMgr.GetHandler().GetProcessor()
	data, err := processor.Marshal(msg.Id, msg.Data)
	if err != nil {
		return err
	}
	sessionMgr.Broadcast(data)
	return nil
}

func (g *Gate) RpcKickByUid(req *gate_proto.KickReq) error {
	if req.Uid == "" {
		return fmt.Errorf("uid is empty")
	}
	g.adapter.GetSessionMgr().KickByUid(req.Uid)
	return nil
}

func (g *Gate) RpcKick(req *gate_proto.KickReq) error {
	if req.SessionId == 0 {
		return fmt.Errorf("sessionId is empty")
	}
	g.adapter.GetSessionMgr().Kick(req.SessionId, req.Reason)
	return nil
}

func (g *Gate) RpcKickAll() error {
	sessionMgr := g.adapter.GetSessionMgr()
	sessionMgr.Broadcast([]byte{}) // 发送一个空包,通知所有用户断线
	// 断开所有连接
	sessionMgr.KickAll()
	return nil
}

func (g *Gate) RpcGetClientIp(req *gate_proto.GetClientIpReq) (*gate_proto.GetClientIpResp, error) {
	sessionMgr := g.adapter.GetSessionMgr()
	session := sessionMgr.GetSessionByUid(req.Uid)
	if session == nil {
		return nil, fmt.Errorf("session not found")
	}
	return &gate_proto.GetClientIpResp{
		Ip: session.GetConn().GetClientIp(),
	}, nil
}
