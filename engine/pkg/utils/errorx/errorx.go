// Package errorx 提供增强的错误处理能力。
//
// 核心能力:
//   - 错误码: 支持 errors.Is 按码匹配，跨多层 Wrap 仍可准确分支
//   - 错误链: 标准 Unwrap 协议，errors.Is/errors.As 自动遍历
//   - 调用位置: 每个 Error 自动记录创建处 file:line
//   - 结构化上下文: WithField 附加键值对，跨层传递不丢失
//
// 使用示例:
//
//	// 定义 sentinel (通常在 def/ 包)
//	var ErrRPCTimeout = errorx.New(1001, "rpc call timeout")
//
//	// 底层返回
//	return errorx.WrapWithCode(err, 1001, "call UserService timeout").
//	    WithField("service", "UserService").WithField("method", "Login")
//
//	// 中间层 wrap
//	return errorx.Wrap(err, "handle login failed")
//
//	// 上层按码分支
//	if errors.Is(err, ErrRPCTimeout) { ... }       // ✅ 跨层匹配
//	if errorx.HasCode(err, 1001)     { ... }       // ✅ 显式查找
//	code := errorx.CodeFrom(err)                    // ✅ 提取首个 code
//	root := errorx.RootCause(err)                   // ✅ 最底层原因
//	fields := errorx.AllFields(err)                 // ✅ 收集全链路 fields
package errorx

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
)

// Field 是附加到错误上的结构化上下文键值对。
type Field struct {
	Key string
	Val any
}

// Error 增强型错误，支持错误码、错误链、调用位置和结构化上下文。
type Error struct {
	code   int     // 错误码，0 表示未设置
	msg    string  // 当前层错误描述
	cause  error   // 被包装的底层错误 (Unwrap 链)
	file   string  // 创建位置文件路径
	line   int     // 创建位置行号
	fields []Field // 结构化上下文
}

// ============================================================
// 创建
// ============================================================

// New 创建带错误码和消息的 Error。
func New(code int, msg string) *Error {
	e := &Error{code: code, msg: msg}
	e.captureCallerSkip(2)
	return e
}

// NewMsg 创建仅带消息的 Error (code = 0)。
func NewMsg(msg string) *Error {
	e := &Error{msg: msg}
	e.captureCallerSkip(2)
	return e
}

// Wrap 包装已有 error，附加描述。err 为 nil 时返回 nil。
func Wrap(err error, msg string) *Error {
	if err == nil {
		return nil
	}
	e := &Error{msg: msg, cause: err}
	e.captureCallerSkip(2)
	return e
}

// Wrapf 包装已有 error，附加格式化描述。err 为 nil 时返回 nil。
func Wrapf(err error, format string, args ...any) *Error {
	if err == nil {
		return nil
	}
	e := &Error{msg: fmt.Sprintf(format, args...), cause: err}
	e.captureCallerSkip(2)
	return e
}

// WrapWithCode 包装已有 error，附加错误码和描述。err 为 nil 时返回 nil。
func WrapWithCode(err error, code int, msg string) *Error {
	if err == nil {
		return nil
	}
	e := &Error{code: code, msg: msg, cause: err}
	e.captureCallerSkip(2)
	return e
}

func (e *Error) captureCallerSkip(skip int) {
	_, file, line, ok := runtime.Caller(skip)
	if ok {
		e.file = file
		e.line = line
	}
}

// ============================================================
// 富化 (返回新 *Error，不修改原件 — sentinel 安全)
// ============================================================

// WithField 返回新 Error，附加一个上下文键值对。
func (e *Error) WithField(key string, val any) *Error {
	c := e.clone()
	c.fields = append(c.fields, Field{Key: key, Val: val})
	return c
}

// WithFields 返回新 Error，附加多个上下文键值对。
func (e *Error) WithFields(fields ...Field) *Error {
	if len(fields) == 0 {
		return e
	}
	c := e.clone()
	c.fields = append(c.fields, fields...)
	return c
}

// WithMsg 返回新 Error，替换描述消息。
func (e *Error) WithMsg(msg string) *Error {
	c := e.clone()
	c.msg = msg
	return c
}

func (e *Error) clone() *Error {
	c := &Error{
		code:  e.code,
		msg:   e.msg,
		cause: e.cause,
		file:  e.file,
		line:  e.line,
	}
	if len(e.fields) > 0 {
		c.fields = make([]Field, len(e.fields))
		copy(c.fields, e.fields)
	}
	return c
}

// ============================================================
// error 接口 + 标准 errors 协议
// ============================================================

