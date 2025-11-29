// Package client
// @Title  消息发送器
// @Description  用来向对应的服务发送消息
// @Author  yr  2024/11/7
// @Update  yr  2024/11/7
package client

import (
	"sync"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/rpc/client/pool"
)

type SenderCreator func(addr string) inf.IRpcSender

var senderMap = map[string]SenderCreator{
	def.RpcTypeLocal: newLClient,
	def.RpcTypeRpcx:  newRpcxClient,
	def.RpcTypeGrpc:  newGrpcClient,
	def.RpcTypeNats:  newNatsClient,
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

	// 初始化连接池管理器
	poolMgr := pool.GetGlobalPoolManager()

	// 注册RPC创建器到连接池管理器
	for rpcType, creator := range senderMap {
		if rpcType != def.RpcTypeLocal { // 本地类型不需要连接池
			poolMgr.RegisterCreator(rpcType, pool.SenderCreator(creator))
		}
	}
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
	lock.Lock()
	defer lock.Unlock()

	// 检查地址是否已存在
	if tps, ok := senderHandlerMap[addr]; ok {
		// 检查类型是否已存在
		if handler, ok := tps[tp]; ok {
			return handler
		}
		// 类型不存在，创建新handler
		handler := senderMap[tp](addr)
		tps[tp] = handler
		return handler
	}

	// 地址不存在，初始化并创建handler
	handler := senderMap[tp](addr)
	senderHandlerMap[addr] = map[string]inf.IRpcSender{tp: handler}
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
		return def.ErrServiceNotFound
	}

	if c.IMailboxChannel != nil {
		// 本地节点的sender
		if c.localHandler == nil {
			c.localHandler = senderMap[def.RpcTypeLocal]("")
		}

		return c.localHandler.SendRequest(c, envelope)
	}

	return getSenderHandler(c.pid.GetAddress(), c.pid.GetRpcType()).SendRequest(c, envelope)
}

func (c *Dispatcher) SendRequestAndRelease(envelope inf.IEnvelope) error {
	if c.pid == nil {
		return def.ErrServiceNotFound
	}

	if c.IMailboxChannel != nil {
		// 本地节点的sender
		if c.localHandler == nil {
			c.localHandler = senderMap[def.RpcTypeLocal]("")
		}

		return c.localHandler.SendRequestAndRelease(c, envelope)
	}
	return getSenderHandler(c.pid.GetAddress(), c.pid.GetRpcType()).SendRequestAndRelease(c, envelope)
}

func (c *Dispatcher) SendResponse(envelope inf.IEnvelope) error {
	if c.pid == nil {
		return def.ErrServiceNotFound
	}
	if c.IMailboxChannel != nil {
		// 本地节点的sender
		if c.localHandler == nil {
			c.localHandler = senderMap[def.RpcTypeLocal]("")
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
