// Package util
// @Title  title
// @Description  desc
// @Author  yr  2025/4/24
// @Update  yr  2025/4/24
package util

import (
	"fmt"
	"github.com/shirou/gopsutil/v4/cpu"
	"golang.org/x/exp/constraints"
	"math/rand/v2"
	"strconv"
)

func GetCPULoad() float64 {
	percents, err := cpu.Percent(0, false)
	if err != nil || len(percents) == 0 {
		return 0.0
	}
	return percents[0] / 100 // 转成 0.0 - 1.0 之间
}

func RandN[T constraints.Integer | constraints.Float](n T) T {
	switch any(n).(type) {
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return T(rand.Int64N(int64(n)))
	case float32, float64:
		return T(rand.Float64() * float64(n))
	default:
		return 0
	}
}

func ToString(val any) string {
	switch v := val.(type) {
	case string:
		return v
	case int:
		return strconv.FormatInt(int64(v), 10)
	case int8:
		return strconv.FormatInt(int64(v), 10)
	case int16:
		return strconv.FormatInt(int64(v), 10)
	case int32:
		return strconv.FormatInt(int64(v), 10)
	case int64:
		return strconv.FormatInt(v, 10)
	case uint:
		return strconv.FormatUint(uint64(v), 10)
	case uint8:
		return strconv.FormatUint(uint64(v), 10)
	case uint16:
		return strconv.FormatUint(uint64(v), 10)
	case uint32:
		return strconv.FormatUint(uint64(v), 10)
	case uint64:
		return strconv.FormatUint(v, 10)
	case error:
		return v.Error()
	case fmt.Stringer:
		return v.String()
	}
	return ""
}

// ToIntT 转换为T类型
func ToIntT[T constraints.Integer](val any) T {
	switch v := val.(type) {
	case int:
		return T(v)
	case int8:
		return T(v)
	case int16:
		return T(v)
	case int32:
		return T(v)
	case int64:
		return T(v)
	case uint:
		return T(v)
	case uint8:
		return T(v)
	case uint16:
		return T(v)
	case uint32:
		return T(v)
	case uint64:
		return T(v)
	case float32:
		return T(v)
	case float64:
		return T(v)
	case string:
		if v == "" {
			return 0
		}
		// 尝试先解析为int64，如果失败再尝试uint64
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return T(i)
		}
		if u, err := strconv.ParseUint(v, 10, 64); err == nil {
			return T(u)
		}
		return 0
	case bool:
		if v {
			return 1
		}
		return 0
	default:
		return 0
	}
}
