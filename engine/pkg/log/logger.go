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
	"context"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type AsyncMode struct {
	Enable bool
	Config *AsyncWriterConfig
}

type AsyncWriterConfig struct {
	// 异步写入缓冲区大小
	BufferSize    int           `binding:""`
	FlushInterval time.Duration `binding:""`
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
	// 生产模式：开启时日志以 JSON 格式输出（适合采集/检索）；同时会禁用 Color。
	Production bool `binding:""`
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
	Rotation *RotationConf `binding:""`
	Routing  *RoutingConf  `binding:""`
}

func fixConf(conf *LoggerConf) *LoggerConf {
	if conf == nil {
		conf = &LoggerConf{}
	}
	if conf.Rotation == nil {
		conf.Rotation = &RotationConf{}
	}
	if conf.Routing == nil {
		conf.Routing = &RoutingConf{}
	}
	// 生产模式强制结构化输出，不使用颜色
	if conf.Production {
		conf.Color = false
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

// Logger is a zap-backed logger that keeps the existing leveled API surface.
// It also implements ILoggerX, so it can be passed through the engine consistently.
type Logger struct {
	shared *loggerShared
	z      *zap.Logger
}

type loggerShared struct {
	closeOnce sync.Once
	closers   []closeFn
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

	levelStr := strings.ToLower(conf.Level)
	minLevel, ok := levelMap[levelStr]
	if !ok {
		minLevel = ErrorLevel
	}

	core, closers, err := buildZapTeeCore(conf, minLevel, conf.Caller, conf.FullCaller, conf.Color, conf.Production)
	if err != nil {
		for _, c := range closers {
			_ = c()
		}
		return nil, err
	}

	opts := []zap.Option{
		zap.AddCallerSkip(2),
		zap.ErrorOutput(zapcore.AddSync(os.Stderr)),
	}
	if conf.Caller {
		opts = append(opts, zap.AddCaller())
	}

	zl := zap.New(core, opts...)
	return &Logger{
		shared: &loggerShared{closers: closers},
		z:      zl,
	}, nil
}

func Release(logger *Logger) {
	if logger == nil {
		return
	}
	_ = logger.Close()
}

func (l *Logger) Close() error {
	if l == nil || l.shared == nil {
		return nil
	}
	l.shared.closeOnce.Do(func() {
		_ = l.z.Sync()
		for _, c := range l.shared.closers {
			_ = c()
		}
		l.shared.closers = nil
	})
	return nil
}

// GetOutput returns an io.Writer for libraries expecting a standard logger output.
// It logs at InfoLevel through this logger.
func (l *Logger) GetOutput() io.Writer {
	return &loggerOutputWriter{l: l}
}

type loggerOutputWriter struct {
	l *Logger
}

func (w *loggerOutputWriter) Write(p []byte) (n int, err error) {
	if w == nil || w.l == nil {
		return len(p), nil
	}
	msg := strings.TrimRight(string(p), "\r\n")
	if msg == "" {
		return len(p), nil
	}
	// Skip this writer frame so caller points to the library using the writer.
	zl := w.l.z.WithOptions(zap.AddCallerSkip(1))
	ce := zl.Check(zapcore.Level(InfoLevel), msg)
	if ce != nil {
		ce.Write()
	}
	return len(p), nil
}

func (l *Logger) WithContext(_ context.Context) ILoggerX {
	return l
}

func (l *Logger) WithField(key string, value interface{}) ILoggerX {
	if l == nil {
		return nil
	}
	return &Logger{shared: l.shared, z: l.z.With(zap.Any(key, value))}
}

func (l *Logger) WithFields(fields map[string]interface{}) ILoggerX {
	if l == nil {
		return nil
	}
	if len(fields) == 0 {
		return l
	}
	zfs := make([]zap.Field, 0, len(fields))
	for k, v := range fields {
		zfs = append(zfs, zap.Any(k, v))
	}
	return &Logger{shared: l.shared, z: l.z.With(zfs...)}
}

func (l *Logger) Slow() ILoggerX   { return l.WithField("tag", "SLOW") }
func (l *Logger) State() ILoggerX  { return l.WithField("tag", "STATE") }
func (l *Logger) Metric() ILoggerX { return l.WithField("tag", "METRIC") }

func (l *Logger) logAt(level Level, msg string) {
	if l == nil {
		return
	}
	msgToLog := msg
	if level == PanicLevel || level == FatalLevel {
		msgToLog = msg + "\n" + string(debug.Stack())
	}
	ce := l.z.Check(level, msgToLog)
	if ce == nil {
		return
	}
	ce.Write()

	// In this project, Fatal/Panic are treated as control-flow, not just levels.
	switch level {
	case PanicLevel:
		panic(msg)
	case FatalLevel:
		_ = l.Close()
		os.Exit(1)
	}
}

func (l *Logger) Trace(args ...interface{}) { l.logAt(TraceLevel, fmt.Sprint(args...)) }
func (l *Logger) Tracef(format string, args ...interface{}) {
	l.logAt(TraceLevel, fmt.Sprintf(format, args...))
}

func (l *Logger) Debug(args ...interface{}) { l.logAt(DebugLevel, fmt.Sprint(args...)) }
func (l *Logger) Debugf(format string, args ...interface{}) {
	l.logAt(DebugLevel, fmt.Sprintf(format, args...))
}

func (l *Logger) Info(args ...interface{}) { l.logAt(InfoLevel, fmt.Sprint(args...)) }
func (l *Logger) Infof(format string, args ...interface{}) {
	l.logAt(InfoLevel, fmt.Sprintf(format, args...))
}

func (l *Logger) Warn(args ...interface{}) { l.logAt(WarnLevel, fmt.Sprint(args...)) }
func (l *Logger) Warnf(format string, args ...interface{}) {
	l.logAt(WarnLevel, fmt.Sprintf(format, args...))
}
func (l *Logger) Warning(args ...interface{}) { l.Warn(args...) }
func (l *Logger) Warningf(format string, args ...interface{}) {
	l.Warnf(format, args...)
}

func (l *Logger) Error(args ...interface{}) { l.logAt(ErrorLevel, fmt.Sprint(args...)) }
func (l *Logger) Errorf(format string, args ...interface{}) {
	l.logAt(ErrorLevel, fmt.Sprintf(format, args...))
}

func (l *Logger) Panic(args ...interface{}) {
	l.logAt(PanicLevel, fmt.Sprint(args...))
}
func (l *Logger) Panicf(format string, args ...interface{}) {
	l.logAt(PanicLevel, fmt.Sprintf(format, args...))
}

func (l *Logger) Fatal(args ...interface{}) {
	l.logAt(FatalLevel, fmt.Sprint(args...))
}
func (l *Logger) Fatalf(format string, args ...interface{}) {
	l.logAt(FatalLevel, fmt.Sprintf(format, args...))
}
