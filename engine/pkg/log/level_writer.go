package log

import (
	"io"
	"os"
	"path"
)

type LevelRouter struct {
	routers map[Level]io.Writer
	closers []io.WriteCloser
}

// buildLevelWriters 根据 LoggerConf.LevelWriter 构建每个 Level 对应的文件 writer。
//
// 行为说明：
//   - 当 LevelWriter 为空或 Enable=false 或 Name 为空时，不创建任何文件 writer，返回 nil；
//   - 每个 LevelRoute 会创建一个独立的 rotateNew 文件前缀（Name 或 Name+"_route.Name"）；
//   - route.Levels 中的所有级别共用同一个 writer，实现“多级别合并到同一个文件”；
//   - Sync=false 时，优先按全局 AsyncMode 决策是否包一层 AsyncWriter（全局未启用则保持同步）。
func buildLevelWriters(conf *LoggerConf) (*LevelRouter, error) {
	// 路由配置使用统一字段 Routing
	var routes []LevelRoute
	var asyncMode *AsyncMode
	routes = conf.Routing.Routes
	asyncMode = conf.Routing.AsyncMode
	if len(routes) == 0 {
		return nil, nil
	}
	if conf.Name == "" {
		return nil, nil
	}
	router := LevelRouter{
		routers: make(map[Level]io.Writer),
		closers: make([]io.WriteCloser, 0),
	}

	for _, route := range routes {
		if len(route.Levels) == 0 {
			continue
		}

		baseName := conf.Name
		if route.Name != "" {
			baseName = conf.Name + "_" + route.Name
		}

		writers := make([]io.Writer, 0, 2)
		if conf.Stdout {
			writers = append(writers, os.Stdout)
		} else {
			writers = append(writers, io.Discard)
		}

		var writerCloser io.WriteCloser
		if len(baseName) > 0 {
			if len(conf.Dir) == 0 {
				conf.Dir = "./" // 默认当前目录
			}
			// 切割周期校验
			every := conf.Rotation.Every
			if err := ValidateEvery(every); err != nil {
				return nil, err
			}
			// 选择模式
			pattern := DeducePattern(every, conf.Rotation.Pattern)

			w, err := rotateNew(
				path.Join(conf.Dir, baseName),
				WithMaxAge(conf.Rotation.MaxAge),
				WithRotationTime(every),
				WithPattern(pattern),
			)
			if err != nil {
				if w != nil {
					_ = w.Close()
				}
				return nil, err
			} else {
				writers = append(writers, w)
				writerCloser = w
			}
		}

		var wCloser io.WriteCloser
		var writer io.Writer
		if asyncMode != nil && asyncMode.Enable {
			// 开启了异步模式,使用异步writer代替同步writer
			w := NewAsyncWriter(
				io.MultiWriter(writers...),
				asyncMode.Config,
				writerCloser,
			)
			writer = w
			// 记录异步模式的writer,用于close的时候释放
			wCloser = w
		} else {
			writer = io.MultiWriter(writers...)
		}

		for _, lvl := range route.Levels {
			router.routers[lvl] = writer
		}

		if asyncMode != nil && asyncMode.Enable {
			if wCloser != nil {
				router.closers = append(router.closers, wCloser)
			}
		} else {
			if writerCloser != nil {
				router.closers = append(router.closers, writerCloser)
			}
		}
	}

	if len(router.routers) == 0 {
		return nil, nil
	}

	return &router, nil
}

func (l *LevelRouter) Route(level Level) io.Writer {
	return l.routers[level]
}

func (l *LevelRouter) Close() error {
	for _, closer := range l.closers {
		if closer != nil {
			_ = closer.Close()
		}
	}
	return nil
}
