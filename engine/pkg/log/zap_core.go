package log

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-colorable"
	"github.com/njtc406/emberengine/engine/pkg/utils/timelib"
	"go.uber.org/zap/zapcore"
)

// stdoutWriteSyncerFactory allows tests to capture stdout logging without touching os.Stdout.
// It must be concurrency-safe for reads/writes; tests should restore it via t.Cleanup.
var stdoutWriteSyncerFactory = func(color bool) zapcore.WriteSyncer {
	ws := zapcore.AddSync(os.Stdout)
	// Better Windows compatibility for ANSI colors.
	if color {
		ws = zapcore.AddSync(colorable.NewColorableStdout())
	}
	return ws
}

type closeFn func() error

type colorMode uint8

const (
	colorNone colorMode = iota
	colorLegacy
)

// buildZapTeeCore builds a zap-native tee core:
// - one (optional) stdout core for all levels
// - N file cores according to Routing.Routes, with "last route wins" semantics per level
// - optional async buffering via zapcore.BufferedWriteSyncer
func buildZapTeeCore(conf *LoggerConf, minLevel Level, addCaller, fullCaller, color bool, isDebug bool) (zapcore.Core, []closeFn, error) {
	if conf == nil {
		conf = &LoggerConf{}
	}
	conf = fixConf(conf)

	cores := make([]zapcore.Core, 0, 1+len(conf.Routing.Routes))
	closers := make([]closeFn, 0, len(conf.Routing.Routes))

	// Stdout core (all levels)
	if conf.Stdout {
		// Production mode never uses ANSI colors.
		useColor := color && !conf.Production
		cm := colorNone
		if useColor {
			cm = colorLegacy
		}
		stdoutWS := stdoutWriteSyncerFactory(useColor)
		cores = append(cores, newOutputCore(
			conf,
			stdoutWS,
			minLevel,
			levelEnablerAll(minLevel),
			addCaller,
			fullCaller,
			cm,
			isDebug,
		))
	}

	// No file routes configured
	if conf.PrefixName == "" || conf.Routing == nil || len(conf.Routing.Routes) == 0 {
		if len(cores) == 0 {
			cores = append(cores, newOutputCore(conf, zapcore.AddSync(io.Discard), minLevel, levelEnablerAll(minLevel), addCaller, fullCaller,
				colorNone, isDebug))
		}
		return zapcore.NewTee(cores...), closers, nil
	}

	// Level -> route index (last wins)
	levelToRoute := make(map[zapcore.Level]int, len(AllLevelStrs))
	for ridx, r := range conf.Routing.Routes {
		for _, lvlStr := range r.Levels {
			lvl, ok := levelMap[strings.ToLower(lvlStr)]
			if !ok {
				continue
			}
			levelToRoute[zapcore.Level(lvl)] = ridx
		}
	}

	// route index -> levels
	routeLevels := make(map[int][]zapcore.Level)
	for lvl, ridx := range levelToRoute {
		routeLevels[ridx] = append(routeLevels[ridx], lvl)
	}

	for ridx, lvls := range routeLevels {
		route := conf.Routing.Routes[ridx]
		ws, closeW, err := openRouteWriteSyncer(conf, &route)
		if err != nil {
			if closeW != nil {
				_ = closeW()
			}
			return nil, nil, err
		}
		if closeW != nil {
			closers = append(closers, closeW)
		}

		enabler := levelEnablerSet(minLevel, lvls)
		// Never write ANSI colors into files.
		cores = append(cores, newOutputCore(conf, ws, minLevel, enabler, addCaller, fullCaller, colorNone, isDebug))
	}

	if len(cores) == 0 {
		cores = append(cores, newOutputCore(conf, zapcore.AddSync(io.Discard), minLevel, levelEnablerAll(minLevel), addCaller, fullCaller, colorNone, isDebug))
	}

	return zapcore.NewTee(cores...), closers, nil
}

