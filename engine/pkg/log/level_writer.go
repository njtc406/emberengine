package log

import (
	"io"
	"path"
)

// levelFileHook 按日志级别将日志写入不同的 io.Writer。
//
// 典型用法：
//   - 通过 LevelWriterConf 描述哪些 Level 共享同一个 writer；
//   - 每个路由创建一个独立的 rotateNew（可选异步包装）作为最终输出目标。
//
// 注意：
//   - 该 Hook 只负责“额外写入”，不会影响 logger.Out 的输出；
//   - 复用现有 Formatter，保证与主输出格式一致。
type levelFileHook struct {
	formatter *Formatter
	writers   map[Level]io.Writer
}

func newLevelFileHook(formatter *Formatter, writers map[Level]io.Writer) *levelFileHook {
	return &levelFileHook{
		formatter: formatter,
		writers:   writers,
	}
}

func (h *levelFileHook) Levels() []Level {
	return []Level{PanicLevel, FatalLevel, ErrorLevel, WarnLevel, InfoLevel, DebugLevel, TraceLevel}
}

func (h *levelFileHook) Fire(entry *Entry) error {
	w, ok := h.writers[entry.Level]
	if !ok || w == nil {
		return nil
	}
	line, err := h.formatter.Format(entry)
	if err != nil {
		return err
	}
	_, err = w.Write(line)
	return err
}

// buildLevelWriters 根据 LoggerConf.LevelWriter 构建每个 Level 对应的文件 writer。
//
// 行为说明：
//   - 当 LevelWriter 为空或 Enable=false 或 Name 为空时，不创建任何文件 writer，返回 nil；
//   - 每个 LevelRoute 会创建一个独立的 rotateNew 文件前缀（Name 或 Name+"_route.Name"）；
//   - route.Levels 中的所有级别共用同一个 writer，实现“多级别合并到同一个文件”；
//   - Sync=false 时，优先按全局 AsyncMode 决策是否包一层 AsyncWriter（全局未启用则保持同步）。
func buildLevelWriters(filePath string, conf *LoggerConf) (map[Level]io.Writer, error) {
	lw := conf.LevelWriter
	if lw == nil || !lw.Enable || conf.Name == "" {
		return nil, nil
	}

	levelWriters := make(map[Level]io.Writer)

	for _, route := range lw.Routes {
		if len(route.Levels) == 0 {
			continue
		}

		baseName := conf.Name
		if route.Name != "" {
			baseName = conf.Name + "_" + route.Name
		}

		// 为该路由创建一个独立的 rotate writer
		w, err := rotateNew(
			path.Join(filePath, baseName),
			WithMaxAge(conf.MaxAge),
			WithRotationTime(conf.RotationTime),
		)
		if err != nil {
			if w != nil {
				_ = w.Close()
			}
			return nil, err
		}

		// 记录该 writer，方便 Release(logger) 统一关闭文件句柄
		// 注意：具体 logger 实例稍后在 NewDefaultLogger 中注册，这里只返回 writer。

		var writer io.Writer = w

		// route.Sync=false 时允许异步；若全局 AsyncMode 未开启，则保持同步
		if !route.Sync && conf.AsyncMode != nil && conf.AsyncMode.Enable {
			writer = NewAsyncWriter(writer, conf.AsyncMode.Config, w)
		}

		for _, lvl := range route.Levels {
			levelWriters[lvl] = writer
		}
	}

	if len(levelWriters) == 0 {
		return nil, nil
	}

	return levelWriters, nil
}
