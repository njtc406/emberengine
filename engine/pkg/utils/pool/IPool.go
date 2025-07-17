// Package pool
// @Title  title
// @Description  desc
// @Author  yr  2025/7/17
// @Update  yr  2025/7/17
package pool

type IPool[T any] interface {
	Get() T
	Put(obj T)
	Stats() *Stats
}
