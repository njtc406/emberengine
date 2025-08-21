// Package def
// @Title  title
// @Description  desc
// @Author  yr  2025/2/11
// @Update  yr  2025/2/11
package def

import "context"

type MethodCallFunc func(ctx context.Context, req interface{}) (interface{}, error)
