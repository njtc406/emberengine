// Package wsmodule
// @Title  websocket连接模块
// @Description  用于管理websocket连接
// @Author  yr  2025/1/21 上午10:33
// @Update  yr  2024/1/21 上午10:33
package wsmodule

import (
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"sync"
	"sync/atomic"

	"github.com/njtc406/emberengine/engine/pkg/core"
	"github.com/njtc406/emberengine/engine/pkg/utils/network"
	"github.com/njtc406/emberengine/engine/pkg/utils/network/processor"
)

type WSPackType int8

const (
	WPTConnected WSPackType = iota
	WPTDisConnected
	WPTPack
	WPTUnknownPack
	WPTReady
)

type WSPack struct {
	Type      WSPackType //0表示连接 1表示断开 2表示数据
	SessionId int64
	ClientId  string
	TraceId   string
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
		m.delClientByRoleId(pack.ClientId)
		m.delClientBySessionId(pack.SessionId)
	case WPTReady:
		m.bindingClient(pack.Data.(*Client))
	case WPTUnknownPack:
		// 未知消息
		m.IRawProcessor.UnknownMsgRoute(pack.SessionId, pack.ClientId, pack.Data)
	case WPTPack:
		if err := m.IRawProcessor.MsgRoute(pack.SessionId, pack.ClientId, pack.Data); err != nil {
			log.SysLogger.Errorf("Client router msg error: %s", err)
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
	delete(m.clients, sessionId)
}

func (m *ClientMgr) delClientByRoleId(roleId string) {
	if roleId == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
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
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, client := range m.clients {
		if err := client.SendMsg(id, msg); err != nil {
			log.SysLogger.Errorf("CastMsgToClient:send msg error: %s", err)
		}
	}
}
