// Package timingwheel
// 模块名: timingwheel
// 说明: 定时器时间轮模块，提供全局启动/停止与时间偏移设置接口
// 作者: yr
// 更新: 2025/1/13
package timingwheel

import (
	"fmt"
	"sync"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/log"
)

var (
	globTW     *TimingWheel
	twMutex    sync.Mutex // 保护tw的并发访问
	cronParser Parser
)

func init() {
	// 格式: 秒,分,时,日,月,周,
	cronParser = NewParser(Second | Minute | Hour | Dom | Month | Dow | DowOptional | Descriptor)
}

func Start(interval time.Duration, wheelSize int64, logger *log.Logger) {
	twMutex.Lock()
	defer twMutex.Unlock()

	if globTW != nil {
		return
	}

	if interval <= 0 {
		interval = time.Second
	}

	if logger == nil {
		l, err := log.NewDefaultLogger(&log.LoggerConf{
			OutputFormat: log.TextFormat,
			Dir:          "./logs",
			PrefixName:   "timingwheel",
			Level:        log.InfoLevelStr,
		})
		if err != nil {
			panic(fmt.Sprintf("create logger failed: %v", err))
		}
		logger = l
	}

	globTW = NewTimingWheel(interval, wheelSize, log.NewLoggerX(logger, log.Fields{"pkg": "timingwheel"}))
	globTW.Start()
}

func Stop() {
	twMutex.Lock()
	defer twMutex.Unlock()

	if globTW != nil {
		globTW.Stop()
		globTW = nil
	}
}

func GetTimingWheel() *TimingWheel {
	twMutex.Lock()
	defer twMutex.Unlock()
	return globTW
}

// SetTimeOffset 设置全局时间轮的时间偏移。
// 这是统一的时间调整入口，offset 为可正可负的时间偏移量。
//
// 该操作为同步操作，会阻塞所有定时器操作直到完成。如果不使用该功能，偏移量保持为 0，
// 不会对性能产生影响。
//
// 示例：
//
//	timingwheel.SetTimeOffset(time.Hour) // 向前调整 1 小时
//	timingwheel.SetTimeOffset(-30 * time.Minute) // 向后调整 30 分钟
func SetTimeOffset(offset time.Duration) {
	twMutex.Lock()
	tw := globTW
	twMutex.Unlock()

	if tw != nil {
		tw.SetTimeOffset(offset)
	}
}
