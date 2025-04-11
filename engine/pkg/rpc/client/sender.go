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

type SenderCreator func(addr string) inf.IRpcSender

var senderMap = map[string]SenderCreator{
	def.RpcTypeLocal: newLClient,
	def.RpcTypeRpcx:  newRpcxClient,
	def.RpcTypeGrpc:  newGrpcClient,
}

// Register 注册消息发送器(目前由于都是在启动阶段注册,没有动态注册,所以就没有给锁,后面有需求再改)
func Register(tp string, creator SenderCreator) {
	senderMap[tp] = creator
}

var lock sync.RWMutex

// TODO 可以给这个池子建立一个淘汰机制?比如某些很久才使用一次的连接,可以不用一直维护
// map[addr][tp]inf.IRpcSender
var senderHandlerMap map[string]map[string]inf.IRpcSender

func init() {
	senderHandlerMap = make(map[string]map[string]inf.IRpcSender)
}

func getSenderHandler(addr string, tp string) inf.IRpcSender {
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

func addSenderHandler(addr, tp string) inf.IRpcSender {
	handler := senderMap[tp](addr)

	lock.Lock()
	defer lock.Unlock()
	if tps, ok := senderHandlerMap[addr]; ok {
		tps[tp] = handler
	} else {
		senderHandlerMap[addr] = make(map[string]inf.IRpcSender)
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

type Dispatcher struct {
	tmp bool // 是否是临时客户端
	pid *actor.PID

	inf.IMailboxChannel
	localHandler inf.IRpcSender
}

func (c *Dispatcher) GetPid() *actor.PID {
	return c.pid
}

func (c *Dispatcher) SetPid(pid *actor.PID) {
	c.pid = pid
}

func (c *Dispatcher) Close() {
	c.pid = nil
}

func (c *Dispatcher) IsClosed() bool {
	return c.pid == nil
}

func (c *Dispatcher) SendRequest(envelope inf.IEnvelope) error {
	if c.pid == nil {
		return def.ServiceNotFound
	}

	if c.IMailboxChannel != nil {
		// 本地节点的sender
		if c.localHandler == nil {
			c.localHandler = senderMap[def.RpcTypeLocal](c.pid.GetAddress())
		}

		return c.localHandler.SendRequest(c, envelope)
	}

	return getSenderHandler(c.pid.GetAddress(), c.pid.GetRpcType()).SendRequest(c, envelope)
}

func (c *Dispatcher) SendRequestAndRelease(envelope inf.IEnvelope) error {
	if c.pid == nil {
		return def.ServiceNotFound
	}

	if c.IMailboxChannel != nil {
		// 本地节点的sender
		if c.localHandler == nil {
			c.localHandler = senderMap[def.RpcTypeLocal](c.pid.GetAddress())
		}

		return c.localHandler.SendRequestAndRelease(c, envelope)
	}
	return getSenderHandler(c.pid.GetAddress(), c.pid.GetRpcType()).SendRequestAndRelease(c, envelope)
}

func (c *Dispatcher) SendResponse(envelope inf.IEnvelope) error {
	if c.pid == nil {
		return def.ServiceNotFound
	}
	if c.IMailboxChannel != nil {
		// 本地节点的sender
		if c.localHandler == nil {
			c.localHandler = senderMap[def.RpcTypeLocal](c.pid.GetAddress())
		}

		return c.localHandler.SendResponse(c, envelope)
	}
	return getSenderHandler(c.pid.GetAddress(), c.pid.GetRpcType()).SendResponse(c, envelope)
}

func NewDispatcher(pid *actor.PID, mailbox inf.IMailboxChannel) inf.IRpcDispatcher {
	return &Dispatcher{
		pid:             pid,
		IMailboxChannel: mailbox,
	}
}

func NewTmpDispatcher(pid *actor.PID, mailbox inf.IMailboxChannel) inf.IRpcDispatcher {
	return &Dispatcher{
		tmp:             true,
		pid:             pid,
		IMailboxChannel: mailbox,
	}
}
