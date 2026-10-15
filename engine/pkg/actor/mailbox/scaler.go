// Package mailbox
// @Title  自动扩容器
// @Description  根据负载策略对 WorkerPool 执行扩缩容决策，并维护冷却窗口避免抖动。
// @Author  yr  2025/4/22
// @Update  yr  2026/4/27
package mailbox

import (
	"math"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/config"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

func clamp(val, min, max int32) int32 {
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}

type AutoScaler struct {
	conf           *config.WorkerStrategyConfig
	lastResizeTime time.Time
	Strategy       AutoScalerStrategy // 策略接口
}

func (s *AutoScaler) ShouldResize(current int, workers []inf.IMailboxWorker) (int32, string, bool) {
	now := time.Now()
	if now.Sub(s.lastResizeTime) < s.conf.ResizeCoolDown {
		return 0, "", false
	}

	cur := int32(current)
	var (
		newSize int32
		reason  string
	)

	// 组合策略决策
	if s.Strategy.ShouldScaleUp(workers) {
		// 指数增长扩容
		add := int32(math.Ceil(float64(cur) * s.conf.GrowthFactor))
		newSize = clamp(cur+add, s.conf.MinWorkerNum, s.conf.MaxWorkerNum)
		reason = "scale up: strategy triggered"
	} else if s.Strategy.ShouldScaleDown(workers, s.conf.MinWorkerNum) {
		// 比例缩减容
		reduce := int32(math.Floor(float64(cur) * s.conf.ShrinkFactor))
		newSize = clamp(cur-reduce, s.conf.MinWorkerNum, s.conf.MaxWorkerNum)
		reason = "scale down: strategy triggered"
	} else {
		// 策略未触发任何动作：同样走过冷却，
		// 避免持续过载下策略被热评估。
		s.lastResizeTime = now
		return int32(current), "", false
	}

	// 达到边界（如 newSize == cur，被上/下限 clamp住）也均走冷却更新，
	// 避免“某黑路径上冷却时间不动”导致下一 tick 立即重复评估。
	s.lastResizeTime = now
	if newSize != cur {
		return newSize, reason, true
	}
	return cur, "", false
}
