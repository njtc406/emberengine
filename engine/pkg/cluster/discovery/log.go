// Package discovery
// @Title  title
// @Description  desc
// @Author  pc  2024/11/5
// @Update  pc  2024/11/5
package discovery

import "github.com/njtc406/emberengine/engine/pkg/utils/log"

// LogWriter 日志输出(这个是给etcd的client使用的,可以输出内部的日志,目前先不用)
type LogWriter struct {
}

func (l *LogWriter) Write(p []byte) (n int, err error) {
	return log.SysLogger.GetOutput().Write(p)
}

func (l *LogWriter) Sync() error {
	return nil
}
