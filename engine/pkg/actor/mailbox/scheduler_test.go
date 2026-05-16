package mailbox

import (
	"testing"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPriorityScheduler_DoesNotMutateCallerConf(t *testing.T) {
	conf := &config.MultiLevelWorkerConf{
		Strategy: def.StrategyWeighted,
		PriorityBatches: map[def.Priority]*config.PriorityConfig{
			def.PriorityNormal: {BatchSize: 8, Weight: 3},
			def.PriorityHigh:   {BatchSize: 16, Weight: 5},
		},
	}

	origNormalWeight := conf.PriorityBatches[def.PriorityNormal].Weight

	s := NewPriorityScheduler(conf)
	require.NotNil(t, s)

	// 修改调用方配置
	conf.PriorityBatches[def.PriorityNormal].Weight = 999

	// scheduler 内部不受影响
	assert.Equal(t, origNormalWeight, s.weights[def.PriorityNormal], "scheduler weights should be isolated from caller mutation")
}

func TestNewPriorityScheduler_NilConf(t *testing.T) {
	s := NewPriorityScheduler(nil)
	require.NotNil(t, s)
	assert.Greater(t, len(s.priorities), 0)
}

func TestNewPriorityScheduler_ModifyCallerMap(t *testing.T) {
	conf := &config.MultiLevelWorkerConf{
		Strategy: def.StrategyAbsolute,
		PriorityBatches: map[def.Priority]*config.PriorityConfig{
			def.PriorityNormal: {BatchSize: 8, Weight: 3},
		},
	}

	s := NewPriorityScheduler(conf)

	// 向调用方 map 添加新 key
	conf.PriorityBatches[def.PriorityHigh] = &config.PriorityConfig{BatchSize: 16, Weight: 5}

	// scheduler 不受影响
	_, exists := s.priorities[def.PriorityHigh]
	assert.False(t, exists, "scheduler priorities should not be affected by caller map mutation")
}
