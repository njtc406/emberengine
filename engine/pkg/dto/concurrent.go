// Package dto
// @Title  title
// @Description  desc
// @Author  yr  2025/2/10
// @Update  yr  2025/2/10
package dto

type ConcurrentTaskCallback struct {
	Callback func(err error, args ...interface{})
	Args     []interface{}
}
