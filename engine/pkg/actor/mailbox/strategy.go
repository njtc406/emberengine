// Package mailbox
// @Title  扩容策略
// @Description  定义 WorkerPool 扩缩容决策所需的策略接口与几种内置实现（根据队列长度 / 利用率等信息产出目标 worker 数）。
// @Author  yr  2025/4/24
// @Update  yr  2026/4/27
package mailbox

import (
	"strings"

	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

type AutoScalerStrategy interface {
	ShouldScaleUp(workers []inf.IMailboxWorker) bool
	ShouldScaleDown(workers []inf.IMailboxWorker, min int32) bool
}

// CompositeStrategy 组合自动扩容器
type CompositeStrategy struct {
	Strategies []AutoScalerStrategy
	Mode       string // "any" 或 "all"（大小写不敏感）
}

func newCompositeStrategy(strategies []AutoScalerStrategy, params map[string]interface{}) AutoScalerStrategy {
	mode, _ := params["Mode"].(string)
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "any"
	}
	return &CompositeStrategy{
		Strategies: strategies,
		Mode:       mode,
	}
}

func (c *CompositeStrategy) ShouldScaleUp(workers []inf.IMailboxWorker) bool {
	if strings.EqualFold(c.Mode, "all") {
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

func (c *CompositeStrategy) ShouldScaleDown(workers []inf.IMailboxWorker, min int32) bool {
	if strings.EqualFold(c.Mode, "all") {
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

type MaxLoadStrategy struct {
	IdleThreshold    int
	MaxLoadThreshold int
}

func newMaxLoadStrategy(_ []AutoScalerStrategy, params map[string]interface{}) AutoScalerStrategy {
	idleThreshold := 50
	if v, ok := params["IdleThreshold"].(int); ok {
		idleThreshold = v
	}
	maxLoadThreshold := 64
	if v, ok := params["MaxLoadThreshold"].(int); ok {
		maxLoadThreshold = v
	}
	return &MaxLoadStrategy{
		IdleThreshold:    idleThreshold,
		MaxLoadThreshold: maxLoadThreshold,
	}
}

func (d *MaxLoadStrategy) ShouldScaleUp(workers []inf.IMailboxWorker) bool {
	for _, w := range workers {
		if w.GetJobLen() > d.MaxLoadThreshold {
			return true
		}
	}
	return false
}

func (d *MaxLoadStrategy) ShouldScaleDown(workers []inf.IMailboxWorker, min int32) bool {
	if len(workers) <= int(min) {
		return false
	}

	idleCount := 0
	for _, w := range workers {
		if w.GetJobLen() == 0 {
			idleCount++
		}
	}

	return idleCount > len(workers)*d.IdleThreshold/100
}
