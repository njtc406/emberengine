package log

import (
	"fmt"
	"os"
	"path"
	"time"
)

var SysLogger ILogger

type LoggerConf struct {
	Path         string        `binding:""`                                              // 日志文件路径
	Name         string        `binding:""`                                              // 日志文件名称
	Level        string        `binding:"oneof=panic fatal error warn info debug trace"` // 日志写入级别 小于设置级别的类型都会被记录
	Caller       bool          `binding:""`                                              // 是否打印调用者
	FullCaller   bool          `binding:""`                                              // 是否打印完整调用者
	Color        bool          `binding:""`                                              // 是否打印级别色彩
	MaxAge       time.Duration `binding:"min=1m,max=720h"`                               // 日志保留时间 min=1m,max=720h 最小1分钟,最大1个月,默认15天
	RotationTime time.Duration `binding:"min=1m,max=24h"`                                // 日志切割时间 min=1m,max=24h 最小1分钟,最大1天,默认1天
}

func Init(conf *LoggerConf, isDebug bool) {
	if SysLogger != nil {
		return
	}
	logger, err := NewDefaultLogger(
		path.Join(conf.Path, conf.Name),
		conf.Name,
		conf.MaxAge, // 保留15天日志
		conf.RotationTime,
		conf.Level,
		conf.Caller,
		conf.FullCaller,
		conf.Color,
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