func newOutputCore(conf *LoggerConf, ws zapcore.WriteSyncer, min zapcore.Level, enabler levelEnabler, addCaller, fullCaller bool, colorMode colorMode, isDebug bool) zapcore.Core {
	if conf != nil && conf.Production {
		enc := newJSONEncoder(addCaller, fullCaller)
		return zapcore.NewCore(enc, ws, zapLevelEnablerAdapter{f: func(l zapcore.Level) bool {
			if enabler == nil {
				return l >= min
			}
			return enabler.Enabled(l)
		}})
	}
	return newFormatCore(ws, min, enabler, addCaller, fullCaller, colorMode, isDebug)
}

type zapLevelEnablerAdapter struct {
	f func(zapcore.Level) bool
}

func (a zapLevelEnablerAdapter) Enabled(l zapcore.Level) bool {
	if a.f == nil {
		return true
	}
	return a.f(l)
}

func newJSONEncoder(addCaller, fullCaller bool) zapcore.Encoder {
	cfg := zapcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stack",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeName:     zapcore.FullNameEncoder,
		EncodeTime: func(_ time.Time, enc zapcore.PrimitiveArrayEncoder) {
			// Keep legacy time semantics (timelib offset)
			enc.AppendString(timelib.Now().Format(defaultTimeFormat))
		},
		EncodeCaller: func(caller zapcore.EntryCaller, enc zapcore.PrimitiveArrayEncoder) {
			if !caller.Defined {
				return
			}
			file := filepath.ToSlash(caller.File)
			if !fullCaller {
				rel := getRelativePath(file)
				if rel != "" {
					file = rel
				}
			}
			enc.AppendString(fmt.Sprintf("%s:%d", file, caller.Line))
		},
	}
	if !addCaller {
		cfg.CallerKey = ""
	}
	return zapcore.NewJSONEncoder(cfg)
}

func openRouteWriteSyncer(conf *LoggerConf, route *LevelRoute) (zapcore.WriteSyncer, closeFn, error) {
	baseName := conf.PrefixName
	if route != nil && route.Name != "" {
		baseName = conf.PrefixName + "_" + route.Name
	}
	if baseName == "" {
		return zapcore.AddSync(io.Discard), nil, nil
	}

	if conf.Dir == "" {
		conf.Dir = "./"
	}

	every := conf.Rotation.Every
	if err := ValidateEvery(every); err != nil {
		return nil, nil, err
	}
	pattern := DeducePattern(every, conf.Rotation.Pattern)

	rw, err := rotateNew(
		path.Join(conf.Dir, baseName),
		WithMaxAge(conf.Rotation.MaxAge),
		WithRotationTime(every),
		WithPattern(pattern),
	)
	if err != nil {
		if rw != nil {
			_ = rw.Close()
		}
		return nil, nil, err
	}

	ws := zapcore.AddSync(rw)
	closeBase := func() error { return rw.Close() }

	// Async buffering (zap-native)
	if conf.Routing != nil && conf.Routing.AsyncMode != nil && conf.Routing.AsyncMode.Enable {
		cfg := fixAsyncWriterConf(conf.Routing.AsyncMode.Config)
		bws := &zapcore.BufferedWriteSyncer{
			WS:            ws,
			Size:          cfg.BufferSize,
			FlushInterval: cfg.FlushInterval,
		}
		closeBuffered := func() error {
			err := bws.Stop()
			if err != nil {
				return err
			}
			_ = bws.Sync()
			return nil
		}
		ws = bws
		return ws, func() error {
			_ = closeBuffered()
			return closeBase()
		}, nil
	}

	return ws, closeBase, nil
}

func fixAsyncWriterConf(config *AsyncWriterConfig) *AsyncWriterConfig {
	// Set default values
	if config.BufferSize == 0 {
		config.BufferSize = 4096
	}
	if config.FlushInterval == 0 {
		config.FlushInterval = time.Second
	}
	return config
}

type levelEnabler interface {
	Enabled(zapcore.Level) bool
}

type levelEnablerFunc func(zapcore.Level) bool

