package log

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/njtc406/logrus"
)

func TestInfo(t *testing.T) {
	logger, err := NewDefaultLogger(&LoggerConf{
		Dir:        "./logs",
		Name:       "app",
		MinLevel:   "info",
		Stdout:     true,
		Caller:     true,
		FullCaller: true,
		Color:      false,
		Rotation: RotationConf{
			MaxAge: 15 * 24 * time.Hour,
			Every:  24 * time.Hour,
		},
		Routing: RoutingConf{
			AsyncMode: &AsyncMode{
				Enable: true,
				Config: &AsyncWriterConfig{
					BufferSize:    1024,
					FlushInterval: time.Second,
				},
			},
			Routes: []LevelRoute{
				{
					Name:   "info",
					Levels: logrus.AllLevels,
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
		Name:       "xx.log",
		MinLevel:   "info",
		Stdout:     true,
		Caller:     true,
		FullCaller: true,
		Color:      false,
		Rotation: RotationConf{
			MaxAge: 15 * 24 * time.Hour,
			Every:  24 * time.Hour,
		},
		Routing: RoutingConf{
			AsyncMode: &AsyncMode{
				Enable: true,
				Config: &AsyncWriterConfig{
					BufferSize:    1024,
					FlushInterval: time.Second,
				},
			},
			Routes: []LevelRoute{
				{
					Name:   "info",
					Levels: logrus.AllLevels,
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
		Dir:      dir,
		Name:     "app.log",
		MinLevel: "debug",
		Rotation: RotationConf{
			MaxAge: 15 * 24 * time.Hour,
			Every:  24 * time.Hour,
		},
		Routing: RoutingConf{
			AsyncMode: &AsyncMode{
				Enable: false,
				Config: &AsyncWriterConfig{
					BufferSize:    1024,
					FlushInterval: time.Second,
				},
			},
			Routes: []LevelRoute{
				{
					Name:   "info",
					Levels: logrus.AllLevels,
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
		Dir:      "./logs",
		Name:     "app.log",
		MinLevel: "info",
		Rotation: RotationConf{
			MaxAge: 15 * 24 * time.Hour,
			Every:  24 * time.Hour,
		},
		Routing: RoutingConf{
			AsyncMode: &AsyncMode{
				Enable: false,
				Config: &AsyncWriterConfig{
					BufferSize:    1024,
					FlushInterval: time.Second,
				},
			},
			Routes: []LevelRoute{
				{
					Name:   "info",
					Levels: []Level{InfoLevel, DebugLevel, WarnLevel, TraceLevel},
				},
				{
					Name:   "error",
					Levels: []Level{ErrorLevel, FatalLevel, PanicLevel},
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
	patternInfo := filepath.Join("./logs", "app.log_info_*.log")
	patternErr := filepath.Join("./logs", "app.log_error_*.log")

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
