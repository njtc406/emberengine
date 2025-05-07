// Package mailbox
// @Title  自动扩容器
// @Description  desc
// @Author  yr  2025/4/22
// @Update  yr  2025/4/22
package mailbox

import (
	"fmt"
	"math"
	"time"
)

func clamp(val, min, max int) int {
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}

type AutoScaler struct {
	MinWorkers     int
	MaxWorkers     int
	GrowthFactor   float64
	ShrinkFactor   float64
	ResizeCoolDown time.Duration
	lastResizeTime time.Time
	Strategy       AutoScalerStrategy // 策略接口
}

func (s *AutoScaler) ShouldResize(current int, workers []*Worker) (int, string, bool) {
	now := time.Now()
	if now.Sub(s.lastResizeTime) < s.ResizeCoolDown {
		return 0, "", false
	}

	var (
		newSize int
		reason  string
	)

	// 组合策略决策
	if s.Strategy.ShouldScaleUp(workers) {
		// 指数增长扩容
		add := int(math.Ceil(float64(current) * s.GrowthFactor))
		newSize = clamp(current+add, s.MinWorkers, s.MaxWorkers)
		reason = fmt.Sprintf("scale up: strategy triggered")
	} else if s.Strategy.ShouldScaleDown(workers, s.MinWorkers) {
		// 比例缩减容
		reduce := int(math.Floor(float64(current) * s.ShrinkFactor))
		newSize = clamp(current-reduce, s.MinWorkers, s.MaxWorkers)
		reason = fmt.Sprintf("scale down: strategy triggered")
	} else {
		return current, "", false
	}

	// 只有当数量变化时才更新
	if newSize != current {
		s.lastResizeTime = now
		return newSize, reason, true
	}

	return current, "", false
}
