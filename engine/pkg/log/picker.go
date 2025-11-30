package log

import (
	"io"
)

type loggerPicker struct {
	router *LevelRouter
}

func newPicker(filePath string, conf *LoggerConf, openStdout bool) (IPicker, error) {
	// 根据配置创建不同的路由
	router, err := buildLevelWriters(filePath, conf, openStdout)
	if err != nil {
		return nil, err
	}

	return &loggerPicker{
		router: router,
	}, nil
}

func (l *loggerPicker) Pick(entry *Entry) io.Writer {
	// 根据日志级别选择 writer
	return l.router.Route(entry.Level)
}
func (l *loggerPicker) Close() error {
	return l.router.Close()
}
