/*
 * Copyright (c) 2023. YR. All rights reserved
 */

// Package errorlib
// 模块名: 调用者
// 功能描述: 用来描述调用者信息
// 作者:  yr  2023/7/3 0003 16:44
// 最后更新:  yr  2023/7/3 0003 16:44
package errorlib

import "fmt"

// caller 调用者信息
type caller struct {
	file string
	line int
}

func (c caller) string() string {
	return fmt.Sprintf("%s:%d", c.file, c.line)
}
