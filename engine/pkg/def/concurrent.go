// Package def
// @Title  title
// @Description  desc
// @Author  yr  2025/2/10
// @Update  yr  2025/2/10
package def

type ConcurrentTaskCallback struct {
	Callback func(err error, args ...interface{})
	Args     []interface{}
}
