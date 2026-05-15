/*
 * Copyright (c) 2023. YR. All rights reserved
 */

// Package errorlib 提供错误码机制（2023 legacy）。
//
// Deprecated: 新代码应使用 errorx 包 (engine/pkg/utils/errorx)。
// errorx 提供结构化错误码、错误链、结构化字段和 proto 序列化能力。
//
// 迁移指引：
//   - NewErrCode → errorx.New(code, msg)
//   - CombineErr → errorx.CombineErrors (底层使用 errors.Join)
//   - CError.IsCode → errors.Is / errorx.HasCode
//   - CError.GetCode → errorx.CodeFrom
//
// 目前仅 CombineErr 仍有 9 处调用 (rpc/message/msgbus/bus.go)，
// 后续将统一迁移到 errorx.CombineErrors。
package errorlib

import (
	"errors"
	"fmt"
	"log"
	"runtime"
	"strings"
)

type CError interface {
	error

	IsCode(int) bool
	GetCode() int
	GetMsg() string
}

// ErrCode 错误码对象
type ErrCode struct {
	Code   int     // 错误码(这个主要用来给一些地方做判断使用,避免直接判断字符串)
	Msg    string  // 错误信息
	caller *caller // 调用者信息
	preMsg string  // 收集的之前的错误
}

// String 当前错误对象的错误信息
func (e *ErrCode) String() string {
	if e.caller != nil {
		return fmt.Sprintf("%s ---> code: %d, msg: %s", e.caller.string(), e.Code, e.Msg)
	} else {
		return fmt.Sprintf("---> code: %d, msg: %s", e.Code, e.Msg)
	}
}

// Error 返回错误信息
func (e *ErrCode) Error() string {
	return e.getAllErr()
}

func (e *ErrCode) getAllErr() string {
	builder := new(strings.Builder)
	builder.WriteString(e.String())
	if e.preMsg != "" {
		builder.WriteString("\n")
		builder.WriteString(e.preMsg)
	}
	return builder.String()
}

// IsCode 判断错误码是否是code
func (e *ErrCode) IsCode(code int) bool {
	return e.Code == code
}

// GetCode 获取错误码
func (e *ErrCode) GetCode() int {
	return e.Code
}

// GetMsg 获取错误信息
func (e *ErrCode) GetMsg() string {
	return e.Msg
}

// NewErrCode 新建错误码
func NewErrCode(code int, args ...interface{}) CError {
	var msg string
	var preMsg error
	if len(args) > 0 {
		for _, v := range args {
			switch v.(type) {
			case string:
				msg = v.(string)
			case error:
				preMsg = v.(error)
			}
		}
	}

	errCode := &ErrCode{
		Code:   code,
		Msg:    msg,
		caller: nil,
	}

	if preMsg != nil {
		errCode.preMsg = preMsg.Error()
	}

	// 获取上一层调用者
	_, file, line, ok := runtime.Caller(1)
	if ok {
		callerInfo := &caller{
			line: line,
			file: file,
		}
		errCode.caller = callerInfo
	} else {
		log.Printf("code:%d can not get caller info\n", code)
	}
	return errCode
}

// CombineErr 将多个错误合并为一个。
//
// Deprecated: 使用 errorx.CombineErrors 代替，它基于 errors.Join，
// 支持 errors.Is/As 遍历所有子错误。
func CombineErr(errs ...error) error {
	if len(errs) == 0 {
		return nil
	}
	var builder strings.Builder
	for _, v := range errs {
		builder.WriteString(v.Error())
		builder.WriteString("\n")
	}

	return errors.New(builder.String())
}
