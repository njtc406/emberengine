/*
 * Copyright (c) 2023. YR. All rights reserved
 */

// Package errorlib
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2023/6/7 0007 1:36
// 最后更新:  yr  2023/6/7 0007 1:36
package errorlib

import (
	"fmt"
	"testing"
)

func TestNewErrCode(t *testing.T) {
	err := NewErrCode(1, "原始错误", nil)
	fmt.Println(err)
	err1 := NewErrCode(2, "错误收集者提示错误", err)
	fmt.Println(err1)
	err2 := NewErrCode(3, err1)
	fmt.Println(err2)
	err3 := NewErrCode(4)
	fmt.Println(err3)
}

func BenchmarkName(b *testing.B) {
	for i := 0; i < b.N; i++ {
		err := NewErrCode(1, "验证操作", nil)
		_ = err.Error()
	}
}