func (f levelEnablerFunc) Enabled(l zapcore.Level) bool { return f(l) }

func levelEnablerAll(min zapcore.Level) levelEnablerFunc {
	return func(l zapcore.Level) bool { return l >= min }
}

func levelEnablerSet(min zapcore.Level, allowed []zapcore.Level) levelEnablerFunc {
	set := make(map[zapcore.Level]struct{}, len(allowed))
	for _, l := range allowed {
		set[l] = struct{}{}
	}
	return func(l zapcore.Level) bool {
		if l < min {
			return false
		}
		_, ok := set[l]
		return ok
	}
}

// formatCore is a small zapcore.Core implementation that only focuses on formatting.
// Routing/level filtering is done via the provided enabler.
type formatCore struct {
	ws         zapcore.WriteSyncer
	minLevel   zapcore.Level
	enabler    levelEnabler
	addCaller  bool
	fullCaller bool
	colorMode  colorMode
	baseFields []zapcore.Field
	isDebug    bool
}

func newFormatCore(ws zapcore.WriteSyncer, min zapcore.Level, enabler levelEnabler, addCaller, fullCaller bool, colorMode colorMode, isDebug bool) zapcore.Core {
	if ws == nil {
		ws = zapcore.AddSync(io.Discard)
	}
	if enabler == nil {
		enabler = levelEnablerAll(min)
	}
	return &formatCore{
		ws:         ws,
		minLevel:   min,
		enabler:    enabler,
		addCaller:  addCaller,
		fullCaller: fullCaller,
		colorMode:  colorMode,
		isDebug:    isDebug,
	}
}

func (c *formatCore) Enabled(lvl zapcore.Level) bool {
	if lvl < c.minLevel {
		return false
	}
	return c.enabler.Enabled(lvl)
}

func (c *formatCore) With(fields []zapcore.Field) zapcore.Core {
	nc := &formatCore{
		ws:         c.ws,
		minLevel:   c.minLevel,
		enabler:    c.enabler,
		addCaller:  c.addCaller,
		fullCaller: c.fullCaller,
		colorMode:  c.colorMode,
		baseFields: append(append([]zapcore.Field{}, c.baseFields...), fields...),
	}
	return nc
}

func (c *formatCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}
	return ce
}

func (c *formatCore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	// Legacy-like output: time [TAG/LEVEL] caller [field] [field] >> msg\n
	moe := zapcore.NewMapObjectEncoder()
	for _, f := range c.baseFields {
		f.AddTo(moe)
	}
	for _, f := range fields {
		f.AddTo(moe)
	}

	var tagOverride string
	if v, ok := moe.Fields["tag"]; ok {
		if s, ok2 := v.(string); ok2 && s != "" {
			tagOverride = strings.ToUpper(s)
		}
		delete(moe.Fields, "tag")
	}

	keys := make([]string, 0, len(moe.Fields))
	for k := range moe.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	b := getBufferPool(c.isDebug).Get()
	b.Grow(256)
	defer func() {
		// Avoid retaining very large buffers in the pool.
		if b.Cap() <= 64*1024 {
			getBufferPool(c.isDebug).Put(b)
		}
	}()

	// keep old time source semantics (timelib offset)
	b.WriteString(timelib.Now().Format(defaultTimeFormat))

	// level/tag
	b.WriteString(" [")
	levelLabel := levelToLabel(ent.Level)
	if tagOverride != "" {
		levelLabel = tagOverride
	}
	b.WriteString(colorizeLabel(ent.Level, levelLabel, c.colorMode))
	b.WriteString("] ")

	// caller
	if c.addCaller && ent.Caller.Defined {
		if c.fullCaller {
			b.WriteString(filepath.ToSlash(ent.Caller.File))
		} else {
			rel := getRelativePath(ent.Caller.File)
			if rel == "" {
				rel = filepath.ToSlash(ent.Caller.File)
			}
			b.WriteString(rel)
		}
		b.WriteByte(':')
		var ibuf [32]byte
		b.Write(strconv.AppendInt(ibuf[:0], int64(ent.Caller.Line), 10))
		b.WriteByte(' ')
	}

	// fields in legacy style: [k=v] [k=v] ...
	for _, k := range keys {
		b.WriteByte('[')
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(fmt.Sprint(moe.Fields[k]))
		b.WriteByte(']')
		b.WriteByte(' ')
	}

	b.WriteString(">> ")
	b.WriteString(ent.Message)
	b.WriteByte('\n')

	_, err := c.ws.Write(b.Bytes())
	return err
}

