// Package interfaces
// @Title  title
// @Description  desc
// @Author  yr  2025/1/15
// @Update  yr  2025/1/15
package interfaces

type ITimer interface {
	Do()
	GetName() string
	GetTimerId() uint64
}
