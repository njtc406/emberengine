package mailbox

import (
	"fmt"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// This opt-in diagnostic retains exactly the v4 object graph, without scheduling
// noise or an attempt to reproduce capacity rejection.
func TestMailboxPerfRetainedHeapDiagnostic(t *testing.T) {
	if os.Getenv("MAILBOX_PERF_HEAP") != "1" {
		t.Skip("explicit heap diagnostic only")
	}
	for _, windows := range []int{1, 4, 32} {
		runtime.GC()
		runtime.GC()
		var before, allocated, retained runtime.MemStats
		runtime.ReadMemStats(&before)
		var released atomic.Int64
		jobs := make([]*perfJob, 65536*windows)
		for i := range jobs {
			key := fmt.Sprintf("key-%d", i%256)
			key = "hot"
			jobs[i] = &perfJob{IMailboxJob: newMetricsTestJob(key), released: &released}
		}
		runtime.ReadMemStats(&allocated)
		start := time.Now()
		runtime.GC()
		gcWall := time.Since(start)
		runtime.ReadMemStats(&retained)
		runtime.KeepAlive(jobs)
		var maxPause uint64
		for gc := before.NumGC; gc < retained.NumGC; gc++ {
			if p := retained.PauseNs[gc%256]; p > maxPause {
				maxPause = p
			}
		}
		t.Logf("v4 windows=%d jobs=%d retainedBytes=%d allocatedBytes=%d objects=%d GC=%d pauseTotal=%s pauseMax=%s forcedGCWall=%s", windows, len(jobs), retained.HeapAlloc-before.HeapAlloc, allocated.TotalAlloc-before.TotalAlloc, retained.HeapObjects-before.HeapObjects, retained.NumGC-before.NumGC, time.Duration(retained.PauseTotalNs-before.PauseTotalNs), time.Duration(maxPause), gcWall)
		for _, j := range jobs {
			j.Release()
		}
	}
}

func TestMailboxPerfWindowProtocol(t *testing.T) {
	windows, err := mailboxPerfReplication("task2-v5-window")
	if err != nil || windows != 32 {
		t.Fatalf("bounded-window protocol unavailable: windows=%d err=%v", windows, err)
	}
}