func (c *formatCore) Sync() error {
	if c.ws == nil {
		return nil
	}
	return c.ws.Sync()
}

func levelToLabel(level zapcore.Level) string {
	if level == zapcore.Level(TraceLevel) {
		return "TRACE"
	}
	return strings.ToUpper(level.String())
}

// keep old time format constant
const defaultTimeFormat = "2006-01-02 15:04:05.000Z0700"

var (
	moduleNameOnce sync.Once
	moduleName     string
)

const (
	ansiPre = "\033["
	ansiSuf = "\033[0m"
)

const (
	// https://en.wikipedia.org/wiki/ANSI_escape_code#Colors
	ansiGray       = "1;90m"  // bright black / gray
	ansiGreen      = "1;32m"  // green
	ansiBlue       = "1;34m"  // blue
	ansiYellow     = "1;33m"  // yellow
	ansiRed        = "1;31m"  // bold red
	ansiHiMagenta  = "1;95m"  // bright magenta
	ansiRedBgWhite = "41;97m" // red background + bright white foreground
)

func colorizeLabel(level zapcore.Level, label string, mode colorMode) string {
	if mode == colorNone {
		return label
	}
	return ansiPre + getLegacyColorByLevel(level) + label + ansiSuf
}

func getLegacyColorByLevel(level zapcore.Level) string {
	switch level {
	case TraceLevel:
		return ansiGray
	case zapcore.DebugLevel:
		return ansiGreen
	case zapcore.WarnLevel:
		return ansiYellow
	case zapcore.ErrorLevel:
		return ansiRed
	case zapcore.DPanicLevel, zapcore.PanicLevel:
		return ansiHiMagenta
	case zapcore.FatalLevel:
		return ansiRedBgWhite
	default:
		// INFO and everything else
		return ansiBlue
	}
}

// getRelativePath extracts a project-root-relative path (like the legacy formatter).
func getRelativePath(fullPath string) string {
	fullPath = filepath.ToSlash(fullPath)

	moduleNameOnce.Do(func() {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Path != "" {
			parts := strings.Split(info.Main.Path, "/")
			if len(parts) > 0 {
				moduleName = parts[len(parts)-1]
			}
		}
	})

	if moduleName != "" {
		parts := strings.Split(fullPath, "/")
		for i := 0; i < len(parts); i++ {
			part := parts[i]
			if strings.HasPrefix(part, moduleName) {
				if i+1 < len(parts) {
					return strings.Join(parts[i+1:], "/")
				}
				return ""
			}
		}
	}

	if idx := strings.Index(fullPath, "/src/"); idx != -1 {
		pathAfterSrc := fullPath[idx+5:]
		if firstSlash := strings.Index(pathAfterSrc, "/"); firstSlash != -1 {
			return pathAfterSrc[firstSlash+1:]
		}
		return pathAfterSrc
	}

	commonPrefixes := []string{"/home/", "C:/Users/", "D:/", "E:/", "F:/"}
	for _, prefix := range commonPrefixes {
		if strings.HasPrefix(fullPath, prefix) {
			parts := strings.Split(fullPath[len(prefix):], "/")
			if len(parts) > 2 {
				return strings.Join(parts[1:], "/")
			}
		}
	}

	parts := strings.Split(fullPath, "/")
	if len(parts) >= 3 {
		return strings.Join(parts[len(parts)-3:], "/")
	}

	return filepath.Base(fullPath)
}