// Error 返回完整错误链的单行描述。
//
// 格式示例:
//
//	[1002] service unavailable {service=UserSvc} (handler.go:45) | caused by: [1001] rpc timeout (bus.go:123) | caused by: context deadline exceeded
func (e *Error) Error() string {
	var b strings.Builder
	e.writeSelf(&b)
	for cause := e.cause; cause != nil; {
		b.WriteString(" | caused by: ")
		if ex, ok := cause.(*Error); ok {
			ex.writeSelf(&b)
			cause = ex.cause
		} else {
			b.WriteString(cause.Error())
			break
		}
	}
	return b.String()
}

// Detail 返回多行缩进格式的错误链，适合日志输出。
//
// 格式示例:
//
//	[1002] service unavailable {service=UserSvc} (handler.go:45)
//	  └─ [1001] rpc timeout (bus.go:123)
//	    └─ context deadline exceeded
func (e *Error) Detail() string {
	var b strings.Builder
	indent := ""
	var cur error = e
	for cur != nil {
		if ex, ok := cur.(*Error); ok {
			b.WriteString(indent)
			ex.writeSelf(&b)
			b.WriteByte('\n')
			cur = ex.cause
			if indent == "" {
				indent = "  └─ "
			} else {
				indent = "  " + indent
			}
		} else {
			b.WriteString(indent)
			b.WriteString(cur.Error())
			b.WriteByte('\n')
			break
		}
	}
	return b.String()
}

func (e *Error) writeSelf(b *strings.Builder) {
	if e.code != 0 {
		fmt.Fprintf(b, "[%d] ", e.code)
	}
	b.WriteString(e.msg)
	if len(e.fields) > 0 {
		b.WriteString(" {")
		for i, f := range e.fields {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(b, "%s=%v", f.Key, f.Val)
		}
		b.WriteByte('}')
	}
	if e.file != "" {
		fmt.Fprintf(b, " (%s:%d)", shortFile(e.file), e.line)
	}
}

// shortFile 提取文件路径的最后两段 (package/file.go)
func shortFile(path string) string {
	// 从后往前找两个分隔符
	count := 0
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			count++
			if count == 2 {
				return path[i+1:]
			}
		}
	}
	return path
}

// Unwrap 返回被包装的底层 error，兼容 errors.Is/errors.As 链式遍历。
func (e *Error) Unwrap() error {
	return e.cause
}

// Is 判断当前 error 是否与 target 匹配——按错误码匹配。
// 当 code != 0 且 target 也是 *Error 且 code 相同时返回 true。
// 这使得 errors.Is(wrappedErr, sentinelErr) 跨多层 Wrap 仍可按码命中。
func (e *Error) Is(target error) bool {
	if t, ok := target.(*Error); ok {
		return e.code != 0 && e.code == t.code
	}
	return false
}

// ============================================================
// 读取
// ============================================================

// Code 返回错误码。
func (e *Error) Code() int { return e.code }

// Message 返回当前层的错误描述（不含 cause 链）。
func (e *Error) Message() string { return e.msg }

// GetFields 返回当前 Error 上附加的上下文字段。
func (e *Error) GetFields() []Field { return e.fields }

// Caller 返回创建位置的文件路径和行号。
func (e *Error) Caller() (file string, line int) { return e.file, e.line }

// ============================================================
// 包级辅助函数
// ============================================================

// CodeFrom 从错误链中提取第一个非零错误码。
// 返回 0 表示链中没有 *Error 或所有 code 均为 0。
func CodeFrom(err error) int {
	for err != nil {
		if e, ok := err.(*Error); ok && e.code != 0 {
			return e.code
		}
		err = errors.Unwrap(err)
	}
	return 0
}

// HasCode 判断错误链中是否存在指定错误码。
func HasCode(err error, code int) bool {
	for err != nil {
		if e, ok := err.(*Error); ok && e.code == code {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}

// RootCause 返回错误链中最底层的原始错误。
func RootCause(err error) error {
	for {
		cause := errors.Unwrap(err)
		if cause == nil {
			return err
		}
		err = cause
	}
}

// AllFields 收集错误链中所有 *Error 上附加的 Field（从外到内）。
func AllFields(err error) []Field {
	var result []Field
	for err != nil {
		if e, ok := err.(*Error); ok && len(e.fields) > 0 {
			result = append(result, e.fields...)
		}
		err = errors.Unwrap(err)
	}
	return result
}

// CombineErrors 合并多个 error。nil 被过滤，全部为 nil 时返回 nil。
// 使用 errors.Join 保留类型信息（errors.Is/errors.As 可遍历所有子错误）。
func CombineErrors(errs ...error) error {
	var real []error
	for _, e := range errs {
		if e != nil {
			real = append(real, e)
		}
	}
	switch len(real) {
	case 0:
		return nil
	case 1:
		return real[0]
	default:
		return errors.Join(real...)
	}
}
