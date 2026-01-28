// Package errorx
// 模块名: 封装error
// 功能描述: 描述
// 作者:  yr  2026/1/29 01:48
// 最后更新:  yr  2026/1/29 01:48
package errorx

type Errorx struct {
	err    error
	caller string
	code   int
	msg    string
	stack  string
	parent *Errorx
}

func NewErrorx() *Errorx {
	return &Errorx{}
}

func (err *Errorx) Error() string {
	// TODO 合并打印所有错误信息
	return err.msg
}

// TODO 类似这样的方法
func (err *Errorx) WrapWithError(errors error) *Errorx {
	return &Errorx{
		err:    errors,
		caller: err.caller,
		code:   err.code,
		msg:    err.msg,
		stack:  err.stack,
		parent: err,
	}
}
