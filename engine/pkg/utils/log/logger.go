/*
 * Copyright (c) 2024. YR. All rights reserved
 */

// Package log
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2024/3/2 0002 18:57
// 最后更新:  yr  2024/3/2 0002 18:57
package log

import (
	"bytes"
	"github.com/njtc406/logrus"
	"io"
	"os"
	"path"
	"strings"
	"sync"
	"time"
)

var locker sync.Mutex
var writerLog = map[ILogger]io.WriteCloser{}

func logWriter(logger ILogger, writer io.WriteCloser) {
	locker.Lock()
	defer locker.Unlock()

	if _, ok := writerLog[logger]; ok {
		return
	}

	writerLog[logger] = writer
}

func logRelease(logger ILogger) {
	locker.Lock()
	defer locker.Unlock()
	if writer, ok := writerLog[logger]; ok {
		_ = writer.Close()
		delete(writerLog, logger)
	}
}

type AsyncMode struct {
	Enable bool
	Config *AsyncWriterConfig
}

type LoggerConf struct {
	Path         string        `binding:""`                                              // 日志文件路径
	Name         string        `binding:""`                                              // 日志文件名称
	Level        string        `binding:"oneof=panic fatal error warn info debug trace"` // 日志写入级别 小于设置级别的类型都会被记录
	AsyncMode    *AsyncMode    `binding:""`                                              // 是否异步写入
	Caller       bool          `binding:""`                                              // 是否打印调用者
	FullCaller   bool          `binding:""`                                              // 是否打印完整调用者
	Color        bool          `binding:""`                                              // 是否打印级别色彩
	MaxAge       time.Duration `binding:"min=1m,max=720h"`                               // 日志保留时间 min=1m,max=720h 最小1分钟,最大1个月,默认15天
	RotationTime time.Duration `binding:"min=1m,max=24h"`                                // 日志切割时间 min=1m,max=24h 最小1分钟,最大1天,默认1天
}

// New creates a new Logger object.
func New(opts ...Option) ILogger {
	l := logrus.New()
	//AddHook(&Hook{})
	l.SetFormatter(&Formatter{
		Mu:              new(sync.Mutex),
		TimestampFormat: "2006-01-02 15:04:05.000",
		//Colors:          false,
		//TrimMessages:    false,
		//NoCaller:        false,
		bufPool: &defaultPool{
			pool: &sync.Pool{
				New: func() interface{} {
					return new(bytes.Buffer)
				},
			},
		},
	})
	for _, opt := range opts {
		opt(l)
	}

	return l
}

func fixConf(conf *LoggerConf) *LoggerConf {
	if conf == nil {
		conf = &LoggerConf{
			Path:  "",
			Name:  "",
			Level: "info",
			AsyncMode: &AsyncMode{
				Enable: false,
				Config: nil,
			},
			Caller:       false,
			FullCaller:   false,
			Color:        false,
			MaxAge:       time.Hour * 24 * 15, // 默认15天
			RotationTime: time.Hour * 24,
		}
	}

	if conf.Level == "" {
		conf.Level = "info"
	}

	if conf.MaxAge == 0 {
		conf.MaxAge = time.Hour * 24 * 15
	}

	if conf.RotationTime == 0 {
		conf.RotationTime = time.Hour * 24
	}

	return conf
}

// NewDefaultLogger 创建一个通用日志对象
// filePath 日志输出目录
// fileName 日志文件名(最终文件名会是 filePath/fileName_20060102150405.log)(fileName为空且开启标准输出的情况下默认输出到stdout,否则无任何输出)
// maxAge 最大存放时间(过期会自动删除)
// rotationTime 自动切分间隔(到期日志自动切换新文件)
// level 日志级别(小于设置级别的信息都会被记录打印,设置级别如果超出限制,默认日志等级为error,取值为panic fatal error warn info debug trace)
// withCaller 是否需要调用者信息
// fullCaller 如果需要打印调用者信息,那么这个参数可以设置调用者信息的详细程度
// withColors 是否需要信息的颜色(基本上只能用于linux的前台打印)
// asyncMode 是否开启异步模式
// openStdout 是否开启标准输出(如果fileName为空,且openStdout未开启,那么将不会有任何日志信息被记录)
// TODO 如果需要远程日志,那么远程日志覆写io.Writer加入到输出就可以了
func NewDefaultLogger(filePath string, conf *LoggerConf, openStdout bool) (ILogger, error) {
	conf = fixConf(conf)
	var writers []io.Writer

	if len(conf.Name) > 0 {
		if len(filePath) == 0 {
			filePath = "./" // 默认当前目录
		}
		if conf.RotationTime < time.Second*60 || conf.RotationTime > time.Hour*24 {
			return nil, RotationTimeErr
		}
		pattern := "_%Y%m%d.log"
		if conf.RotationTime < time.Minute*60 {
			pattern = "_%Y%m%d%H%M.log"
		} else if conf.RotationTime < time.Hour*24 {
			pattern = "_%Y%m%d%H.log"
		}

		w, err := rotateNew(
			path.Join(filePath, conf.Name),
			WithMaxAge(conf.MaxAge),
			WithRotationTime(conf.RotationTime),
			WithPattern(pattern),
		)
		if err != nil {
			_ = w.Close()
			return nil, err
		} else {
			writers = append(writers, w)
		}
	}

	if openStdout {
		writers = append(writers, os.Stdout)
	} else {
		writers = append(writers, io.Discard)
	}

	level := strings.ToLower(conf.Level)
	if _, ok := levelMap[level]; !ok {
		level = "error"
	}

	var writerCloser io.WriteCloser
	var writer io.Writer
	if conf.AsyncMode != nil && conf.AsyncMode.Enable {
		// 开启了异步模式,使用异步writer代替同步writer
		w := NewAsyncWriter(
			io.MultiWriter(writers...),
			conf.AsyncMode.Config,
		)
		writer = w
		// 记录异步模式的writer,用于close的时候释放
		writerCloser = w
	} else {
		writer = io.MultiWriter(writers...)
	}

	logger := New(
		WithLevel(levelMap[level]),
		WithCaller(conf.Caller),
		WithColor(conf.Color),
		WithOut(writer),
		WithFullCaller(conf.FullCaller),
	)

	// 由于是追加模式,所以默认为无锁(gpt认为这里在多线程环境中可能会产生一些问题,在使用中确实遇到过)
	//logger.SetNoLock()

	if writerCloser != nil {
		logWriter(logger, writerCloser)
	}

	return logger, nil
}

func Release(logger ILogger) {
	if logger == nil {
		return
	}

	logRelease(logger)
}
