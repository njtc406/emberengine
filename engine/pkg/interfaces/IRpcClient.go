// Package interfaces
// @Title  请填写文件名称（需要改）
// @Description  请填写文件描述（需要改）
// @Author  yr  2024/7/29 下午4:47
// @Update  yr  2024/7/29 下午4:47
package interfaces

import (
	"context"

	"github.com/njtc406/emberengine/engine/pkg/actor"
)

type IRpcDispatcher interface {
	IMailboxChannel

	// Deliver 投递一条 envelope。
	// 语义：调用方在调用后不得再使用该 envelope（所有权转移）。
	// 本地路径：通常会 PostJob 到对端 mailbox，由对端处理后 Release。
	// 远端路径：发送完成后由 sender 负责 Release。
	Deliver(ctx context.Context, envelope IEnvelope) error

	IActor
	Close()
	IsClosed() bool
}

// TODO 还有优化空间,可以参考grpc.ClientConnInterface
type IRpcSender interface {
	// Deliver 由具体 sender 将 envelope 投递到 dispatcher 指向的目标。
	// 语义同 IRpcDispatcher.Deliver：调用后 envelope 所有权转移。
	Deliver(ctx context.Context, dispatcher IRpcDispatcher, envelope IEnvelope) error
	Close()
	IsClosed() bool
}

type IRpcSenderFactory interface {
	GetDispatcher(pid *actor.PID) IRpcDispatcher
}
