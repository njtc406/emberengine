// Package timingwheel
// @Title  title
// @Description  desc
// @Author  yr  2025/1/13
// @Update  yr  2025/1/13
package timingwheel

import (
	"sync"
	"time"
)

var (
	tw         *TimingWheel
	once       = new(sync.Once)
	cronParser Parser
)

func init() {
	// 格式: 秒,分,时,日,月,周,
	cronParser = NewParser(Second | Minute | Hour | Dom | Month | Dow | DowOptional | Descriptor)
}

func Start(interval time.Duration, wheelSize int64) {
	if tw != nil {
		return
	}
	once.Do(func() {
		tw = NewTimingWheel(interval, wheelSize)
		tw.Start()
	})
}

func Stop() {
	if tw != nil {
		tw.Stop()
	}
}
