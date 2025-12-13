package log

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/utils/timelib"
)

func TestInfo(t *testing.T) {
	logger, err := NewDefaultLogger(&LoggerConf{
		Dir:        "./logs",
		PrefixName: "app",
		Level:      "info",
		Stdout:     true,
		Caller:     true,
		FullCaller: true,
		Color:      false,
		Rotation: &RotationConf{
			MaxAge: 15 * 24 * time.Hour,
			Every:  24 * time.Hour,
		},
		Routing: &RoutingConf{
			AsyncMode: &AsyncMode{
				Enable: true,
				Config: &AsyncWriterConfig{
					BufferSize:    1024,
					FlushInterval: time.Second,
				},
			},
			Routes: []LevelRoute{
				{
					Name:   "access.log",
					Levels: AllLevelStrs,
				},
			},
		},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	defer Release(logger)
	defer func() {
		if err := recover(); err != nil {
			return
		}
	}()
	start := time.Now()
	logger.Debug("-----------debug test")

	//Logs.Fatal("fatal test")
	//Logs.Panic("panic test")
	logger.Info("-----------info test")
	logger.Error("-----------error test")

	end := time.Now()
	fmt.Println(end.Sub(start))
}

func BenchmarkName(b *testing.B) {
	logger, err := NewDefaultLogger(&LoggerConf{
		Dir:        "./logs",
		PrefixName: "xx",
		Level:      "info",
		Stdout:     true,
		Caller:     true,
		FullCaller: false,
		Color:      false,
		Rotation: &RotationConf{
			MaxAge: 15 * 24 * time.Hour,
			Every:  24 * time.Hour,
		},
		Routing: &RoutingConf{
			AsyncMode: &AsyncMode{
				Enable: true,
				Config: &AsyncWriterConfig{
					BufferSize:    1024,
					FlushInterval: time.Second,
				},
			},
			Routes: []LevelRoute{
				{
					Name:   "access.log",
					Levels: AllLevelStrs,
				},
			},
		},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	for i := 0; i < b.N; i++ {
		logger.Error("aaaa")
	}
}

func TestSingleFileViaDefaultLevelWriter(t *testing.T) {
	dir := t.TempDir()

	logger, err := NewDefaultLogger(&LoggerConf{
		Dir:        dir,
		PrefixName: "app",
		Level:      "debug",
		Rotation: &RotationConf{
			MaxAge: 15 * 24 * time.Hour,
			Every:  24 * time.Hour,
		},
		Routing: &RoutingConf{
			AsyncMode: &AsyncMode{
				Enable: false,
				Config: &AsyncWriterConfig{
					BufferSize:    1024,
					FlushInterval: time.Second,
				},
			},
			Routes: []LevelRoute{
				{
					Name:   "access.log",
					Levels: AllLevelStrs,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewDefaultLogger error: %v", err)
	}

	defer Release(logger)

	logger.Info("info msg")
	logger.Error("error msg")

	// 等待少许时间，确保写入完成（主要防止未来改为异步时 flaky）
	time.Sleep(100 * time.Millisecond)

	// 预期只有一个按日期切割的文件存在
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir error: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("expected at least one log file, got 0")
	}
}

func TestMultiFileByLevelRoutes(t *testing.T) {
	logger, err := NewDefaultLogger(&LoggerConf{
		Dir:        "./logs",
		PrefixName: "app",
		Level:      "info",
		Caller:     true,
		Stdout:     true,
		Rotation: &RotationConf{
			MaxAge: 15 * 24 * time.Hour,
			Every:  24 * time.Hour,
		},
		Routing: &RoutingConf{
			AsyncMode: &AsyncMode{
				Enable: false,
				Config: &AsyncWriterConfig{
					BufferSize:    1024,
					FlushInterval: time.Second,
				},
			},
			Routes: []LevelRoute{
				{
					Name:   "access.log",
					Levels: []string{InfoLevelStr, DebugLevelStr, WarnLevelStr, TraceLevelStr},
				},
				{
					Name:   "error.log",
					Levels: []string{ErrorLevelStr, FatalLevelStr, PanicLevelStr},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewDefaultLogger error: %v", err)
	}

	defer Release(logger)

	logger.Info("info msg")
	logger.Debug("debug msg")
	logger.Warn("warn msg")
	logger.Trace("trace msg")

	logger.Error("error msg")
	//logger.Fatal("fatal msg")
	//logger.Panic("panic msg")

	time.Sleep(100 * time.Millisecond)

	// 检查 info 文件和 error 文件都存在
	patternInfo := filepath.Join("./logs", "app_access.log.*")
	patternErr := filepath.Join("./logs", "app_error.log.*")

	matchesInfo, _ := filepath.Glob(patternInfo)
	matchesErr, _ := filepath.Glob(patternErr)

	if len(matchesInfo) == 0 {
		t.Fatalf("expected info log file matching %s", patternInfo)
	}
	if len(matchesErr) == 0 {
		t.Fatalf("expected error log file matching %s", patternErr)
	}

	// 简单打印下路径，便于调试
	fmt.Println("info files:", matchesInfo)
	fmt.Println("error files:", matchesErr)
}

func TestLoggerX(t *testing.T) {
	timelib.SetTimeOffset(time.Hour)
	logger, err := NewDefaultLogger(&LoggerConf{
		Dir:        "./logs",
		PrefixName: "app",
		Level:      "info",
		Stdout:     true,
		Caller:     true,
		FullCaller: false,
		Color:      true,
		Rotation: &RotationConf{
			MaxAge: 15 * 24 * time.Hour,
			Every:  24 * time.Hour,
		},
		Routing: &RoutingConf{
			AsyncMode: &AsyncMode{
				Enable: false,
				Config: &AsyncWriterConfig{
					BufferSize:    1024,
					FlushInterval: time.Second,
				},
			},
			Routes: []LevelRoute{
				{
					Name:   "access.log",
					Levels: []string{InfoLevelStr, DebugLevelStr, WarnLevelStr, TraceLevelStr},
				},
				{
					Name:   "error.log",
					Levels: []string{ErrorLevelStr, FatalLevelStr, PanicLevelStr},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewDefaultLogger error: %v", err)
	}
	defer Release(logger)

	// 创建一个 LoggerX 实例
	loggerX := NewLoggerX(logger, Fields{"app": "emberengine"})

	loggerX.Slow().Trace("trace msg")
	loggerX.Slow().Debug("debug msg")
	loggerX.Slow().Info("info msg")
	loggerX.Slow().Warn("warn msg")
	loggerX.Slow().Error("error msg")
	//func() {
	//	defer func() { _ = recover() }()
	//	loggerX.Slow().Panic("panic msg")
	//}()
	// Fatal 会触发 os.Exit(1)，不应在单测中直接调用。
	// loggerX.Slow().Fatal("fatal msg")
	//loggerX.State().Debug("debug msg")
	//loggerX.Metric().Warn("warn msg")
	//loggerX.Trace("trace msg")
	//loggerX.Error("error msg")

}

func TestNoAnsiInFileEvenWhenColorEnabled(t *testing.T) {
	dir := t.TempDir()

	logger, err := NewDefaultLogger(&LoggerConf{
		Dir:        dir,
		PrefixName: "app",
		Level:      "debug",
		Stdout:     true,
		Caller:     true,
		FullCaller: false,
		Color:      true,
		Rotation: &RotationConf{
			MaxAge: 15 * 24 * time.Hour,
			Every:  24 * time.Hour,
		},
		Routing: &RoutingConf{
			AsyncMode: &AsyncMode{
				Enable: false,
				Config: &AsyncWriterConfig{BufferSize: 1024, FlushInterval: time.Second},
			},
			Routes: []LevelRoute{{Name: "access.log", Levels: AllLevelStrs}},
		},
	})
	if err != nil {
		t.Fatalf("NewDefaultLogger error: %v", err)
	}
	defer Release(logger)

	logger.Error("error msg")
	time.Sleep(100 * time.Millisecond)

	pattern := filepath.Join(dir, "app_access.log.*")
	matches, _ := filepath.Glob(pattern)
	if len(matches) == 0 {
		t.Fatalf("expected log file matching %s", pattern)
	}

	bs, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if strings.Contains(string(bs), "\x1b[") {
		t.Fatalf("expected no ANSI escapes in file output")
	}
}
