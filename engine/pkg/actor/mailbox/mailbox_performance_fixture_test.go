package mailbox

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/def"
)

// The wrapper belongs to the fixture; each underlying pooled Job is acquired
// afresh and is never accessed after Release. No Event payload is set by v4.
// The release counter is published after the underlying Job's pool Put.
type mailboxPerfWindow struct {
	jobs     []*perfJob
	released *atomic.Int64
	want     int64
	active   bool
}

func newMailboxPerfWindow(n int, released *atomic.Int64) *mailboxPerfWindow {
	f := &mailboxPerfWindow{jobs: make([]*perfJob, n), released: released}
	for i := range f.jobs {
		f.jobs[i] = &perfJob{released: released}
	}
	return f
}

func (f *mailboxPerfWindow) prepare(s mailboxPerfScenario) error {
	if f.active {
		return fmt.Errorf("previous window not collected")
	}
	f.want = f.released.Load() + int64(len(f.jobs))
	for i, j := range f.jobs {
		key := fmt.Sprintf("key-%d", i%256)
		if s.Hot {
			key = "hot"
		}
		j.IMailboxJob = newMetricsTestJob(key)
		j.started, j.latency = 0, 0
		j.executed.Store(false)
		if s.RW && i%4 != 0 {
			j.SetRWMode(def.RWModeRead)
		}
	}
	f.active = true
	return nil
}

func (f *mailboxPerfWindow) collect(dst []time.Duration) ([]time.Duration, error) {
	if !f.active || f.released.Load() != f.want {
		return dst, fmt.Errorf("window not released or already collected")
	}
	for _, j := range f.jobs {
		if j.executed.Load() {
			dst = append(dst, j.latency)
		}
		j.IMailboxJob = nil
	}
	f.active = false
	return dst, nil
}
