package log

import (
	"github.com/njtc406/logrus"
	"io"
)

// TODO 差一个文件日志,这个日志是用来写入一些统计日志的,所以格式上可能会和其他不太一样,只需要数据,不需要附加信息,可以接入kafka
// TODO 同时可以增加一个文件日志切分后的自动上传,远端收到文件直接分析文件内容

type Logger = logrus.Logger

type Fields = logrus.Fields

type Level = logrus.Level

type FormatFunction = logrus.LogFunction

type HookFunction = logrus.Hook

// These are the different logging levels. You can set the logging level to log
// on your instance of rusLogger, obtained with `logrus.New()`.
const (
	// PanicLevel level, the highest level of severity. Logs and then calls panic with the
	// message passed to Debug, Info, ...
	PanicLevel Level = logrus.PanicLevel
	// FatalLevel level. Logs and then calls `Exit(1)`. It will exit even if the
	// logging level is set to Panic.
	FatalLevel Level = logrus.FatalLevel
	// ErrorLevel level. Logs. Used for errors that should definitely be noted.
	// Commonly used for hooks to send errors to an error tracking service.
	ErrorLevel Level = logrus.ErrorLevel
	// WarnLevel level. Non-critical entries that deserve eyes.
	WarnLevel Level = logrus.WarnLevel
	// InfoLevel level. General operational entries about what's going on inside the
	// application.
	InfoLevel Level = logrus.InfoLevel
	// DebugLevel level. Usually only enabled when debugging. Very verbose logging.
	DebugLevel Level = logrus.DebugLevel
	// TraceLevel level. Designates finer-grained informational events than the Debug.
	TraceLevel Level = logrus.TraceLevel
)

const (
	PanicLevelStr = "panic"
	FatalLevelStr = "fatal"
	ErrorLevelStr = "error"
	WarnLevelStr  = "warn"
	InfoLevelStr  = "info"
	DebugLevelStr = "debug"
	TraceLevelStr = "trace"
)

var levelMap = map[string]logrus.Level{
	PanicLevelStr: PanicLevel,
	FatalLevelStr: FatalLevel,
	ErrorLevelStr: ErrorLevel,
	WarnLevelStr:  WarnLevel,
	InfoLevelStr:  InfoLevel,
	DebugLevelStr: DebugLevel,
	TraceLevelStr: TraceLevel,
}

type Option func(*Logger)

// WithLevel sets the rusLogger level.
func WithLevel(level Level) Option {
	return func(logger *Logger) {
		logger.SetLevel(level)
	}
}

// WithOut sets the rusLogger output.
func WithOut(output io.Writer) Option {
	return func(logger *Logger) {
		logger.SetOutput(output)
	}
}

// WithColor sets the rusLogger color.
func WithColor(color bool) Option {
	return func(logger *Logger) {
		logger.Formatter.(*Formatter).SetColors(color)
	}
}

// WithCaller sets the rusLogger caller.
func WithCaller(caller bool) Option {
	return func(logger *Logger) {
		logger.SetReportCaller(caller)
		logger.Formatter.(*Formatter).SetCallerDisable(!caller)
	}
}

// WithFullCaller sets print full caller info
func WithFullCaller(full bool) Option {
	return func(logger *Logger) {
		logger.Formatter.(*Formatter).SetFullCaller(full)
	}
}

func WithTimeFormat(formatStr string) Option {
	return func(logger *Logger) {
		logger.Formatter.(*Formatter).SetTimestampFormat(formatStr)
	}
}

func WithHook(hook HookFunction) Option {
	return func(logger *Logger) {
		logger.AddHook(hook)
	}
}
