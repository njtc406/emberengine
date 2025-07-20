// Package core
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/7/20 0020 10:45
// 最后更新:  yr  2025/7/20 0020 10:45
package core

import inf "github.com/njtc406/emberengine/engine/pkg/interfaces"

type MsgHookFun func(ev inf.IEvent) bool

func (s *Service) RegisterUserMsgHook(fns ...MsgHookFun) {
	for _, fn := range fns {
		s.userMsgHooks = append(s.userMsgHooks, fn)
	}
}

func (s *Service) RegisterSystemMsgHook(fns ...MsgHookFun) {
	for _, fn := range fns {
		s.sysMsgHooks = append(s.sysMsgHooks, fn)
	}
}
