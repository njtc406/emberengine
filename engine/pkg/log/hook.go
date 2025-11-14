// Copyright 2023 YR. All rights reserved
// 模块名: 日志钩子函数
// 模块功能简介: 会在日志执行format之前调用注册到日志中的钩子函数中的fire函数

package log

import (
	"fmt"
	"github.com/njtc406/logrus"
)

// CallerHook 增加调用者信息的钩子(由于调用者被集成到了Format函数中,所以这里暂时废弃)
type CallerHook struct{}

func (CallerHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (CallerHook) Fire(entry *logrus.Entry) error {
	if entry.HasCaller() {
		entry.Message = fmt.Sprintf("(%s:%d %s) %s", entry.Caller.File, entry.Caller.Line, entry.Caller.Function, entry.Message)
	}
	return nil
}
