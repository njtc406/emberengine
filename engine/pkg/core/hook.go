// Package core
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/7/20 0020 10:45
// 最后更新:  yr  2025/7/20 0020 10:45
package core

import inf "github.com/njtc406/emberengine/engine/pkg/interfaces"

type MsgHookFun func(ev inf.IEvent) bool

// 废弃
func (s *Service) AddMsgHook(fns ...MsgHookFun) {
	for _, fn := range fns {
		s.msgHooks = append(s.msgHooks, fn)
	}
}

func (s *Service) AddMailboxMiddlewares(middlewares ...inf.IMailboxMiddleware) {
	s.mailboxMiddlewares = append(s.mailboxMiddlewares, middlewares...)
}
