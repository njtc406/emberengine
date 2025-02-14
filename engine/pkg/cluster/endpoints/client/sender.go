// Package client
// @Title  消息发送器
// @Description  用来向对应的服务发送消息
// @Author  yr  2024/11/7
// @Update  yr  2024/11/7
package client

import (
	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"sync"
)

type HandlerCreator func(addr string) inf.IRpcSenderHandler

var handlerMap = map[string]HandlerCreator{
	def.RpcTypeLocal: newLClient,
	def.RpcTypeRpcx:  newRpcxClient,
	def.RpcTypeGrpc:  newGrpcClient,
}

func Register(tp string, creator HandlerCreator) {
	handlerMap[tp] = creator
}

var lock sync.RWMutex

// TODO 可以给这个池子建立一个淘汰机制?比如某些很久才使用一次的连接,可以不用一直维护
var senderHandlerMap map[string]map[string]inf.IRpcSenderHandler

func init() {
	senderHandlerMap = make(map[string]map[string]inf.IRpcSenderHandler)
}

func getSenderHandler(addr string, tp string) inf.IRpcSenderHandler {
	lock.RLock()
	if tps, ok := senderHandlerMap[addr]; ok {
		if handler, ok := tps[tp]; ok {
			lock.RUnlock()
			return handler
		}
		// 不存在该类型的连接,则创建一个
		lock.RUnlock()
		return addSenderHandler(addr, tp)
	}
	lock.RUnlock()

	return addSenderHandler(addr, tp)
}

func addSenderHandler(addr, tp string) inf.IRpcSenderHandler {
	handler := handlerMap[tp](addr)

	lock.Lock()
	defer lock.Unlock()
	if tps, ok := senderHandlerMap[addr]; ok {
		tps[tp] = handler
	} else {
		senderHandlerMap[addr] = make(map[string]inf.IRpcSenderHandler)
		senderHandlerMap[addr][tp] = handler
	}
	return handler
}

func Close() {
	for _, tps := range senderHandlerMap {
		for _, handler := range tps {
			handler.Close()
		}
	}
}

type Sender struct {
	tmp        bool // 是否是临时客户端
	pid        *actor.PID
	senderType string
	inf.IMailboxChannel
	localHandler inf.IRpcSenderHandler
}

func (c *Sender) GetPid() *actor.PID {
	return c.pid
}

func (c *Sender) SetPid(pid *actor.PID) {
	c.pid = pid
}

func (c *Sender) Close() {
	c.pid = nil
}

func (c *Sender) IsClosed() bool {
	return c.pid == nil
}

func (c *Sender) SendRequest(envelope inf.IEnvelope) error {
	if c.pid == nil {
		return def.ServiceNotFound
	}

	if c.IMailboxChannel != nil {
		// 本地节点的sender
		if c.localHandler == nil {
			c.localHandler = handlerMap[def.RpcTypeLocal](c.pid.GetAddress())
		}

		return c.localHandler.SendRequest(c, envelope)
	}

	return getSenderHandler(c.pid.GetAddress(), c.pid.GetRpcType()).SendRequest(c, envelope)
}

func (c *Sender) SendRequestAndRelease(envelope inf.IEnvelope) error {
	if c.pid == nil {
		return def.ServiceNotFound
	}

	if c.IMailboxChannel != nil {
		// 本地节点的sender
		if c.localHandler == nil {
			c.localHandler = handlerMap[def.RpcTypeLocal](c.pid.GetAddress())
		}

		return c.localHandler.SendRequestAndRelease(c, envelope)
	}
	return getSenderHandler(c.pid.GetAddress(), c.pid.GetRpcType()).SendRequestAndRelease(c, envelope)
}

func (c *Sender) SendResponse(envelope inf.IEnvelope) error {
	if c.pid == nil {
		return def.ServiceNotFound
	}
	if c.IMailboxChannel != nil {
		// 本地节点的sender
		if c.localHandler == nil {
			c.localHandler = handlerMap[def.RpcTypeLocal](c.pid.GetAddress())
		}

		return c.localHandler.SendResponse(c, envelope)
	}
	return getSenderHandler(c.pid.GetAddress(), c.pid.GetRpcType()).SendResponse(c, envelope)
}

func NewSender(senderType string, pid *actor.PID, mailbox inf.IMailboxChannel) inf.IRpcSender {
	return &Sender{
		pid:             pid,
		IMailboxChannel: mailbox,
		senderType:      senderType,
	}
}

func NewTmpSender(senderType string, pid *actor.PID, mailbox inf.IMailboxChannel) inf.IRpcSender {
	return &Sender{
		tmp:             true,
		pid:             pid,
		IMailboxChannel: mailbox,
		senderType:      senderType,
	}
}
