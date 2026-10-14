package mailbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"sync/atomic"
	"testing"
	"time"

	"github.com/njtc406/emberengine/engine/internal/perftest"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

type perfJob struct {
	inf.IMailboxJob
	started  time.Time
	latency  time.Duration
	released *atomic.Int64
	executed atomic.Bool
}

func (j *perfJob) Release()               { j.IMailboxJob.Release(); j.released.Add(1) }
func (j *perfJob) GetRWMode() def.RWMode  { return j.IMailboxJob.(inf.IRWModeJob).GetRWMode() }
func (j *perfJob) SetRWMode(m def.RWMode) { j.IMailboxJob.(inf.IRWModeJob).SetRWMode(m) }

type perfInvoker struct {
	mockInvoker
	discarded atomic.Int64
	work      int
}

func (i *perfInvoker) ExecuteJob(_ context.Context, j inf.IMailboxJob) error {
	p := j.(*perfJob)
	// Fixed deterministic CPU work; observable state prevents dead-code elimination.
	x := uint64(1)
	for k := 0; k < i.work; k++ {
		x = x*6364136223846793005 + 1
	}
	atomic.AddInt64(&i.state, int64(x))
	p.latency = time.Since(p.started)
	p.executed.Store(true)
	return nil
}
func (i *perfInvoker) OnJobDiscarded(inf.IMailboxJob, error) { i.discarded.Add(1) }

type perfLogger struct{ testLogger }

func (*perfLogger) Debugf(string, ...interface{})   {}
func (*perfLogger) Infof(string, ...interface{})    {}
func (*perfLogger) Warnf(string, ...interface{})    {}
func (*perfLogger) Warningf(string, ...interface{}) {}
func (*perfLogger) Errorf(string, ...interface{})   {}

type mailboxPerfScenario struct {
	Name, Mode string
	Workers    int32
	RW, Hot    bool
	Kind       string
}

func mailboxPerfScenarios() []mailboxPerfScenario {
	var out []mailboxPerfScenario
	for _, mode := range []string{"dual", "priority"} {
		for _, workers := range []int32{1, 4} {
			for _, rw := range []bool{false, true} {
				if rw && workers == 1 {
					continue
				}
				for _, hot := range []bool{false, true} {
					out = append(out, mailboxPerfScenario{fmt.Sprintf("%s-w%d-rw%t-hot%t-steady", mode, workers, rw, hot), mode, workers, rw, hot, "steady"})
				}
			}
		}
	}
	for _, kind := range []string{"mostly-empty", "resize", "overload"} {
		out = append(out, mailboxPerfScenario{"dual-w4-rwfalse-hotfalse-" + kind, "dual", 4, false, false, kind})
	}
	return out
}

type mailboxPerfRow struct {
	Protocol                                                               string
	Machine                                                                perftest.Metadata
	Candidate, Scenario                                                    string
	Round                                                                  int
	Workers                                                                int32
	EffectiveRW                                                            bool
	Samples, Success, MainRejected, RWDropped, StopRejected, OtherRejected int
	Wall, CPU                                                              time.Duration
	Completion                                                             perftest.Quantiles
	RejectionRatio                                                         float64
	Drained                                                                bool
}

