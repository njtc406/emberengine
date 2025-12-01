package log

import (
	"io"
	"os"
)

type loggerPicker struct {
	router *LevelRouter
}

func newPicker(conf *LoggerConf) (IPicker, error) {
	// 根据配置创建不同的路由
	router, err := buildLevelWriters(conf)
	if err != nil {
		return nil, err
	}
	if router == nil {
		// 构造兜底路由：根据 openStdout 决定输出到控制台或丢弃
		var fallback io.Writer
		if conf.Stdout {
			fallback = os.Stdout
		} else {
			fallback = io.Discard
		}
		router = &LevelRouter{
			routers: map[Level]io.Writer{
				PanicLevel: fallback,
				FatalLevel: fallback,
				ErrorLevel: fallback,
				WarnLevel:  fallback,
				InfoLevel:  fallback,
				DebugLevel: fallback,
				TraceLevel: fallback,
			},
			closers: nil,
		}
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
