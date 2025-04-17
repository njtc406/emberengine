package log

import (
	"fmt"
	"os"
	"path"
)

var SysLogger ILogger

func Init(conf *LoggerConf, isDebug bool) {
	if SysLogger != nil {
		return
	}
	logger, err := NewDefaultLogger(
		path.Join(conf.Path, conf.Name),
		conf,
		isDebug, // 是否开启前台打印
	)
	if err != nil {
		panic(err)
	}

	SysLogger = logger

	SysLogger.Info("-------->system log init ok<---------")
}

func Close() {
	SysLogger.Info("-------->system log release<---------")
	if err := Release(SysLogger); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
	}
}
