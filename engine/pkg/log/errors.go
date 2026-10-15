/*
 * Copyright (c) 2023. YR. All rights reserved
 */

// Package log
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2023/5/22 0022 21:49
// 最后更新:  yr  2023/5/22 0022 21:49
package log

import "errors"

var (
	ErrRotationTime = errors.New("rotationTime must >= 1min and <= 24hour")
	ErrLevel        = errors.New("log level must <= 6")
)
