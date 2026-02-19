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
		reason = fmt.Sprintf("scale up: strategy triggered")
	} else if s.Strategy.ShouldScaleDown(workers, s.conf.MinWorkerNum) {
		// 比例缩减容
		reduce := int32(math.Floor(float64(cur) * s.conf.ShrinkFactor))
		newSize = clamp(cur-reduce, s.conf.MinWorkerNum, s.conf.MaxWorkerNum)
		reason = fmt.Sprintf("scale down: strategy triggered")
	} else {
		return int32(current), "", false
	}

	// 只有当数量变化时才更新
	if newSize != cur {
		s.lastResizeTime = now
		return newSize, reason, true
	}

	return cur, "", false
}
