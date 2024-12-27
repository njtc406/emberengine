// Package inf
// @Title  请填写文件名称（需要改）
// @Description  请填写文件描述（需要改）
// @Author  yr  2024/7/29 下午4:47
// @Update  yr  2024/7/29 下午4:47
package inf

import "github.com/njtc406/emberengine/engine/actor"

type IRpcSender interface {
	IRpcHandler
	IRpcSenderHandler

	GetPid() *actor.PID
}

type IRpcSenderHandler interface {
	Close()
	// SendRequest 发送请求消息
	SendRequest(envelope IEnvelope) error
	SendRequestAndRelease(envelope IEnvelope) error
	SendResponse(envelope IEnvelope) error
}

type IRpcSenderFactory interface {
	GetSender(pid *actor.PID) IRpcSender
}
