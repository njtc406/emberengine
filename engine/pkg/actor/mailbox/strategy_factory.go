// Package mailbox
// @Title  策略构建器
// @Description  desc
// @Author  yr  2025/4/24
// @Update  yr  2025/4/24
package mailbox

import (
	"fmt"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/utils/syncx"
)

const (
	MaxLoadStrategyName   = "mailbox_load"
	CPUBasedStrategyName  = "cpu"
	CompositeStrategyName = "composite"
)

type StrategyBuilder func(subs []AutoScalerStrategy, params map[string]interface{}) AutoScalerStrategy

var builderMap = syncx.Map[string, StrategyBuilder]{}

func init() {
	builderMap.Store(MaxLoadStrategyName, newMaxLoadStrategy)
	builderMap.Store(CPUBasedStrategyName, newCPUBasedStrategy)
	builderMap.Store(CompositeStrategyName, newCompositeStrategy)
}

// RegisterStrategy 注册策略
func RegisterStrategy(name string, strategy StrategyBuilder) {
	builderMap.Store(name, strategy)
}

func BuildStrategy(cfg *config.WorkerStrategyConfig) (AutoScalerStrategy, error) {
	if cfg == nil {
		return newMaxLoadStrategy(nil, map[string]interface{}{"minLoadThreshold": 64}), nil
	}
	if cfg.Name == "" {
		cfg.Name = MaxLoadStrategyName
	}
	builder, ok := builderMap.Load(cfg.Name)
	if !ok {
		return nil, fmt.Errorf("unknown strategy name: %s", cfg.Name)
	}

	var subs []AutoScalerStrategy
	for _, item := range cfg.Subs {
		strategy, err := BuildStrategy(item)
		if err != nil {
			return nil, err
		}
		subs = append(subs, strategy)
	}

	return builder(subs, cfg.Params), nil
}
