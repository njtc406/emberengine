// Package wsmodule
// @Title  websocket连接模块
// @Description  用于管理websocket连接
// @Author  yr  2025/1/21 上午10:33
// @Update  yr  2024/1/21 上午10:33
package wsmodule

import (
	"sync"
	"sync/atomic"

	"github.com/njtc406/emberengine/engine/pkg/core"
	"github.com/njtc406/emberengine/engine/pkg/utils/network"
	"github.com/njtc406/emberengine/engine/pkg/utils/network/processor"
)

type ClientMgr struct {
	core.Module

	processor.IRawProcessor // 消息解析器
	Handler                 // 消息处理器

	clients   sync.Map
	roleIdMap sync.Map

	sessionIdSeed atomic.Uint64 // 会话id
}

func NewClientMgr() *ClientMgr {
	return &ClientMgr{}
}

func (m *ClientMgr) OnInit() error {
	return nil
}

func (m *ClientMgr) OnRelease() {
	m.clients.Range(func(key, value any) bool {
		if c, ok := value.(*Client); ok {
			c.Close()
		}
		return true
	})
}

func (m *ClientMgr) NewAgent(conn *network.WSConn) network.Agent {
	return newClient(m, conn)
}

func (m *ClientMgr) SetMsgProcessor(processor processor.IRawProcessor) {
	m.IRawProcessor = processor
}

func (m *ClientMgr) SetHandler(handler Handler) {
	m.Handler = handler
}

// addClient 添加一个 client 返回值表示是否有重复
func (m *ClientMgr) addClient(key string, client *Client) bool {
	oldClient, loaded := m.clients.Swap(key, client)
	if loaded {
		// 如果 key 已存在，关闭旧的 client
		oldClient.(*Client).Close()
	}
	return !loaded
}

func (m *ClientMgr) delClient(key string) {
	m.clients.Delete(key)
}

func (m *ClientMgr) GetClient(key string) *Client {
	if c, ok := m.clients.Load(key); ok {
		return c.(*Client)
	}
	return nil
}

func (m *ClientMgr) CastMsgToClient(msg interface{}) {
	// 广播使用并发执行
	m.GetService().AsyncDo(func() bool {
		m.clients.Range(func(key, value any) bool {
			value.(*Client).SendMsg(msg)
			return true
		})
		return true
	}, nil)
}
