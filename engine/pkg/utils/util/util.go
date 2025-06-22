// Package util
// @Title  title
// @Description  desc
// @Author  yr  2025/4/24
// @Update  yr  2025/4/24
package util

import (
	"github.com/shirou/gopsutil/v4/cpu"
	"golang.org/x/exp/constraints"
	"math/rand/v2"
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
