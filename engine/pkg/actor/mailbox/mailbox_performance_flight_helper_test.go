package mailbox

import (
	"encoding/json"
	"os"
	"runtime/metrics"
	"time"
)

// Single producer owns this ring. Release progress is sampled atomically from
// the existing harness; no worker/queue production instrumentation is added.
type mailboxFlightSample struct {
	Window, Index                                 int
	ScheduledNS, ActualNS, ReturnedNS             int64
	OccupancyBefore, OccupancyAfter               int
	CompletedBefore, CompletedAfter               int64
	GCCycles, GCPauses, SchedEvents, SchedOver1ms uint64
	Rejected                                      bool
}
type mailboxFlight struct {
	samples                 []mailboxFlightSample
	next, count             int
	frozen                  bool
	runtime                 []metrics.Sample
	gc, pauses, sched, slow uint64
}

func newMailboxFlight(size int) *mailboxFlight {
	return &mailboxFlight{samples: make([]mailboxFlightSample, size), runtime: []metrics.Sample{{Name: "/gc/cycles/total:gc-cycles"}, {Name: "/sched/pauses/total/gc:seconds"}, {Name: "/sched/latencies:seconds"}}}
}
func (f *mailboxFlight) record(s mailboxFlightSample, reject bool) {
	if f.frozen {
		return
	}
	s.Rejected = reject
	f.samples[f.next] = s
	f.next = (f.next + 1) % len(f.samples)
	if f.count < len(f.samples) {
		f.count++
	}
	f.frozen = reject
}
func (f *mailboxFlight) ordered() []mailboxFlightSample {
	out := make([]mailboxFlightSample, f.count)
	for i := range out {
		out[i] = f.samples[(f.next-f.count+i+len(f.samples))%len(f.samples)]
	}
	return out
}
func (f *mailboxFlight) readRuntime() {
	metrics.Read(f.runtime)
	f.gc = f.runtime[0].Value.Uint64()
	f.pauses = 0
	for _, v := range f.runtime[1].Value.Float64Histogram().Counts {
		f.pauses += v
	}
	h := f.runtime[2].Value.Float64Histogram()
	f.sched = 0
	f.slow = 0
	for i, v := range h.Counts {
		f.sched += v
		if h.Buckets[i] >= 0.001 {
			f.slow += v
		}
	}
}
func (f *mailboxFlight) write(path string) error {
	data, err := json.Marshal(struct {
		Warning    string
		Frozen     bool
		CapturedAt time.Time
		Samples    []mailboxFlightSample
	}{"DIAGNOSTIC ONLY: per-publication clock/queue locks and per-batch runtime metrics perturb timing. Occupancy/progress observations are non-transactional. Completed = releases minus rejection returns; runtime counters are process-wide, refreshed per batch. No acceptance or root-cause attribution from correlation alone.", f.frozen, time.Now().UTC(), f.ordered()})
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
