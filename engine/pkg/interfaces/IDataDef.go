// Package interfaces
// @Title  title
// @Description  desc
// @Author  yr  2024/11/14
// @Update  yr  2024/11/14
package interfaces

type IDataDef interface {
	IsRef() bool
	Ref()
	UnRef()
}

type IReset interface {
	Reset()
}
