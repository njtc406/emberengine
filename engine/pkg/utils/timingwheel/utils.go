package timingwheel

import (
	"sync"
	"time"
)

// truncate 将 x 向零方向舍入到 m 的倍数。如果 m <= 0，则返回原值。
func truncate(x, m int64) int64 {
	if m <= 0 {
		return x
	}
	return x - x%m
}

// timeToMs 将 time.Time 转换为毫秒表示的整数。
func timeToMs(t time.Time) int64 {
	return t.UnixNano() / int64(time.Millisecond)
}

// msToTime 将以毫秒为单位的 Unix 时间转换为 UTC 时间。
func msToTime(t int64) time.Time {
	return time.Unix(0, t*int64(time.Millisecond)).UTC()
}

type waitGroupWrapper struct {
	sync.WaitGroup
}

func (w *waitGroupWrapper) Wrap(cb func()) {
	w.Add(1)
	go func() {
		cb()
		w.Done()
	}()
}
