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
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"

	"github.com/njtc406/emberengine/engine/pkg/utils/emberctx"
	"github.com/njtc406/logrus"
)

var (
	colorPre = "\033["
	colorSuf = "\033[0m"

	// 缓存模块名（项目名），用于路径提取优化
	moduleNameOnce sync.Once
	moduleName     string
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

	Mu sync.Mutex
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
}

// Format 格式化日志条目
// 输出格式：日期 [级别] 文件名:行号 [fields] [context] >> 消息
func (f *Formatter) Format(entry *logrus.Entry) ([]byte, error) {
	b := entry.Buffer

	// 1. 写入时间戳
	f.writeTimestamp(b, entry)

	// 2. 写入日志级别
	f.writeLevel(b, entry)

	// 3. 写入调用者信息（文件名:行号）
	f.writeCallerInfo(b, entry)

	// 4. 写入额外字段
	f.writeFieldsInfo(b, entry)

	// 5. 写入上下文头（如 traceId）
	f.writeContextHeaders(b, entry)

	// 6. 分隔符
	b.WriteString(" >> ")

	// 7. 写入日志消息
	f.writeMessage(b, entry)

	// 8. 换行
	b.WriteByte('\n')

	return b.Bytes(), nil
}

// writeTimestamp 写入时间戳
func (f *Formatter) writeTimestamp(b *bytes.Buffer, entry *logrus.Entry) {
	timestampFormat := f.TimestampFormat
	if timestampFormat == "" {
		timestampFormat = "2006-01-02 15:04:05.000"
	}
	b.WriteString(entry.Time.Format(timestampFormat))
}

// writeLevel 写入日志级别
func (f *Formatter) writeLevel(b *bytes.Buffer, entry *logrus.Entry) {
	b.WriteString(" [")
	if f.Colors {
		_, _ = fmt.Fprintf(b, "%s%s", colorPre, getColorByLevel(entry.Level))
	}
	b.WriteString(strings.ToUpper(entry.Level.String()))
	if f.Colors {
		b.WriteString(colorSuf)
	}
	b.WriteString("] ")
}

// writeCallerInfo 写入调用者信息
func (f *Formatter) writeCallerInfo(b *bytes.Buffer, entry *logrus.Entry) {
	if f.NoCaller {
		return
	}

	if f.FullCaller {
		f.writeCaller(b, entry)
	} else {
		f.writeSimpleCaller(b, entry)
	}
	b.WriteString(" ")
}

// writeFieldsInfo 写入额外字段
func (f *Formatter) writeFieldsInfo(b *bytes.Buffer, entry *logrus.Entry) {
	if f.FieldsOrder == nil {
		f.writeFields(b, entry)
	} else {
		f.writeOrderedFields(b, entry)
	}
}

// writeContextHeaders 写入上下文头信息（如 traceId）
func (f *Formatter) writeContextHeaders(b *bytes.Buffer, entry *logrus.Entry) {
	if entry.Context == nil {
		return
	}

	header := emberctx.ToHeaders(entry.Context)
	if len(header) == 0 {
		return
	}

	b.WriteString("[")
	// 对 keys 排序以保证输出顺序一致
	keys := make([]string, 0, len(header))
	for k := range header {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// 按排序后的顺序输出
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%s", k, header[k]))
	}
	b.WriteString(strings.Join(pairs, ", "))
	b.WriteString("]")
}

// writeMessage 写入日志消息
func (f *Formatter) writeMessage(b *bytes.Buffer, entry *logrus.Entry) {
	if f.TrimMessages {
		b.WriteString(strings.TrimSpace(entry.Message))
	} else {
		b.WriteString(entry.Message)
	}
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
	if entry.HasCaller() {
		if f.CustomCallerFormatter != nil {
			_, _ = fmt.Fprint(b, f.CustomCallerFormatter(entry.Caller))
		} else {
			_, _ = fmt.Fprintf(
				b,
				"%s:%d",
				entry.Caller.File,
				entry.Caller.Line,
			)
		}
	}
}

func (f *Formatter) writeSimpleCaller(b *bytes.Buffer, entry *logrus.Entry) {
	if entry.HasCaller() {
		if f.CustomCallerFormatter != nil {
			_, _ = fmt.Fprint(b, f.CustomCallerFormatter(entry.Caller))
		} else {
			// 提取相对路径：从项目根目录开始
			filePath := getRelativePath(entry.Caller.File)
			_, _ = fmt.Fprintf(
				b,
				"%s:%d",
				filePath,
				entry.Caller.Line,
			)
		}
	}
}

// getRelativePath 提取相对路径，智能识别项目根目录
// 优先级：
// 1. 使用 go module 的项目名在路径中定位项目根目录（最精确）
// 2. 从路径中移除 GOPATH/src/项目名 前缀
// 3. 移除盘符和常见前缀后取合理路径
// 4. 兜底返回最后三级目录+文件名
func getRelativePath(fullPath string) string {
	// 将路径统一为正斜杠
	fullPath = filepath.ToSlash(fullPath)

	// 策略1：使用 go module 的项目名定位
	// 首次调用时初始化模块名缓存
	moduleNameOnce.Do(func() {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Path != "" {
			// 从 module path 中提取最后一段作为项目名
			// 例如：github.com/njtc406/emberengine -> emberengine
			parts := strings.Split(info.Main.Path, "/")
			if len(parts) > 0 {
				moduleName = parts[len(parts)-1]
			}
		}
	})

	// 如果获取到了模块名，在路径中查找它
	if moduleName != "" {
		// 查找项目名在路径中的位置
		// 例如：F:/go/src/emberengine/example/test.go
		// 项目名：emberengine
		idx := strings.Index(fullPath, "/"+moduleName+"/")
		if idx != -1 {
			// 返回项目名之后的路径
			return fullPath[idx+len(moduleName)+2:]
		}
		// 处理路径末尾是项目名的情况（不太可能，但做个兜底）
		if strings.HasSuffix(fullPath, "/"+moduleName) {
			return ""
		}
	}

	// 策略2：移除 GOPATH/src/ 前缀
	// 例如：F:/go/src/emberengine/example/test.go -> example/test.go
	if idx := strings.Index(fullPath, "/src/"); idx != -1 {
		pathAfterSrc := fullPath[idx+5:] // 跳过 "/src/"
		// 从 src 后的路径中，取第一个斜杠之后的部分（去掉项目名）
		if firstSlash := strings.Index(pathAfterSrc, "/"); firstSlash != -1 {
			return pathAfterSrc[firstSlash+1:]
		}
		return pathAfterSrc
	}

	// 策略3：移除常见的用户目录前缀
	commonPrefixes := []string{"/home/", "C:/Users/", "D:/", "E:/", "F:/"}
	for _, prefix := range commonPrefixes {
		if strings.HasPrefix(fullPath, prefix) {
			// 找到用户名后的路径
			parts := strings.Split(fullPath[len(prefix):], "/")
			if len(parts) > 2 {
				// 返回从第二级开始的路径（通常是项目名/子路径/文件）
				return strings.Join(parts[1:], "/")
			}
		}
	}

	// 兜底：返回最后三级目录 + 文件名
	parts := strings.Split(fullPath, "/")
	if len(parts) >= 3 {
		return strings.Join(parts[len(parts)-3:], "/")
	}

	// 最终兜底：只返回文件名
	return filepath.Base(fullPath)
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
			if !foundFieldsMap[field] {
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
