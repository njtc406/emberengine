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
)

// ILoggerX is the project-wide logging interface used across engine packages.
//
// IMPORTANT: This must NOT be a type alias to a specific logging library,
// otherwise swapping backends becomes a repo-wide refactor.
//
// The method set is intentionally shaped to match current call sites:
//   - WithContext/WithField/WithFields chaining
//   - classic leveled logging + printf variants
type ILoggerX interface {
	WithContext(ctx context.Context) ILoggerX
	WithField(key string, value interface{}) ILoggerX
	WithFields(fields map[string]interface{}) ILoggerX

	// Project-specific tagged variants.
	Slow() ILoggerX
	State() ILoggerX
	Metric() ILoggerX

	Trace(args ...interface{})
	Tracef(format string, args ...interface{})

	Debug(args ...interface{})
	Debugf(format string, args ...interface{})

	Info(args ...interface{})
	Infof(format string, args ...interface{})

	Warn(args ...interface{})
	Warnf(format string, args ...interface{})
	Warning(args ...interface{})
	Warningf(format string, args ...interface{})

	Error(args ...interface{})
	Errorf(format string, args ...interface{})

	Fatal(args ...interface{})
	Fatalf(format string, args ...interface{})

	Panic(args ...interface{})
	Panicf(format string, args ...interface{})
}
