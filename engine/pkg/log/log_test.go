package log

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInfo(t *testing.T) {
	logger, err := NewDefaultLogger("./", &LoggerConf{
		Path:  "log",
		Name:  "xx.log",
		Level: "info",
		AsyncMode: &AsyncMode{
			Enable: true,
			Config: &AsyncWriterConfig{
				BufferSize:    1024,
				FlushInterval: time.Second,
			},
		},
		Caller:       true,
		FullCaller:   true,
		Color:        false,
		MaxAge:       time.Hour * 24 * 15,
		RotationTime: time.Hour * 24,
	}, true)
	if err != nil {
		fmt.Println(err)
		return
	}
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
	logger.Error("-----------error test")
	logger.Error("-----------error test")
	logger.Error("-----------error test")
	logger.Error("-----------error test")
	logger.Error("-----------error test")
	logger.Error("-----------error test")
	logger.Error("-----------error test")

	end := time.Now()
	fmt.Println(end.Sub(start))
	Release(logger)
}

func BenchmarkName(b *testing.B) {
	logger, err := NewDefaultLogger("./", &LoggerConf{
		Path:  "log",
		Name:  "xx.log",
		Level: "info",
		AsyncMode: &AsyncMode{
			Enable: true,
			Config: &AsyncWriterConfig{
				BufferSize:    1024,
				FlushInterval: time.Second,
			},
		},
		Caller:       true,
		FullCaller:   true,
		Color:        false,
		MaxAge:       time.Hour * 24 * 15,
		RotationTime: time.Hour * 24,
	}, true)
	if err != nil {
		fmt.Println(err)
		return
	}
	for i := 0; i < b.N; i++ {
		logger.Error("aaaa")
	}

	Release(logger)
}

func TestSingleFileViaDefaultLevelWriter(t *testing.T) {
	dir := t.TempDir()

	logger, err := NewDefaultLogger(dir, &LoggerConf{
		Path:  "",
		Name:  "app.log",
		Level: "debug",
		AsyncMode: &AsyncMode{
			Enable: false, // 简化测试，同步写入
		},
	}, false)
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
	dir := t.TempDir()

	logger, err := NewDefaultLogger(dir, &LoggerConf{
		Path:  "",
		Name:  "app.log",
		Level: "debug",
		AsyncMode: &AsyncMode{
			Enable: false, // 为了简单起见，先用同步写入
		},
		LevelWriter: &LevelWriterConf{
			Sync: true,
			Routes: []LevelRoute{
				{
					Levels: []Level{ErrorLevel, FatalLevel, PanicLevel},
					Name:   "error",
				},
				{
					Levels: []Level{InfoLevel, DebugLevel},
					Name:   "info",
				},
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("NewDefaultLogger error: %v", err)
	}
	defer Release(logger)

	logger.Info("info msg")
	logger.Error("error msg")

	time.Sleep(100 * time.Millisecond)

	// 检查 info 文件和 error 文件都存在
	patternInfo := filepath.Join(dir, "app.log_info_*.log")
	patternErr := filepath.Join(dir, "app.log_error_*.log")

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
