package dto

import (
	"github.com/njtc406/emberengine/engine/pkg/def"
)

// MiddlewareResult 中间件处理结果
type MiddlewareResult struct {
	Action def.MiddlewareAction // 动作
	Err    error                // 拒绝时的错误信息
}

// Continue 创建继续执行的结果
func Continue() MiddlewareResult {
	return MiddlewareResult{Action: def.ActionContinue}
}

// Reject 创建拒绝消息的结果
func Reject(err error) MiddlewareResult {
	return MiddlewareResult{Action: def.ActionReject, Err: err}
}

// Skip 创建跳过后续中间件的结果
func Skip() MiddlewareResult {
	return MiddlewareResult{Action: def.ActionSkip}
}

type MailboxJob[T any] struct {
	payload T
}
