// Package mailbox
// @Title  策略构建器
// @Description  根据 MailboxConf.ScalingStrategy 配置构建对应的扩缩容策略实例，为 WorkerPool 与 AutoScaler 提供统一入口。
// @Author  yr  2025/4/24
// @Update  yr  2026/4/27
package mailbox

import (
	"fmt"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/utils/syncx"
)

const (
	MaxLoadStrategyName   = "max_load"  // 最大负载策略（推荐）
	CompositeStrategyName = "composite" // 复合策略
)

type StrategyBuilder func(subs []AutoScalerStrategy, params map[string]interface{}) AutoScalerStrategy

var builderMap = syncx.Map[string, StrategyBuilder]{}

func init() {
	builderMap.Store(MaxLoadStrategyName, newMaxLoadStrategy)
	builderMap.Store(CompositeStrategyName, newCompositeStrategy)
}

// RegisterStrategy 注册策略
func RegisterStrategy(name string, strategy StrategyBuilder) {
	builderMap.Store(name, strategy)
}

const maxStrategyDepth = 10 // 防止配置循环引用导致无限递归

func BuildStrategy(cfg *config.WorkerStrategyConfig) (AutoScalerStrategy, error) {
	return buildStrategyRecur(cfg, 0)
}

func buildStrategyRecur(cfg *config.WorkerStrategyConfig, depth int) (AutoScalerStrategy, error) {
	if depth > maxStrategyDepth {
		return nil, fmt.Errorf("strategy nesting too deep (max %d), possible circular reference", maxStrategyDepth)
	}
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
		strategy, err := buildStrategyRecur(item, depth+1)
		if err != nil {
			return nil, err
		}
		subs = append(subs, strategy)
	}

	return builder(subs, cfg.Params), nil
}
