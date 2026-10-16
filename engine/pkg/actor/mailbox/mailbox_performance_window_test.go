package mailbox

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/njtc406/emberengine/v2/engine/internal/perftest"
	"github.com/njtc406/emberengine/v2/engine/pkg/def"
	inf "github.com/njtc406/emberengine/v2/engine/pkg/interfaces"
)

func TestMailboxPerfWindowOwnership(t *testing.T) {
	var released atomic.Int64
	f := newMailboxPerfWindow(8, &released)
	var pooled []time.Duration
	for window := 0; window < 4; window++ {
		if err := f.prepare(mailboxPerfScenario{RW: true}); err != nil {
			t.Fatal(err)
		}
		seen := map[inf.IMailboxJob]bool{}
		for i, j := range f.jobs {
			if seen[j.IMailboxJob] {
				t.Fatal("shared live payload")
			}
			seen[j.IMailboxJob] = true
			want := def.RWModeWrite
			if i%4 != 0 {
				want = def.RWModeRead
			}
			if j.GetRWMode() != want || j.executed.Load() || j.latency != 0 {
				t.Fatal("dirty window")
			}
		}
		if err := f.prepare(mailboxPerfScenario{}); err == nil {
			t.Fatal("reused unreleased jobs")
		}
		for i, j := range f.jobs {
			if window == 0 || i == 0 {
				j.executed.Store(true)
				j.latency = time.Duration(1 + window*100)
			}
			j.Release()
		}
		var err error
		pooled, err = f.collect(pooled)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.collect(pooled); err == nil {
			t.Fatal("double collection")
		}
	}
	if len(f.jobs) != 8 || len(pooled) != 11 || released.Load() != 32 {
		t.Fatal("window bounds or conservation")
	}
	if got := perftest.Percentiles(pooled); got != perftest.Percentiles([]time.Duration{1, 1, 1, 1, 1, 1, 1, 1, 101, 201, 301}) {
		t.Fatal("not pooled message percentiles")
	}
}
