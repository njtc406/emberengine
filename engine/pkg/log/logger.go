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
	"io"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/njtc406/logrus"
)

type AsyncMode struct {
	Enable bool
	Config *AsyncWriterConfig
}

type LevelRoute struct {
	// 要路由到同一 writer 的日志级别集合
	Levels []Level
	// 文件名后缀；空则使用主 Name
	Name string `binding:""`
}

// LevelWriterConf 描述按级别拆分时的 writer 组合配置。
type LevelWriterConf struct {
	// 是否异步写入(默认开启)
	AsyncMode *AsyncMode `binding:""`
	// 每个路由对应一组级别和一个目标 writer
	Routes []LevelRoute
}

type LoggerConf struct {
	// 日志文件路径
	Path string `binding:""`
	// 日志文件名称(可以包含扩展名)
	Name string `binding:""`
	// 日志写入级别 小于设置级别的类型都会被记录
	Level string `binding:"oneof=panic fatal error warn info debug trace"`
	// 是否打印调用者
	Caller bool `binding:""`
	// 是否打印完整调用者
	FullCaller bool `binding:""`
	// 是否打印级别色彩
	Color bool `binding:""`
	// 日志保留时间 min=1m,max=720h 最小1分钟,最大1个月,默认15天
	MaxAge time.Duration `binding:"min=1m,max=720h"`
	// 日志切割时间 min=1m,max=24h 最小1分钟,最大1天,默认1天
	RotationTime time.Duration `binding:"min=1m,max=24h"`

	// LevelWriter 统一控制“单文件/多文件”的按级别路由：
	//   - 未配置且 Name 不为空时，默认生成一个“所有级别 -> Name”的单文件路由；
	//   - 显式配置 Routes 时，可将不同级别路由到不同文件（多文件）。
	LevelWriter *LevelWriterConf `binding:""`
}

// New creates a new Logger object.
func New(opts ...Option) ILogger {
	l := logrus.New()
	//AddHook(&Hook{})
	l.SetBufferPool(bufferPool)
	l.SetFormatter(&Formatter{
		Mu:              new(sync.Mutex),
		TimestampFormat: "2006-01-02 15:04:05.000",
	})
	for _, opt := range opts {
		opt(l)
	}

	return l
}

func fixConf(conf *LoggerConf) *LoggerConf {
	if conf == nil {
		conf = &LoggerConf{
			Path:         "",
			Name:         "",
			Level:        "info",
			Caller:       true,
			FullCaller:   false,
			Color:        false,
			MaxAge:       time.Hour * 24 * 15, // 默认15天
			RotationTime: time.Hour * 24,
			LevelWriter: &LevelWriterConf{
				AsyncMode: &AsyncMode{
					Enable: true,
					Config: &AsyncWriterConfig{
						BufferSize:    65536, // 64kb
						FlushInterval: time.Second,
					},
				},
				Routes: []LevelRoute{
					{
						Levels: []Level{
							PanicLevel,
							FatalLevel,
							ErrorLevel,
							WarnLevel,
							InfoLevel,
							DebugLevel,
							TraceLevel,
						},
						Name: "all",
					},
				},
			},
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

	// 如果 Name 为空，则不写文件，也不启用 LevelWriter
	if conf.Name == "" {
		conf.LevelWriter = nil
		return conf
	}

	// 如果用户未配置 LevelWriter，但提供了 Name，则默认构造“所有级别 -> 单文件”的路由，
	// 这样单文件和多文件在语义上都是基于 LevelRoute 的按级别路由。
	if conf.LevelWriter == nil {
		conf.LevelWriter = &LevelWriterConf{
			AsyncMode: conf.LevelWriter.AsyncMode,
			Routes: []LevelRoute{
				{
					Levels: []Level{
						PanicLevel,
						FatalLevel,
						ErrorLevel,
						WarnLevel,
						InfoLevel,
						DebugLevel,
						TraceLevel,
					},
					Name: "",
				},
			},
		}
	}

	return conf
}

// NewDefaultLogger 创建一个通用日志对象
// filePath 日志输出目录
// conf 日志配置：
//   - 通过 LevelWriter.Routes 将不同级别写入不同文件；
//   - 如果未显式配置 LevelWriter 且 Name 非空，则默认所有级别写入同一个文件（单文件）。
//
// openStdout 是否开启标准输出(如果Name为空,且openStdout未开启,那么将不会有任何日志信息被记录)
// TODO 如果需要远程日志,那么远程日志覆写io.Writer加入到输出就可以了
func NewDefaultLogger(filePath string, conf *LoggerConf, openStdout bool) (ILogger, error) {
	conf = fixConf(conf)
	// TODO 这里gpt改的不对,应该是直接构建一个io.Writer，只是这个writer里面会根据level来判断写入到哪个文件中
	// 统一通过 LevelWriter 构建按级别路由的文件 writers：
	//   - 单文件：fixConf 自动补一个“所有级别 -> Name”的 Route；
	//   - 多文件：用户显式配置多个 Route。
	picker, err := newPicker(path.Join(filePath, conf.Path), conf, openStdout)
	if err != nil {
		return nil, err
	}

	level := strings.ToLower(conf.Level)
	if _, ok := levelMap[level]; !ok {
		level = ErrorLevelStr
	}

	logger := New(
		WithLevel(levelMap[level]),
		WithCaller(conf.Caller),
		WithColor(conf.Color),
		WithFullCaller(conf.FullCaller),
		WithOuterPicker(picker),
	)

	return logger, nil
}

// LoggerX 扩展日志对象,可以在日志中添加一些固定的字段
type LoggerX struct {
	ILogger
	fields Fields
}

func NewLoggerX(logger ILogger, fields Fields) ILogger {
	return &LoggerX{
		ILogger: logger,
		fields:  fields,
	}
}

// LevelRouterWriter 根据日志级别将输出路由到不同的底层 writer。
type LevelRouterWriter struct {
	defaultWriter io.Writer
	routes        map[Level]io.Writer
}

func NewLevelRouterWriter(defaultWriter io.Writer, routes map[Level]io.Writer) io.Writer {
	if len(routes) == 0 {
		return defaultWriter
	}
	return &LevelRouterWriter{defaultWriter: defaultWriter, routes: routes}
}

func (w *LevelRouterWriter) Write(p []byte) (n int, err error) {
	// 这里无法直接看到 Level；真正的按级别路由需要 Hook 支持，
	// 所以 LevelRouterWriter 主要用于概念预留，当前仍由主 writer 写入。
	// 后续如果将 ILogger 扩展为按级别调用不同 writer，可在这里接入。
	return w.defaultWriter.Write(p)
}

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

func Release(logger ILogger) {
	if logger == nil {
		return
	}

	_ = logger.GetOuterPicker().Close()
}
