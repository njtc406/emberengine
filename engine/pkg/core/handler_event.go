// Package core
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/7/20 0020 10:18
// 最后更新:  yr  2025/7/20 0020 10:18
package core

import (
	"context"

	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

// TODO 下面这些函数做成系统命令

// 具体的系统消息处理器实现
func (s *Service) handleServiceSuspended(ctx context.Context, ev inf.IEvent) error {
	// 服务挂起
	s.mailbox.Suspend()
	return nil
}

func (s *Service) handleServiceResumed(ctx context.Context, ev inf.IEvent) error {
	// 服务恢复
	s.mailbox.Resume()
	return nil
}

func (s *Service) handleServiceHeartbeat(ctx context.Context, ev inf.IEvent) error {
	// 服务健康检查
	// TODO 需要回复服务负载等等信息
	return nil
}
