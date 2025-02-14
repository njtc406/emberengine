// Package client
// @Title  title
// @Description  desc
// @Author  yr  2024/11/7
// @Update  yr  2024/11/7
package client

import (
	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

// TODO sender的可能优化?目前是每个service都会创建一个自己的sender,但是实际上都是连接的node的监听接口,
// 所以可以考虑优化为给不同的node只有一个sender,给这个node发送都使用相同的sender,可以减少连接数量,集群能兼容更多的node
// 这里需要考虑的还有一个就是私有服务的回调,由于私有服务的node是不会注册的,所以集群中可能没有node信息,那么pid中还是需要带上addr
// 根据节点id找到该节点的sender,如果找不到,那么就创建一个临时的sender,临时sender的关闭还是和现在一样,采用idle机制,长时间未使用就关闭
// 这里需要注意的是如果修改为共有sender之后,那么sender必须实现pool,且线程安全,目前使用的rpcx和grpc都是线程安全的,所以不需要考虑线程安全
// 改动可能比较大,可能需要修改很多模块的东西,仔细考虑一下再来改

type HandlerCreator func(sender inf.IRpcSender) inf.IRpcSenderHandler

var handlerMap = map[string]HandlerCreator{
	def.RpcTypeLocal: newLClient,
	def.RpcTypeRpcx:  newRpcxClient,
	def.RpcTypeGrpc:  newGrpcClient,
}

func Register(tp string, creator HandlerCreator) {
	handlerMap[tp] = creator
}

type Sender struct {
	tmp        bool // 是否是临时客户端
	pid        *actor.PID
	senderType string
	inf.IMailbox
	inf.IRpcSenderHandler
}

func (c *Sender) GetPid() *actor.PID {
	return c.pid
}

func (c *Sender) SetPid(pid *actor.PID) {
	c.pid = pid
}

func (c *Sender) Close() {
	if c.IRpcSenderHandler != nil {
		c.IRpcSenderHandler.Close()
	}
}

func (c *Sender) SendRequest(envelope inf.IEnvelope) error {
	if c.pid == nil {
		return def.ServiceNotFound
	}
	if c.IRpcSenderHandler == nil {
		c.IRpcSenderHandler = handlerMap[c.senderType](c)
	}

	return c.IRpcSenderHandler.SendRequest(envelope)
}

func (c *Sender) SendRequestAndRelease(envelope inf.IEnvelope) error {
	if c.pid == nil {
		return def.ServiceNotFound
	}
	if c.IRpcSenderHandler == nil {
		c.IRpcSenderHandler = handlerMap[c.senderType](c)
	}

	return c.IRpcSenderHandler.SendRequestAndRelease(envelope)
}

func (c *Sender) SendResponse(envelope inf.IEnvelope) error {
	if c.pid == nil {
		return def.ServiceNotFound
	}
	if c.IRpcSenderHandler == nil {
		c.IRpcSenderHandler = handlerMap[c.senderType](c)
	}

	return c.IRpcSenderHandler.SendResponse(envelope)
}

func (c *Sender) IsClosed() bool {
	if c.IRpcSenderHandler == nil {
		return true
	}
	return c.IRpcSenderHandler.IsClosed()
}

func NewSender(senderType string, pid *actor.PID, mailbox inf.IMailbox) inf.IRpcSender {
	return &Sender{
		pid:        pid,
		IMailbox:   mailbox,
		senderType: senderType,
	}
}

func NewTmpSender(senderType string, pid *actor.PID, mailbox inf.IMailbox) inf.IRpcSender {
	return &Sender{
		tmp:        true,
		pid:        pid,
		IMailbox:   mailbox,
		senderType: senderType,
	}
}
