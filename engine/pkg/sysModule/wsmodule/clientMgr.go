// Package wsmodule
// @Title  websocket连接模块
// @Description  用于管理websocket连接
// @Author  yr  2025/1/21 上午10:33
// @Update  yr  2024/1/21 上午10:33
package wsmodule

import (
	"context"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
	"sync"
	"sync/atomic"

	"github.com/njtc406/emberengine/engine/pkg/core"
	"github.com/njtc406/emberengine/engine/pkg/utils/network"
	"github.com/njtc406/emberengine/engine/pkg/utils/network/processor"
)

type PackMsg struct {
	Id   int32
	Data interface{}
}

func (p *PackMsg) Reset() {
	p.Id = 0
	p.Data = nil
}

func (p *PackMsg) Release() {
	msgPool.Put(p)
}

type WSPackType int8

var msgPool = pool.NewSyncPoolWrapper(
	func() *PackMsg {
		return &PackMsg{}
	},
	pool.NewStatsRecorder("websocket_msg_pool"),
	pool.WithReset(func(p *PackMsg) {
		p.Reset()
	}),
)

const (
	WPTConnected WSPackType = iota
	WPTDisConnected
	WPTPack
	WPTUnknownPack
	WPTReady
	WPTWriteErr
)

type WSPack struct {
	Type      WSPackType //0表示连接 1表示断开 2表示数据
	SessionId int64
	ClientId  string
	Ctx       context.Context
	Data      any
}

type ClientMgr struct {
	core.Module
	processor.IRawProcessor // 消息解析器

	mu        sync.RWMutex
	clients   map[int64]*Client  // map[sessionId]client
	roleIdMap map[string]*Client // map[roleId]client

	sessionIdSeed atomic.Int64 // 会话id种子
}

func NewClientMgr() *ClientMgr {
	return &ClientMgr{}
}

func (m *ClientMgr) OnInit() error {
	m.clients = make(map[int64]*Client)
	m.roleIdMap = make(map[string]*Client)

	m.GetEventProcessor().RegEventReceiverFunc(event.SysEventWebSocket, m.GetEventHandler(), m.wsEventHandler)
	return nil
}

// TODO 这一整块可能都需要重新考虑一下
func (m *ClientMgr) wsEventHandler(e inf.IEvent) {
	pack := e.(*event.Event).Data.(*WSPack)
	switch pack.Type {
	case WPTConnected:
		// 建立连接
		m.addClient(pack.SessionId, pack.Data.(*Client))
		m.IRawProcessor.ConnectedRoute(pack.SessionId, pack.ClientId)
	case WPTDisConnected:
		// 断开连接
		m.IRawProcessor.DisConnectedRoute(pack.SessionId, pack.ClientId)
		m.delClientBySessionId(pack.SessionId)
		// TODO 通知router,玩家已断开
	case WPTReady:
		m.bindingClient(pack.Data.(*Client))
		// TODO 通知router,玩家已就绪
	case WPTUnknownPack:
		// 未知消息
		m.IRawProcessor.UnknownMsgRoute(pack.SessionId, pack.ClientId, pack.Data)
	case WPTPack:
		if err := m.IRawProcessor.MsgRoute(pack.Ctx, pack.SessionId, pack.ClientId, pack.Data); err != nil {
			log.SysLogger.WithContext(pack.Ctx).Errorf("Client router msg error: %s", err)
		}
	}
}

func (m *ClientMgr) OnRelease() {
	if m.clients == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, client := range m.clients {
		client.Close()
	}
	m.clients = nil
	m.roleIdMap = nil
}

func (m *ClientMgr) KickOutAll() {
	if m.clients == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for sessionId, client := range m.clients {
		client.Close()
		delete(m.clients, sessionId)
	}
	m.roleIdMap = make(map[string]*Client)
}

func (m *ClientMgr) NewAgent(conn *network.WSConn) network.Agent {
	return newClient(m, conn)
}

func (m *ClientMgr) SetMsgProcessor(processor processor.IRawProcessor) {
	m.IRawProcessor = processor
}

// addClient 添加一个client
func (m *ClientMgr) addClient(sessionId int64, client *Client) {
	m.mu.Lock()
	m.clients[sessionId] = client
	defer m.mu.Unlock()
}

func (m *ClientMgr) delClientBySessionId(sessionId int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	client := m.clients[sessionId]
	client.Close()
	delete(m.clients, sessionId)
	delete(m.roleIdMap, client.roleId)
}

func (m *ClientMgr) delClientByRoleId(roleId string) {
	if roleId == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	client := m.roleIdMap[roleId]
	client.Close()
	delete(m.clients, client.sessionId)
	delete(m.roleIdMap, roleId)
}

func (m *ClientMgr) bindingClient(client *Client) {
	if client.roleId != "" {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.roleIdMap[client.roleId] = client
	}
}

func (m *ClientMgr) GetClientBySessionId(sessionId int64) *Client {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if c, ok := m.clients[sessionId]; ok {
		return c
	}
	return nil
}

func (m *ClientMgr) GetClientByRoleId(roleId string) *Client {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if c, ok := m.roleIdMap[roleId]; ok {
		return c
	}
	return nil
}

func (m *ClientMgr) GenSessionId() int64 {
	return m.sessionIdSeed.Add(1)
}

func (m *ClientMgr) CastMsgToClient(id int32, msg interface{}) {
	var delList []int64
	defer func() {
		for _, sessionId := range delList {
			m.delClientBySessionId(sessionId)
		}
	}()
	m.mu.RLock()
	defer m.mu.RUnlock()
	for sessionId, client := range m.clients {
		pack := msgPool.Get()
		pack.Id = id
		pack.Data = msg
		if err := client.SendMsg(pack); err != nil {
			// 发送失败, 删除该client
			delList = append(delList, sessionId)
			log.SysLogger.Errorf("CastMsgToClient:send msg error: %s", err)
		}
	}
}

func (m *ClientMgr) RpcSendMsgToClient(roleId string, id int32, msg interface{}) error {
	var err error
	defer func() {
		if err != nil {
			m.delClientByRoleId(roleId)
		}
	}()
	m.mu.RLock()
	defer m.mu.RUnlock()
	if client, ok := m.roleIdMap[roleId]; ok {
		pack := msgPool.Get()
		pack.Id = id
		pack.Data = msg
		if err = client.SendMsg(pack); err != nil {
			log.SysLogger.Errorf("RpcSendMsgToClient:send msg error: %s", err)
			return err
		}
	}
	return nil
}
