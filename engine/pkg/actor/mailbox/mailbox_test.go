package mailbox

import (
	"testing"

	"github.com/njtc406/emberengine/v2/engine/pkg/config"
	"github.com/njtc406/emberengine/v2/engine/pkg/def"
	"github.com/stretchr/testify/require"
)

func TestNewMailboxDefaultPriorityConfig(t *testing.T) {
	conf := &config.MailboxConf{
		QueueMode:      QueueModePriority,
		SchedulePolicy: &config.WorkerSchedulePolicy{InitialWorkerNum: 2},
	}
	invoker := &mockInvoker{name: "default-priority"}
	m, err := NewMailbox(conf, &testLogger{t: t}, invoker, nil)
	require.NoError(t, err)
	require.NotNil(t, m)
	m.Start()
	t.Cleanup(m.Stop)

	snapshot := m.workerPool.snap.Load()
	require.Equal(t, 2, snapshot.count)
	for _, worker := range snapshot.workers {
		w, ok := worker.(*Worker)
		require.True(t, ok)
		queues, ok := w.queueManager.(*PriorityQueueManager)
		require.True(t, ok, "priority mode must create priority queues")
		require.Equal(t, map[def.Priority]int{
			def.PrioritySys: 64, def.PriorityUrgent: 32, def.PriorityHigh: 16,
			def.PriorityNormal: 8, def.PriorityLow: 4, def.PriorityBatch: 2,
		}, queues.batchSizes)
	}

	for _, priority := range []def.Priority{
		def.PrioritySys, def.PriorityUrgent, def.PriorityHigh,
		def.PriorityNormal, def.PriorityLow, def.PriorityBatch,
	} {
		job := newRWJob(def.RWModeWrite, "default-priorities")
		job.SetPriority(priority)
		require.NoError(t, m.PostJob(job))
	}
	m.Stop()
	require.EqualValues(t, 6, invoker.writeCount.Load())
}

func TestNewMailboxCustomPriorityConfig(t *testing.T) {
	priorities := map[def.Priority]*config.PriorityConfig{
		def.PriorityUrgent: {BatchSize: 50, Weight: 10},
		def.PriorityHigh:   {BatchSize: 30, Weight: 7},
		def.PriorityNormal: {BatchSize: 20, Weight: 5},
	}
	conf := &config.MailboxConf{
		QueueMode: QueueModePriority,
		SchedulePolicy: &config.WorkerSchedulePolicy{
			InitialWorkerNum: 2,
			MultiLevelQueueConf: &config.MultiLevelQueueConf{
				Strategy:        def.StrategyWeighted,
				PriorityBatches: priorities,
			},
		},
	}
	invoker := &mockInvoker{name: "custom-priority"}
	m, err := NewMailbox(conf, &testLogger{t: t}, invoker, nil)
	require.NoError(t, err)
	m.Start()
	t.Cleanup(m.Stop)

	for _, worker := range m.workerPool.snap.Load().workers {
		w, ok := worker.(*Worker)
		require.True(t, ok)
		queues, ok := w.queueManager.(*PriorityQueueManager)
		require.True(t, ok)
		require.Equal(t, map[def.Priority]int{
			def.PriorityUrgent: 50, def.PriorityHigh: 30, def.PriorityNormal: 20,
		}, queues.batchSizes)
		require.Equal(t, def.StrategyWeighted, queues.scheduler.strategy)
		for priority, want := range priorities {
			require.Equal(t, want.Weight, queues.scheduler.priorities[priority].Weight)
		}
	}
	for priority := range priorities {
		job := newRWJob(def.RWModeWrite, "custom-priorities")
		job.SetPriority(priority)
		require.NoError(t, m.PostJob(job))
	}
	m.Stop()
	require.EqualValues(t, 3, invoker.writeCount.Load())
}

func TestNewMailboxWorkerScheduleConfig(t *testing.T) {
	// Worker configuration is supplied at construction, not via a runtime setter.
	for _, mode := range []string{QueueModeDual, QueueModePriority} {
		t.Run(mode, func(t *testing.T) {
			conf := &config.MailboxConf{
				QueueMode: mode,
				SchedulePolicy: &config.WorkerSchedulePolicy{
					InitialWorkerNum: 1,
					MultiLevelQueueConf: &config.MultiLevelQueueConf{
						Strategy: def.StrategyWeighted,
						PriorityBatches: map[def.Priority]*config.PriorityConfig{
							def.PriorityHigh:   {BatchSize: 30, Weight: 7},
							def.PriorityNormal: {BatchSize: 20, Weight: 5},
						},
					},
				},
			}
			m, err := NewMailbox(conf, &testLogger{t: t}, &mockInvoker{name: mode}, nil)
			require.NoError(t, err)
			m.Start()
			t.Cleanup(m.Stop)
			snapshot := m.workerPool.snap.Load()
			require.Equal(t, 1, snapshot.count)
			w, ok := snapshot.sole.(*Worker)
			require.True(t, ok)
			if mode == QueueModeDual {
				require.IsType(t, &DualQueueManager{}, w.queueManager)
			} else {
				queues, ok := w.queueManager.(*PriorityQueueManager)
				require.True(t, ok)
				require.Equal(t, map[def.Priority]int{
					def.PriorityHigh: 30, def.PriorityNormal: 20,
				}, queues.batchSizes)
				require.Equal(t, def.StrategyWeighted, queues.scheduler.strategy)
			}
		})
	}
}
