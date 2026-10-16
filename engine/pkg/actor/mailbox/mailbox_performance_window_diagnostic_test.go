package mailbox

import (
	"encoding/json"
	"os"
	"runtime"
	"runtime/metrics"
	"sync/atomic"
	"testing"
	"time"

	"github.com/njtc406/emberengine/v2/engine/internal/perftest"
)

func TestMailboxPerfWindowHeapDiagnostic(t *testing.T) {
	if os.Getenv("MAILBOX_PERF_HEAP") != "1" {
		t.Skip("explicit heap diagnostic only")
	}
	runtime.GC()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	var released atomic.Int64
	f := newMailboxPerfWindow(65536, &released)
	// Exact message-level samples remain pointer-free; job retention is bounded.
	durations := make([]time.Duration, 0, 65536*32)
	for window := 0; window < 32; window++ {
		if err := f.prepare(mailboxPerfScenario{Hot: true}); err != nil {
			t.Fatal(err)
		}
		for _, j := range f.jobs {
			j.executed.Store(true)
			j.latency = time.Nanosecond
			j.Release()
		}
		var err error
		durations, err = f.collect(durations)
		if err != nil {
			t.Fatal(err)
		}
		if window == 0 || window == 3 || window == 31 {
			start := time.Now()
			runtime.GC()
			gcWall := time.Since(start)
			runtime.ReadMemStats(&after)
			t.Logf("v5 windows=%d wrappers=%d samples=%d retainedBytes=%d allocatedBytes=%d objects=%d GC=%d pauseTotal=%s forcedGCWall=%s", window+1, len(f.jobs), len(durations), after.HeapAlloc-before.HeapAlloc, after.TotalAlloc-before.TotalAlloc, after.HeapObjects-before.HeapObjects, after.NumGC-before.NumGC, time.Duration(after.PauseTotalNs-before.PauseTotalNs), gcWall)
		}
	}
	sample := []metrics.Sample{{Name: "/sched/pauses/total/gc:seconds"}, {Name: "/cpu/classes/gc/total:cpu-seconds"}, {Name: "/gc/scan/heap:bytes"}}
	metrics.Read(sample)
	for _, s := range sample {
		switch s.Value.Kind() {
		case metrics.KindFloat64:
			t.Logf("%s=%g", s.Name, s.Value.Float64())
		case metrics.KindUint64:
			t.Logf("%s=%d", s.Name, s.Value.Uint64())
		case metrics.KindFloat64Histogram:
			h := s.Value.Float64Histogram()
			for i, c := range h.Counts {
				if c != 0 {
					t.Logf("%s [%g,%g) count=%d", s.Name, h.Buckets[i], h.Buckets[i+1], c)
				}
			}
		}
	}
	runtime.KeepAlive(f)
	runtime.KeepAlive(durations)
}

func TestMailboxPerfWindowMailboxOwnership(t *testing.T) {
	for _, backend := range []string{"unbounded", "bounded"} {
		t.Run(backend, func(t *testing.T) {
			conf := newSimpleConf(4)
			if err := json.Unmarshal([]byte(`{"QueueBackend":"`+backend+`","DualQueueCapacity":{"System":4096,"User":4096},"MaxQueueStorageBytes":16777216}`), conf); err != nil {
				t.Fatal(err)
			}
			inv := &perfInvoker{work: 64}
			mb, err := NewMailbox(conf, &perfLogger{}, inv, nil)
			if err != nil {
				t.Fatal(err)
			}
			mb.Start()
			defer mb.Stop()
			var released atomic.Int64
			f := newMailboxPerfWindow(128, &released)
			var samples []time.Duration
			for window := 0; window < 32; window++ {
				if err := f.prepare(mailboxPerfScenario{Hot: true}); err != nil {
					t.Fatal(err)
				}
				for _, j := range f.jobs {
					j.started = perftest.Now()
					if err := mb.PostJob(j); err != nil {
						t.Fatal(err)
					}
				}
				deadline := time.Now().Add(5 * time.Second)
				for released.Load() != int64((window+1)*128) {
					if time.Now().After(deadline) {
						t.Fatal("release timeout")
					}
					runtime.Gosched()
				}
				samples, err = f.collect(samples)
				if err != nil {
					t.Fatal(err)
				}
			}
			if len(samples) != 4096 {
				t.Fatalf("samples=%d", len(samples))
			}
		})
	}
}
