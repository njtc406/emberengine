// Package def
// @Title  title
// @Description  desc
// @Author  yr  2025/2/11
// @Update  yr  2025/2/11
package def

import "reflect"

type MethodInfo struct {
	Handler  reflect.Value
	Method   reflect.Method
	In       []reflect.Type
	Out      []reflect.Type
	MultiOut bool // 是否是多参数返回(排除error以外,还有两个及以上的返回值)
}
