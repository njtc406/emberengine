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
	"strings"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/utils/timelib"
	"github.com/njtc406/logrus"
)

type AsyncMode struct {
	Enable bool
	Config *AsyncWriterConfig
}

type LevelRoute struct {
	// 要路由到同一 writer 的日志级别集合
	Levels []string
	// 文件名后缀；空则使用主 Name
	Name string `binding:""`
}

// LevelWriterConf 描述按级别拆分时的 writer 组合配置。
/* Deleted: LevelWriterConf obsolete */
// 统一的切割配置
type RotationConf struct {
	MaxAge  time.Duration `binding:"min=1m,max=720h"`
	Every   time.Duration `binding:"min=1m,max=24h"`
	Pattern string        `binding:""`
}

// 统一的路由配置
type RoutingConf struct {
	AsyncMode *AsyncMode `binding:""`
	Routes    []LevelRoute
}

type LoggerConf struct {
	// 统一命名
	Dir string `binding:""`
	// 日志文件名前缀
	PrefixName string `binding:""`
	// 日志级别
	Level string `binding:"oneof=panic fatal error warn info debug trace"`
	// 是否打印到标准输出
	Stdout bool `binding:""`
	// 是否打印调用者
	Caller bool `binding:""`
	// 是否打印完整调用者
	FullCaller bool `binding:""`
	// 是否打印级别色彩
	Color bool `binding:""`
	// 切割与路由
	Rotation RotationConf `binding:""`
	Routing  RoutingConf  `binding:""`
}

// New creates a new Logger object.
func New(isDebug bool, opts ...Option) *Logger {
	l := logrus.New()
	l.SetBufferPool(getBufferPool(isDebug))
	l.SetTimeFunc(timelib.Now)
	l.SetFormatter(&Formatter{
		TimestampFormat: defaultTimeFormat,
	})
	for _, opt := range opts {
		opt(l)
	}

	return l
}

func fixConf(conf *LoggerConf) *LoggerConf {
	if conf == nil {
		conf = &LoggerConf{}
	}
	// 默认值
	if conf.Stdout == false {
		conf.Stdout = true
	}
	if conf.Caller == false {
		conf.Caller = true
	}
	if conf.Level == "" {
		conf.Level = "info"
	}
	if conf.Rotation.MaxAge == 0 {
		conf.Rotation.MaxAge = time.Hour * 24 * 15
	}
	if conf.Rotation.Every == 0 {
		conf.Rotation.Every = time.Hour * 24
	}
	// 如果 PrefixName 为空，则不写文件，也不启用路由
	if conf.PrefixName == "" {
		conf.Routing.Routes = nil
		return conf
	}
	// 推导默认 Pattern
	conf.Rotation.Pattern = DeducePattern(conf.Rotation.Every, conf.Rotation.Pattern)
	// 当未配置路由时，默认“所有级别->单文件”
	if len(conf.Routing.Routes) == 0 {
		if conf.Routing.AsyncMode == nil {
			conf.Routing.AsyncMode = &AsyncMode{
				Enable: true,
				Config: &AsyncWriterConfig{BufferSize: 65536, FlushInterval: time.Second},
			}
		}
		conf.Routing.Routes = []LevelRoute{
			{Levels: AllLevelStrs, Name: ""},
		}
	}
	return conf
}

// NewDefaultLogger 创建一个通用日志对象
// filePath 日志输出目录
// conf 日志配置：
//   - 通过 Routing.Routes 将不同级别写入不同文件；
//   - 如果未显式配置 Routing，则默认所有级别写入同一个文件（单文件）。
//
// openStdout 是否开启标准输出(如果Name为空,且openStdout未开启,那么将不会有任何日志信息被记录)
// TODO 如果需要远程日志,增加一个firehook,比如当日志等级为error时,将日志发送到远程服务器
func NewDefaultLogger(conf *LoggerConf) (*Logger, error) {
	conf = fixConf(conf)
	picker, err := newPicker(conf)
	if err != nil {
		return nil, err
	}

	level := strings.ToLower(conf.Level)
	if _, ok := levelMap[level]; !ok {
		level = ErrorLevelStr
	}

	logger := New(
		conf.Stdout,
		WithLevel(levelMap[level]),
		WithCaller(conf.Caller),
		WithColor(conf.Color),
		WithFullCaller(conf.FullCaller),
		WithOuterPicker(picker),
	)

	return logger, nil
}

func Release(logger *Logger) {
	if logger == nil {
		return
	}

	_ = logger.GetOuterPicker().Close()
}
