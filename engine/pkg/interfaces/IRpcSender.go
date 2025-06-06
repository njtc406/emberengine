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

	// TODO 这边的三个接口可以考虑放到包下面,看能不能使用一个envelope来发送消息,这样在批量发送时,就可以只构建一次结构
	SendRequest(envelope IEnvelope) error
	SendRequestAndRelease(envelope IEnvelope) error
	SendResponse(envelope IEnvelope) error

	IActor
	Close()
	IsClosed() bool
}

// TODO 还有优化空间,可以参考grpc.ClientConnInterface
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
