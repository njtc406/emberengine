package log

import "go.uber.org/zap/zapcore"

// TODO 差一个文件日志,这个日志是用来写入一些统计日志的,所以格式上可能会和其他不太一样,只需要数据,不需要附加信息,可以接入kafka
// TODO 同时可以增加一个文件日志切分后的自动上传,远端收到文件直接分析文件内容

type Fields map[string]interface{}

// Level is backed by zapcore.Level. We also keep a Trace level below Debug.
type Level = zapcore.Level

// These are the different logging levels.
const (
	// PanicLevel level, the highest level of severity. Logs and then calls panic with the
	// message passed to Debug, Info, ...
	PanicLevel Level = zapcore.PanicLevel
	// FatalLevel level. Logs and then calls `Exit(1)`. It will exit even if the
	// logging level is set to Panic.
	FatalLevel Level = zapcore.FatalLevel
	// ErrorLevel level. Logs. Used for errors that should definitely be noted.
	// Commonly used for hooks to send errors to an error tracking service.
	ErrorLevel Level = zapcore.ErrorLevel
	// WarnLevel level. Non-critical entries that deserve eyes.
	WarnLevel Level = zapcore.WarnLevel
	// InfoLevel level. General operational entries about what's going on inside the
	// application.
	InfoLevel Level = zapcore.InfoLevel
	// DebugLevel level. Usually only enabled when debugging. Very verbose logging.
	DebugLevel Level = zapcore.DebugLevel
	// TraceLevel level. Designates finer-grained informational events than the Debug.
	TraceLevel Level = Level(-2)
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

var levelMap = map[string]Level{
	PanicLevelStr: PanicLevel,
	FatalLevelStr: FatalLevel,
	ErrorLevelStr: ErrorLevel,
	WarnLevelStr:  WarnLevel,
	InfoLevelStr:  InfoLevel,
	DebugLevelStr: DebugLevel,
	TraceLevelStr: TraceLevel,
}

var AllLevelStrs = []string{
	PanicLevelStr,
	FatalLevelStr,
	ErrorLevelStr,
	WarnLevelStr,
	InfoLevelStr,
	DebugLevelStr,
	TraceLevelStr,
}
