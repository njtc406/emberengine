package mailbox

import (
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/njtc406/emberengine/v2/engine/internal/perftest"
	"github.com/njtc406/emberengine/v2/engine/pkg/def"
)

// Diagnostic controls are deliberately separate from the unchanged acceptance protocol.
func TestMailboxRWBacklogDiagnostic(t *testing.T) {
	if os.Getenv("MAILBOX_RW_DIAGNOSTIC") != "1" {
		t.Skip("explicit diagnostic only")
	}
	for _, c := range []struct {
		name, backend string
		rw, catchup   bool
		batch         int
	}{
		{"original", "bounded", true, true, 32},
		{"no-catchup", "bounded", true, false, 32},
		{"8-per-ms", "bounded", true, true, 8},
		{"rw-off", "bounded", false, true, 32},
		{"unbounded", "unbounded", true, true, 32},
	} {
		t.Run(c.name, func(t *testing.T) {
			conf := newSimpleConf(4)
			conf.EnableRWMode, conf.MaxConcurrentReads = c.rw, 32
			if err := json.Unmarshal([]byte(fmt.Sprintf(`{"QueueBackend":%q,"DualQueueCapacity":{"System":4096,"User":4096},"MaxQueueStorageBytes":16777216}`, c.backend)), conf); err != nil {
				t.Fatal(err)
			}
			inv := &perfInvoker{work: 64}
			mb, err := NewMailbox(conf, &perfLogger{}, inv, nil)
			if err != nil {
				t.Fatal(err)
			}
			mb.Start()
			defer mb.Stop()
			const n = 65536
			var released atomic.Int64
			jobs := make([]*perfJob, n)
			for i := range jobs {
				j := &perfJob{IMailboxJob: newMetricsTestJob("hot"), released: &released}
				if c.rw && i%4 != 0 {
					j.SetRWMode(def.RWModeRead)
				}
				jobs[i] = j
			}
			type point struct {
				Index, Queue, ReadQueue int
				Released                int64
				Dispatched, Launched    uint64
				ActualNS, LateNS        int64
			}
			points := make([]point, 0, n/c.batch)
			start := time.Now()
			due := start
			rejected := 0
			for i, j := range jobs {
				if i%c.batch == 0 {
					if c.catchup {
						due = start.Add(time.Duration(i/c.batch) * time.Millisecond)
					}
					if d := time.Until(due); d > 0 {
						time.Sleep(d)
					}
					now := time.Now()
					p := point{Index: i, ActualNS: now.Sub(start).Nanoseconds(), LateNS: now.Sub(due).Nanoseconds(), Released: released.Load()}
					for _, w := range mb.workerPool.snap.Load().workers {
						worker := w.(*Worker)
						p.Queue += worker.GetJobLen()
						p.ReadQueue += len(worker.readCh)
						p.Dispatched += worker.readsDispatched.Load()
						p.Launched += worker.readsLaunched.Load()
					}
					points = append(points, p)
					if !c.catchup {
						due = now.Add(time.Millisecond)
					}
				}
				j.started = perftest.Now()
				if err := mb.PostJob(j); err != nil {
					if err != def.ErrMailboxWorkerIsFull {
						t.Fatal(err)
					}
					rejected++
				}
			}
			offered := time.Since(start)
			atEnd := released.Load()
			deadline := time.Now().Add(15 * time.Second)
			for released.Load() != n && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if released.Load() != n {
				t.Fatalf("drain: %d/%d", released.Load(), n)
			}
			wall := time.Since(start)
			latencies := make([]time.Duration, 0, n)
			for _, j := range jobs {
				if j.executed.Load() {
					latencies = append(latencies, j.latency)
				}
			}
			drops := int(inv.discarded.Load()) - rejected
			if len(latencies)+rejected+drops != n || drops != 0 {
				t.Fatalf("conservation/drop: %d %d %d", len(latencies), rejected, drops)
			}
			result := struct {
				Name                       string
				Offered, Wall              time.Duration
				ReleasedAtEnd              int64
				Success, Rejected, Dropped int
				Completion                 perftest.Quantiles
				Points                     []point
			}{c.name, offered, wall, atEnd, len(latencies), rejected, drops, perftest.Percentiles(latencies), points}
			data, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if prefix := os.Getenv("RW_DIAGNOSTIC_OUTPUT"); prefix != "" {
				if err := os.WriteFile(prefix+"-"+c.name+".json", data, 0644); err != nil {
					t.Fatal(err)
				}
			}
			t.Logf("%s offered=%v wall=%v releasedAtEnd=%d success=%d rejected=%d dropped=%d p99=%v", c.name, offered, wall, atEnd, len(latencies), rejected, drops, result.Completion.P99)
		})
	}
}
