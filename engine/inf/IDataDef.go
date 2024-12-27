// Package inf
// @Title  title
// @Description  desc
// @Author  yr  2024/11/14
// @Update  yr  2024/11/14
package inf

type IDataDef interface {
	IsRef() bool
	Ref()
	UnRef()
}
