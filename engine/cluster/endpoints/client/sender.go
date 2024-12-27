// Package client
// @Title  title
// @Description  desc
// @Author  yr  2024/11/7
// @Update  yr  2024/11/7
package client

import (
	"github.com/njtc406/emberengine/engine/actor"
	"github.com/njtc406/emberengine/engine/def"
	"github.com/njtc406/emberengine/engine/errdef"
	"github.com/njtc406/emberengine/engine/inf"
)

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
	inf.IRpcHandler
	inf.IRpcSenderHandler
}

func (c *Sender) GetPid() *actor.PID {
	return c.pid
}

func (c *Sender) Close() {}

func (c *Sender) SendRequest(envelope inf.IEnvelope) error {
	if c.pid == nil {
		return errdef.ServiceNotFound
	}
	if c.IRpcSenderHandler == nil {
		c.IRpcSenderHandler = handlerMap[c.senderType](c)
	}
	if c.tmp {
		defer c.IRpcSenderHandler.Close()
	}
	return c.IRpcSenderHandler.SendRequest(envelope)
}

func (c *Sender) SendRequestAndRelease(envelope inf.IEnvelope) error {
	if c.pid == nil {
		return errdef.ServiceNotFound
	}
	if c.IRpcSenderHandler == nil {
		c.IRpcSenderHandler = handlerMap[c.senderType](c)
	}
	if c.tmp {
		defer c.IRpcSenderHandler.Close()
	}
	return c.IRpcSenderHandler.SendRequestAndRelease(envelope)
}

func (c *Sender) SendResponse(envelope inf.IEnvelope) error {
	if c.pid == nil {
		return errdef.ServiceNotFound
	}
	if c.IRpcSenderHandler == nil {
		c.IRpcSenderHandler = handlerMap[c.senderType](c)
	}
	if c.tmp {
		defer c.IRpcSenderHandler.Close()
	}
	return c.IRpcSenderHandler.SendResponse(envelope)
}

func NewSender(senderType string, pid *actor.PID, rpcHandler inf.IRpcHandler) inf.IRpcSender {
	return &Sender{
		pid:         pid,
		IRpcHandler: rpcHandler,
		senderType:  senderType,
	}
}

func NewTmpSender(senderType string, pid *actor.PID, rpcHandler inf.IRpcHandler) inf.IRpcSender {
	return &Sender{
		tmp:         true,
		pid:         pid,
		IRpcHandler: rpcHandler,
		senderType:  senderType,
	}
}
