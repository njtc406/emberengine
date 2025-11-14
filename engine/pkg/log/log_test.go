package log

import (
	"fmt"
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
