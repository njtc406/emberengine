// Package inf
// @Title  title
// @Description  desc
// @Author  yr  2024/11/14
// @Update  yr  2024/11/14
package timer

import "time"

// ITimer 定时器接口
type ITimer interface {
	GetId() uint64
	Cancel()
	GetName() string
	IsActive() bool
	IsOpen() bool
	Open(bOpen bool)
	AppendChannel(timer ITimer)
	Do()
	GetFireTime() time.Time
	SetupTimer(now time.Time) error
}
