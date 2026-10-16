package mailbox

import (
	"fmt"
	"strings"
	"testing"

	"github.com/njtc406/emberengine/v2/engine/pkg/config"
	"github.com/njtc406/emberengine/v2/engine/pkg/def"
	"github.com/njtc406/viper"
	"github.com/stretchr/testify/require"
)

func TestParsedMailboxBackendWorkerBehavior(t *testing.T) {
	for _, mode := range []string{"dual", "priority"} {
		for _, backend := range []string{"", "unbounded", "bounded"} {
			t.Run(mode+"/"+backend, func(t *testing.T) {
				parser := viper.New()
				parser.SetConfigType("yaml")
				require.NoError(t, parser.ReadConfig(strings.NewReader(fmt.Sprintf(`Mailbox:
  QueueMode: %s
  QueueBackend: %q
  StopTimeout: 5s
  MaxQueueStorageBytes: 1048576
  DualQueueCapacity: {System: 2, User: 2}
  SchedulePolicy:
    InitialWorkerNum: 1
    MultiLevelQueueConf:
      Strategy: fairness
      TotalBatchLimit: 16
      PriorityBatches:
        "-3": {Capacity: 2, BatchSize: 1, Weight: 1}
        "-2": {Capacity: 2, BatchSize: 1, Weight: 1}
        "-1": {Capacity: 2, BatchSize: 1, Weight: 1}
        "0": {Capacity: 2, BatchSize: 1, Weight: 1}
`, mode, backend))))
				var decoded struct{ Mailbox *config.MailboxConf }
				require.NoError(t, parser.Unmarshal(&decoded))
				mb, _ := startLifecycleMailbox(t, decoded.Mailbox, DrainExecute)
				first, unblock := blockLifecycleConsumer(t, mb)
				jobs := []*lifecycleJob{first}
				for i := 0; i < 2; i++ {
					j := trackedLifecycleJob()
					jobs = append(jobs, j)
					require.NoError(t, mb.PostJob(j))
				}
				extra := trackedLifecycleJob()
				err := mb.PostJob(extra)
				if backend == "bounded" {
					require.ErrorIs(t, err, def.ErrMailboxWorkerIsFull)
					require.Equal(t, int64(1), extra.releases.Load())
					require.Zero(t, extra.executes.Load())
					system := trackedLifecycleJob()
					system.SetPriority(def.PrioritySys)
					require.NoError(t, mb.PostJob(system), "user saturation must not exhaust system capacity")
					jobs = append(jobs, system)
				} else {
					require.NoError(t, err)
					jobs = append(jobs, extra)
				}
				unblock()
				mb.Stop()
				for _, j := range jobs {
					require.Equal(t, int64(1), j.executes.Load())
					require.Equal(t, int64(1), j.releases.Load())
				}
			})
		}
	}
}
