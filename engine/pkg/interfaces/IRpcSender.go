// Package interfaces
// @Title  请填写文件名称（需要改）
// @Description  请填写文件描述（需要改）
// @Author  yr  2024/7/29 下午4:47
// @Update  yr  2024/7/29 下午4:47
package interfaces

import (
	"github.com/njtc406/emberengine/engine/pkg/actor"
)

type IRpcSender interface {
	IMailboxChannel

	SendRequest(envelope IEnvelope) error
	SendRequestAndRelease(envelope IEnvelope) error
	SendResponse(envelope IEnvelope) error

	GetPid() *actor.PID
	SetPid(pid *actor.PID)
	Close()
	IsClosed() bool
}

type IRpcSenderHandler interface {
	SendRequest(sender IRpcSender, envelope IEnvelope) error
	SendRequestAndRelease(sender IRpcSender, envelope IEnvelope) error
	SendResponse(sender IRpcSender, envelope IEnvelope) error
	Close()
	IsClosed() bool
}

type IRpcSenderDispatcher interface {
	// SendRequest 发送请求消息
	SendRequest(sender IRpcSender, envelope IEnvelope) error
	SendRequestAndRelease(sender IRpcSender, envelope IEnvelope) error
	SendResponse(sender IRpcSender, envelope IEnvelope) error
}

type IRpcSenderFactory interface {
	GetSender(pid *actor.PID) IRpcSender
}
