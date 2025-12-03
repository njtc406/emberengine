/*
 * Copyright (c) 2024. YR. All rights reserved
 */

// Package log
// 模块名: 日志接口定义
// 功能描述: 日志接口
// 作者:  yr  2024/3/1 0001 11:12
// 最后更新:  yr  2024/3/1 0001 11:12
package log

import (
	"context"

	"github.com/njtc406/logrus"
)

type ILogger = logrus.ILogger

type ILoggerX interface {
	WithFields(Fields) *Entry
	WithField(string, interface{}) *Entry
	WithContext(ctx context.Context) *Entry

	Trace(args ...interface{})
	Debug(args ...interface{})
	Info(args ...interface{})
	Warn(args ...interface{})
	Error(args ...interface{})
	Fatal(args ...interface{})
	Panic(args ...interface{})

	Tracef(format string, args ...interface{})
	Debugf(format string, args ...interface{})
	Infof(format string, args ...interface{})
	Warnf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
	Fatalf(format string, args ...interface{})
	Panicf(format string, args ...interface{})

	Traceln(args ...interface{})
	Debugln(args ...interface{})
	Infoln(args ...interface{})
	Warnln(args ...interface{})
	Errorln(args ...interface{})
	Fatalln(args ...interface{})
	Panicln(args ...interface{})
}
