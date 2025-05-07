// Package util
// @Title  title
// @Description  desc
// @Author  yr  2025/4/24
// @Update  yr  2025/4/24
package util

import (
	"github.com/shirou/gopsutil/v4/cpu"
	"math/rand"
	"time"
)

func GetCPULoad() float64 {
	percents, err := cpu.Percent(0, false)
	if err != nil || len(percents) == 0 {
		return 0.0
	}
	return percents[0] / 100 // 转成 0.0 - 1.0 之间
}

func RandIntn(n int) int {
	return rand.New(rand.NewSource(time.Now().UnixNano())).Intn(n)
}
