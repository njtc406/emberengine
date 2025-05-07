// Package mailbox
// @Title  策略构建器
// @Description  desc
// @Author  yr  2025/4/24
// @Update  yr  2025/4/24
package mailbox

import (
	"fmt"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"sync"
)

const (
	DefaultStrategyName   = "default"
	CompositeStrategyName = "composite"
	CPUBasedStrategyName  = "cpu"
)

type StrategyBuilder func([]AutoScalerStrategy, map[string]interface{}) AutoScalerStrategy

var locker sync.RWMutex
var builderMap = map[string]StrategyBuilder{
	DefaultStrategyName:   newDefaultStrategy,
	CPUBasedStrategyName:  newCPUBasedStrategy,
	CompositeStrategyName: newCompositeStrategy,
}

// RegisterStrategy 注册策略
func RegisterStrategy(name string, strategy StrategyBuilder) {
	locker.Lock()
	defer locker.Unlock()
	builderMap[name] = strategy
}

func BuildStrategy(cfg *config.WorkerStrategyConfig) (AutoScalerStrategy, error) {
	if cfg == nil {
		return newDefaultStrategy(nil, map[string]interface{}{"minLoadThreshold": 10}), nil
	}
	if cfg.Name == "" {
		cfg.Name = DefaultStrategyName
	}
	locker.RLock()
	builder, ok := builderMap[cfg.Name]
	if !ok {
		locker.RUnlock()
		return nil, fmt.Errorf("unknown strategy name: %s", cfg.Name)
	}
	locker.RUnlock()

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
