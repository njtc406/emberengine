package mailbox

import (
	"testing"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPriorityQueueManager_DoesNotMutateCallerConf(t *testing.T) {
	conf := &config.MultiLevelQueueConf{
		Strategy:        def.StrategyAbsolute,
		TotalBatchLimit: 32,
		PriorityBatches: map[def.Priority]*config.PriorityConfig{
			def.PriorityNormal: {BatchSize: 8, Weight: 3},
			def.PriorityHigh:   {BatchSize: 16, Weight: 5},
		},
	}

	// 保存原始状态
	origLen := len(conf.PriorityBatches)
	origNormalBatch := conf.PriorityBatches[def.PriorityNormal].BatchSize
	origHighBatch := conf.PriorityBatches[def.PriorityHigh].BatchSize

	_ = NewPriorityQueueManager(conf, nil)

	// 验证调用方配置未被修改
	assert.Equal(t, origLen, len(conf.PriorityBatches), "PriorityBatches map length should not change")
	assert.Equal(t, origNormalBatch, conf.PriorityBatches[def.PriorityNormal].BatchSize)
	assert.Equal(t, origHighBatch, conf.PriorityBatches[def.PriorityHigh].BatchSize)
}

func TestNewPriorityQueueManager_EmptyPriorityBatches_NoMutation(t *testing.T) {
	conf := &config.MultiLevelQueueConf{
		Strategy:        def.StrategyAbsolute,
		TotalBatchLimit: 32,
		PriorityBatches: map[def.Priority]*config.PriorityConfig{},
	}

	m := NewPriorityQueueManager(conf, nil)

	// 原始 map 应仍为空
	assert.Empty(t, conf.PriorityBatches, "caller's empty PriorityBatches should remain empty")
	// manager 应正常工作（内部使用了默认配置）
	require.NotNil(t, m)
	assert.Greater(t, len(m.sortedPriorities), 0)
}

func TestNewPriorityQueueManager_NilConf(t *testing.T) {
	m := NewPriorityQueueManager(nil, nil)
	require.NotNil(t, m)
	assert.Greater(t, len(m.sortedPriorities), 0)
}

func TestNewPriorityQueueManager_ModifyCallerAfterConstruction(t *testing.T) {
	conf := &config.MultiLevelQueueConf{
		Strategy:        def.StrategyAbsolute,
		TotalBatchLimit: 32,
		PriorityBatches: map[def.Priority]*config.PriorityConfig{
			def.PriorityNormal: {BatchSize: 8, Weight: 3},
		},
	}

	m := NewPriorityQueueManager(conf, nil)

	// 修改调用方配置
	conf.PriorityBatches[def.PriorityNormal].BatchSize = 999

	// manager 内部不受影响
	assert.Equal(t, 8, m.batchSizes[def.PriorityNormal], "manager batchSizes should be isolated from caller mutation")
}