func TestMailboxPerformanceSample(t *testing.T) {
	if os.Getenv("MAILBOX_PERF_SAMPLE") != "1" {
		t.Skip("explicit experiment only")
	}
	candidate := os.Getenv("PERF_CANDIDATE")
	if candidate != "baseline" && candidate != "unbounded" && candidate != "bounded" {
		t.Fatal("invalid candidate")
	}
	var rows []mailboxPerfRow
	for _, s := range mailboxPerfScenarios() {
		t.Run(s.Name, func(t *testing.T) {
			conf := newSimpleConf(s.Workers)
			conf.QueueMode = s.Mode
			conf.EnableRWMode = s.RW
			conf.MaxConcurrentReads = 32
			// JSON keeps this exact fixture compilable against the explicit old commit.
			backend := "unbounded"
			if candidate == "bounded" {
				backend = "bounded"
			}
			patch := fmt.Sprintf(`{"QueueBackend":%q,"DualQueueCapacity":{"System":4096,"User":4096},"MaxQueueStorageBytes":16777216}`, backend)
			if err := json.Unmarshal([]byte(patch), conf); err != nil {
				t.Fatal(err)
			}
			// Priority's JSON keys are the actual def.Priority values, not guessed labels.
			batches := map[def.Priority]map[string]int{}
			for _, p := range []def.Priority{def.PrioritySys, def.PriorityUrgent, def.PriorityHigh, def.PriorityNormal, def.PriorityLow, def.PriorityBatch} {
				batches[p] = map[string]int{"Capacity": 4096, "BatchSize": 16, "Weight": 1}
			}
			data, err := json.Marshal(map[string]any{"SchedulePolicy": map[string]any{"MultiLevelQueueConf": map[string]any{"PriorityBatches": batches}}})
			if err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(data, conf); err != nil {
				t.Fatal(err)
			}
			inv := &perfInvoker{work: 64}
			if s.Kind == "overload" {
				inv.work = 16384
			}
			mb, err := NewMailbox(conf, &perfLogger{}, inv, nil)
			if err != nil {
				t.Fatal(err)
			}
			mb.Start()
			defer mb.Stop()
			n := 65536
			batch := 32
			tick := time.Millisecond
			if s.Kind == "mostly-empty" {
				n = 8192
				batch = 1
			}
			if s.Kind == "overload" {
				n = 65536
				batch = n
			}
			var released atomic.Int64
			makeJob := func(index int) *perfJob {
				key := fmt.Sprintf("key-%d", index%256)
				if s.Hot {
					key = "hot"
				}
				j := &perfJob{IMailboxJob: newMetricsTestJob(key), released: &released}
				if s.RW && index%4 != 0 {
					j.SetRWMode(def.RWModeRead)
				}
				return j
			}
			wait := func(want int64) {
				deadline := time.Now().Add(30 * time.Second)
				for released.Load() != want {
					if time.Now().After(deadline) {
						t.Fatalf("release timeout got=%d want=%d", released.Load(), want)
					}
					time.Sleep(50 * time.Microsecond)
				}
			}
			for i := 0; i < 256; i++ {
				j := makeJob(i)
				j.started = time.Now()
				if err := mb.PostJob(j); err != nil {
					t.Fatal(err)
				}
				wait(int64(i + 1))
			}
			jobs := make([]*perfJob, n)
			for i := range jobs {
				jobs[i] = makeJob(i)
			}
			row := mailboxPerfRow{Protocol: "task2-v2", Machine: perftest.Machine(), Candidate: candidate, Scenario: s.Name, Workers: s.Workers, EffectiveRW: mb.workerPool.rw.enabled.Load(), Samples: n}
			before, err := perftest.CPUTime()
			if err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			for i, j := range jobs {
				if i%batch == 0 {
					due := start.Add(time.Duration(i/batch) * tick)
					if delay := time.Until(due); delay > 0 {
						time.Sleep(delay)
					}
				}
				if s.Kind == "resize" && i == n/3 {
					mb.workerPool.resizeWorkers(6)
				}
				if s.Kind == "resize" && i == 2*n/3 {
					mb.workerPool.resizeWorkers(4)
				}
				j.started = time.Now()
				err := mb.PostJob(j)
				switch {
				case err == nil:
				case errors.Is(err, def.ErrMailboxWorkerIsFull):
					row.MainRejected++
				default:
					row.OtherRejected++
				}
			}
			wait(int64(n + 256))
			row.Wall = time.Since(start)
			after, err := perftest.CPUTime()
			if err != nil {
				t.Fatal(err)
			}
			row.CPU = after - before
			durations := make([]time.Duration, 0, n)
			for _, j := range jobs {
				if j.executed.Load() {
					durations = append(durations, j.latency)
				}
			}
			row.Success = len(durations)
			row.RWDropped = int(inv.discarded.Load()) - row.MainRejected - row.OtherRejected
			row.Drained = released.Load() == int64(n+256)
			row.Completion = perftest.Percentiles(durations)
			row.RejectionRatio = float64(n-row.Success) / float64(n)
			if row.Success+row.MainRejected+row.RWDropped+row.OtherRejected != n {
				t.Fatal("conservation failure")
			}
			if s.Kind != "overload" && row.Success != n {
				t.Errorf("non-overload loss: %+v", row)
			}
			rows = append(rows, row)
		})
	}
	data, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if path := os.Getenv("PERF_OUTPUT"); path != "" {
		if err = os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	} else {
		t.Log(string(data))
	}
}

func validateMailboxPerfBaseline(rows []mailboxPerfRow) error {
	if len(rows) == 0 {
		return errors.New("missing explicit 73ecd28 baseline")
	}
	counts := map[string]int{}
	for _, r := range rows {
		if r.Machine.Commit != "73ecd28" || r.Machine.CPU == "" || r.Machine.OS == "" || r.CPU <= 0 || r.Success == 0 {
			return errors.New("invalid baseline metadata or CPU resolution")
		}
		counts[r.Scenario]++
	}
	for _, s := range mailboxPerfScenarios() {
		if counts[s.Name] < 5 {
			return fmt.Errorf("missing five baseline rounds for %s", s.Name)
		}
	}
	return nil
}
func TestMailboxPerformanceGateRequiresBaseline(t *testing.T) {
	if err := validateMailboxPerfBaseline(nil); err == nil {
		t.Fatal("empty baseline accepted")
	}
}
func TestMailboxPerformanceGate(t *testing.T) {
	if os.Getenv("MAILBOX_PERF_GATE") != "1" {
		t.Skip("production gate opt-in")
	}
	data, err := os.ReadFile(os.Getenv("MAILBOX_PERF_BASELINE"))
	if err != nil {
		t.Fatalf("missing baseline: %v", err)
	}
	var rows []mailboxPerfRow
	if err = json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	if err = validateMailboxPerfBaseline(rows); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(os.Getenv("MAILBOX_PERF_CURRENT"))
	if err != nil {
		t.Fatalf("missing current samples: %v", err)
	}
	var current []mailboxPerfRow
	if err = json.Unmarshal(data, &current); err != nil {
		t.Fatal(err)
	}
	for _, failure := range compareMailboxPerf(rows, current) {
		t.Error(failure)
	}
	// A development sample cannot certify deployment hardware or overload memory stability.
	t.Fatal("NOT APPROVED: target-machine attestation and bounded overload memory plateau evidence are required; see tools/mailboxperf/results comparisons")
}
