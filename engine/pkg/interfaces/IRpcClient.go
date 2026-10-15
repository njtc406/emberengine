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

	// DeliverRequest 投递一条请求 envelope 到目标服务。
	// 语义：调用方在调用后不得再使用该 envelope（所有权转移）。
	// 本地路径：PostJob 到对端 mailbox，由对端处理后 Release。
	// 远端路径：发送完成后由 sender 负责 Release。
	DeliverRequest(ctx context.Context, envelope IEnvelope) error

	// DeliverResponse 投递一条回复 envelope 给调用方。
	// 本地路径：直接唤醒同步 Call 的等待方，或投递异步回调到 mailbox。
	// 远端路径：序列化后通过网络发送回调用节点。
	DeliverResponse(ctx context.Context, envelope IEnvelope) error

	IActor
	Close()
	IsClosed() bool
}

// TODO 还有优化空间,可以参考grpc.ClientConnInterface
type IRpcSender interface {
	// DeliverRequest 将请求 envelope 投递到 dispatcher 指向的目标服务。
	// 调用后 envelope 所有权转移。
	DeliverRequest(ctx context.Context, dispatcher IRpcDispatcher, envelope IEnvelope) error

	// DeliverResponse 将回复 envelope 投递回调用方。
	// 本地路径负责处理 CallState 唤醒；远端路径通过网络发回。
	DeliverResponse(ctx context.Context, dispatcher IRpcDispatcher, envelope IEnvelope) error

	Close()
	IsClosed() bool
}

type IRpcSenderFactory interface {
	GetDispatcher(pid *actor.PID) IRpcDispatcher
}
