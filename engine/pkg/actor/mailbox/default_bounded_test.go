package mailbox

import (
	"github.com/njtc406/emberengine/v2/engine/pkg/config"
	"github.com/njtc406/emberengine/v2/engine/pkg/def"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestDefaultBoundedMailbox(t *testing.T) {
	for _, mode := range []string{"", "dual", "priority"} {
		for _, backend := range []string{"", "bounded", "unbounded"} {
			t.Run(mode+"/"+backend, func(t *testing.T) {
				c := &config.MailboxConf{QueueMode: mode, QueueBackend: backend}
				p, err := NewWorkerPool(c, nil, &mockInvoker{})
				require.NoError(t, err)
				defer p.cancel()
				if backend == "unbounded" {
					require.Equal(t, "unbounded", p.queueSpec.backend)
					return
				}
				require.Equal(t, "bounded", p.queueSpec.backend)
				require.Positive(t, p.queueSpec.storageBytes)
				m := createQueueManager(p.queueSpec, nil)
				for i := 0; i < 4096; i++ {
					require.NoError(t, m.Submit(&boundedManagerJob{priority: def.PriorityNormal}))
				}
				require.ErrorIs(t, m.Submit(&boundedManagerJob{priority: def.PriorityNormal}), def.ErrMailboxWorkerIsFull)
				require.NoError(t, m.Submit(&boundedManagerJob{priority: def.PrioritySys}))
				require.Nil(t, c.SchedulePolicy, "normalization must not mutate caller")
			})
		}
	}
	p, err := NewWorkerPool(nil, nil, &mockInvoker{})
	require.NoError(t, err)
	defer p.cancel()
	require.Equal(t, "bounded", p.queueSpec.backend)
}

func TestDefaultBoundedManagers(t *testing.T) {
	for _, m := range []IQueueManager{NewDualQueueManager(), NewPriorityQueueManager(nil, nil)} {
		for i := 0; i < 4096; i++ {
			require.NoError(t, m.Submit(&boundedManagerJob{priority: def.PriorityNormal}))
		}
		require.ErrorIs(t, m.Submit(&boundedManagerJob{priority: def.PriorityNormal}), def.ErrMailboxWorkerIsFull)
	}
}
