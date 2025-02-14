// Package dto
// @Title  title
// @Description  desc
// @Author  yr  2024/11/14
// @Update  yr  2024/11/14
package dto

import "sync/atomic"

type DataRef struct {
	ref int32 `json:"-"`
}

func (d *DataRef) IsRef() bool {
	return atomic.LoadInt32(&d.ref) == 1
}

func (d *DataRef) Ref() {
	atomic.StoreInt32(&d.ref, 1)
}

func (d *DataRef) UnRef() {
	atomic.StoreInt32(&d.ref, 0)
}
