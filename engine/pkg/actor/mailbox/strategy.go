// Package mailbox
// @Title  扩容策略
// @Description  desc
// @Author  yr  2025/4/24
// @Update  yr  2025/4/24
package mailbox

import "github.com/njtc406/emberengine/engine/pkg/utils/util"

type AutoScalerStrategy interface {
	ShouldScaleUp(workers []*Worker) bool
	ShouldScaleDown(workers []*Worker, min int) bool
}

// CompositeStrategy 组合自动扩容器
type CompositeStrategy struct {
	Strategies []AutoScalerStrategy
	Mode       string // "any" 或 "all"
}

func newCompositeStrategy(strategies []AutoScalerStrategy, params map[string]interface{}) AutoScalerStrategy {
	return &CompositeStrategy{
		Strategies: strategies,
		Mode:       params["mode"].(string),
	}
}

func (c *CompositeStrategy) ShouldScaleUp(workers []*Worker) bool {
	if c.Mode == "all" {
		for _, s := range c.Strategies {
			if !s.ShouldScaleUp(workers) {
				return false
			}
		}
		return true
	}

	// default: any
	for _, s := range c.Strategies {
		if s.ShouldScaleUp(workers) {
			return true
		}
	}
	return false
}

func (c *CompositeStrategy) ShouldScaleDown(workers []*Worker, min int) bool {
	if c.Mode == "all" {
		for _, s := range c.Strategies {
			if !s.ShouldScaleDown(workers, min) {
				return false
			}
		}
		return true
	}

	// default: any
	for _, s := range c.Strategies {
		if s.ShouldScaleDown(workers, min) {
			return true
		}
	}
	return false
}

type DefaultStrategy struct {
	MaxLoadThreshold int
}

func newDefaultStrategy(_ []AutoScalerStrategy, params map[string]interface{}) AutoScalerStrategy {
	return &DefaultStrategy{
		MaxLoadThreshold: params["max_load_threshold"].(int),
	}
}

func (d *DefaultStrategy) ShouldScaleUp(workers []*Worker) bool {
	for _, w := range workers {
		if w.userMailbox.Len() > d.MaxLoadThreshold {
			return true
		}
	}
	return false
}

func (d *DefaultStrategy) ShouldScaleDown(workers []*Worker, min int) bool {
	if len(workers) <= min {
		return false
	}

	idleCount := 0
	for _, w := range workers {
		if w.userMailbox.Len() == 0 && w.systemMailbox.Len() == 0 {
			idleCount++
		}
	}

	return idleCount > len(workers)/2
}

type CPUBasedStrategy struct {
	MinLoadThreshold float64 // 比如 0.3 表示 30%
	MaxLoadThreshold float64 // 比如 0.8 表示 80%
	GetCPULoad       func() float64
}

func newCPUBasedStrategy(_ []AutoScalerStrategy, params map[string]interface{}) AutoScalerStrategy {
	return &CPUBasedStrategy{
		MinLoadThreshold: params["min_load_threshold"].(float64),
		MaxLoadThreshold: params["max_load_threshold"].(float64),
		GetCPULoad:       util.GetCPULoad,
	}
}

func (s *CPUBasedStrategy) ShouldScaleUp(workers []*Worker) bool {
	if s.GetCPULoad() < s.MaxLoadThreshold {
		return false
	}

	avgLoad := 0
	for _, w := range workers {
		avgLoad += w.userMailbox.Len()
	}
	avgLoad /= len(workers)

	return avgLoad > 10
}

func (s *CPUBasedStrategy) ShouldScaleDown(workers []*Worker, min int) bool {
	if len(workers) <= min {
		return false
	}

	if s.GetCPULoad() > s.MinLoadThreshold {
		return false
	}

	idleCount := 0
	for _, w := range workers {
		if w.userMailbox.Len() == 0 && w.systemMailbox.Len() == 0 {
			idleCount++
		}
	}

	return idleCount > len(workers)/2
}
