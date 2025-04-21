// Package interfaces
// @Title  请填写文件名称（需要改）
// @Description  请填写文件描述（需要改）
// @Author  yr  2024/7/29 下午4:47
// @Update  yr  2024/7/29 下午4:47
package interfaces

import (
	"github.com/njtc406/emberengine/engine/pkg/actor"
)

type IRpcDispatcher interface {
	IMailboxChannel

	SendRequest(envelope IEnvelope) error
	SendRequestAndRelease(envelope IEnvelope) error
	SendResponse(envelope IEnvelope) error

	IActor
	Close()
	IsClosed() bool
}

type IRpcSender interface {
	SendRequest(dispatcher IRpcDispatcher, envelope IEnvelope) error
	SendRequestAndRelease(dispatcher IRpcDispatcher, envelope IEnvelope) error
	SendResponse(dispatcher IRpcDispatcher, envelope IEnvelope) error
	Close()
	IsClosed() bool
}

type IRpcSenderFactory interface {
	GetDispatcher(pid *actor.PID) IRpcDispatcher
}
