/*
 * Copyright (c) 2024. YR. All rights reserved
 */

// Package log
// 模块名: 日志格式化
// 功能描述: 日志格式化
// 作者:  yr  2024/3/1 0001 11:12
// 最后更新:  yr  2024/3/1 0001 11:12
package log

import (
	"bytes"
	"fmt"
	"github.com/njtc406/logrus"
	"runtime"
	"sort"
	"strings"
	"sync"
)

var (
	colorPre = "\033["
	colorSuf = "\033[0m"
	//colorMap = map[logrus.Level]string{
	//	logrus.PanicLevel: "1;36m",
	//	logrus.FatalLevel: "1;35m",
	//	logrus.ErrorLevel: "1;31m",
	//	logrus.WarnLevel:  "1;33m",
	//	logrus.InfoLevel:  "1;37m",
	//	logrus.DebugLevel: "1;32m",
	//	logrus.TraceLevel: "1;34m",
	//}
)

const (
	ColorRed     = "1;31m"  // 红色
	ColorGreen   = "1;32m"  // 绿色
	ColorYellow  = "1;33m"  // 黄色
	ColorBlue    = "1;34m"  // 蓝色
	ColorMagenta = "1;35m"  // 紫色
	ColorCyan    = "1;36m"  // 天蓝色
	ColorWhite   = "1;37m"  // 白色
	ColorRedBg   = "41;37m" // 红底白字
)

// Formatter - logrus formatter, implements logrus.Formatter
type Formatter struct {
	logrus.Formatter

	Mu *sync.Mutex
	// FieldsOrder - default: fields sorted alphabetically
	FieldsOrder []string

	// TimestampFormat - default: time.StampMilli = "2006-01-02 15:04:05.000"
	TimestampFormat string

	// HideKeys - show [fieldValue] instead of [fieldKey:fieldValue]
	HideKeys bool

	// Colors - enable colors, default is disable
	Colors bool

	// TrimMessages - trim whitespaces on messages
	TrimMessages bool

	// NoCaller - disable print caller info
	NoCaller bool

	// FullCaller - print full caller info
	FullCaller bool

	// CustomCallerFormatter - set custom formatter for caller info
	CustomCallerFormatter func(*runtime.Frame) string

	// bufPool -  The buffer pool used to format the log
	bufPool *defaultPool
}

// Format a log entry (2006-01-02 15:04:05.000 [DEBUG] (test.go:5 func test) aaa=1 bbb=2 this is message)
func (f *Formatter) Format(entry *logrus.Entry) ([]byte, error) {

	// output buffer
	b := f.bufPool.Get()
	defer f.bufPool.Put(b)

	// write time
	timestampFormat := f.TimestampFormat
	if timestampFormat == "" {
		timestampFormat = "2006-01-02 15:04:05.000"
	}
	b.WriteString(entry.Time.Format(timestampFormat))

	// write level
	b.WriteString(" [")
	if f.Colors {
		_, _ = fmt.Fprintf(b, "%s%s", colorPre, getColorByLevel(entry.Level))
	}
	b.WriteString(strings.ToUpper(entry.Level.String()))
	if f.Colors {
		b.WriteString(colorSuf)
	}
	b.WriteString("] ")

	// write caller
	if !f.NoCaller {
		if f.FullCaller {
			f.writeCaller(b, entry)
		} else {
			f.writeSimpleCaller(b, entry)
		}

	}

	// write fields
	if f.FieldsOrder == nil {
		f.writeFields(b, entry)
	} else {
		f.writeOrderedFields(b, entry)
	}

	b.WriteString(" ")

	// write message
	if f.TrimMessages {
		b.WriteString(strings.TrimSpace(entry.Message))
	} else {
		b.WriteString(entry.Message)
	}

	b.WriteByte('\n')

	return b.Bytes(), nil
}

// SetColors 是否启用颜色(默认不启动)
func (f *Formatter) SetColors(colors bool) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.Colors = colors
}

// SetTimestampFormat 日期格式化样式(默认 2006-01-02 15:04:05.000)
func (f *Formatter) SetTimestampFormat(timestampFormat string) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.TimestampFormat = timestampFormat
}

// SetCallerDisable 关闭调用者信息打印(默认开启)
func (f *Formatter) SetCallerDisable(status bool) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.NoCaller = status
}

// SetFullCaller 开启详细调用者信息打印(默认关闭)
func (f *Formatter) SetFullCaller(status bool) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.FullCaller = status
}

func (f *Formatter) writeCaller(b *bytes.Buffer, entry *logrus.Entry) {
	// TODO 后面在看需不需要根据日志等级来控制某些等级的日志不需要caller信息
	if entry.HasCaller() {
		if f.CustomCallerFormatter != nil {
			_, _ = fmt.Fprintf(b, f.CustomCallerFormatter(entry.Caller))
		} else {
			_, _ = fmt.Fprintf(
				b,
				"(file: %s:%d function: %s) >> ",
				entry.Caller.File,
				entry.Caller.Line,
				entry.Caller.Function,
			)
		}
	}
}

func (f *Formatter) writeSimpleCaller(b *bytes.Buffer, entry *logrus.Entry) {
	// TODO 后面在看需不需要根据日志等级来控制某些等级的日志不需要caller信息
	if entry.HasCaller() {
		if f.CustomCallerFormatter != nil {
			_, _ = fmt.Fprintf(b, f.CustomCallerFormatter(entry.Caller))
		} else {
			_, _ = fmt.Fprintf(
				b,
				"%s:%d >> ",
				entry.Caller.File,
				entry.Caller.Line,
			)
		}
	}
}

func (f *Formatter) writeFields(b *bytes.Buffer, entry *logrus.Entry) {
	if len(entry.Data) != 0 {
		fields := make([]string, 0, len(entry.Data))
		for field := range entry.Data {
			fields = append(fields, field)
		}

		sort.Strings(fields)

		for _, field := range fields {
			f.writeField(b, entry, field)
		}
	}
}

func (f *Formatter) writeOrderedFields(b *bytes.Buffer, entry *logrus.Entry) {
	length := len(entry.Data)
	foundFieldsMap := map[string]bool{}
	for _, field := range f.FieldsOrder {
		if _, ok := entry.Data[field]; ok {
			foundFieldsMap[field] = true
			length--
			f.writeField(b, entry, field)
		}
	}

	if length > 0 {
		notFoundFields := make([]string, 0, length)
		for field := range entry.Data {
			if foundFieldsMap[field] == false {
				notFoundFields = append(notFoundFields, field)
			}
		}

		sort.Strings(notFoundFields)

		for _, field := range notFoundFields {
			f.writeField(b, entry, field)
		}
	}
}

func (f *Formatter) writeField(b *bytes.Buffer, entry *logrus.Entry, field string) {
	if f.HideKeys {
		_, _ = fmt.Fprintf(b, "[%v]", entry.Data[field])
	} else {
		_, _ = fmt.Fprintf(b, "[%s=%v]", field, entry.Data[field])
	}

	b.WriteString(" ")
}

func getColorByLevel(level logrus.Level) string {
	switch level {
	case logrus.TraceLevel:
		return ColorWhite
	case logrus.DebugLevel:
		return ColorGreen
	case logrus.WarnLevel:
		return ColorYellow
	case logrus.ErrorLevel:
		return ColorRed
	case logrus.FatalLevel:
		return ColorMagenta
	case logrus.PanicLevel:
		return ColorRedBg
	default:
		return ColorBlue
	}
}
