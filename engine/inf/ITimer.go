// Package inf
// @Title  title
// @Description  desc
// @Author  yr  2025/1/15
// @Update  yr  2025/1/15
package inf

type ITimer interface {
	Do()
	GetName() string
	GetTimerId() uint64
}
