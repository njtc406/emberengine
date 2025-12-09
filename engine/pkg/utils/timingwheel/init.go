// Package timingwheel
// @Title  title
// @Description  desc
// @Author  yr  2025/1/13
// @Update  yr  2025/1/13
package timingwheel

import (
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

	globTW = NewTimingWheel(interval, wheelSize, logger)
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

// AdjustTime adjusts all timers in the global timing wheel after time offset change.
// This is designed for development/testing environments only.
// offsetMs: the time offset in milliseconds (can be positive or negative)
//
// WARNING: This operation is expensive and will block all timer operations.
// DO NOT use in production environment.
//
// Usage:
//
//	timelib.SetTimeOffset(offset) // first adjust timelib
//	timingwheel.AdjustTime(offset / time.Millisecond) // then adjust timing wheel
func AdjustTime(offsetMs int64) {
	twMutex.Lock()
	tw := globTW
	twMutex.Unlock()

	if tw != nil {
		tw.AdjustTime(offsetMs)
	}
}
