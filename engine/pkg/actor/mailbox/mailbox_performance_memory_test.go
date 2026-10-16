package mailbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/njtc406/emberengine/v2/engine/pkg/def"
	inf "github.com/njtc406/emberengine/v2/engine/pkg/interfaces"
)

type perfMemoryState struct {
	accepted                      bool
	executed, discarded, released atomic.Int64
}

func validatePerfMemoryStates(states []perfMemoryState) error {
	for id := range states {
		s := &states[id]
		wantExecuted, wantDiscarded := int64(0), int64(1)
		if s.accepted {
			wantExecuted, wantDiscarded = 1, 0
		}
		if s.executed.Load() != wantExecuted || s.discarded.Load() != wantDiscarded || s.released.Load() != 1 {
			return fmt.Errorf("submission %d accepted=%t executed=%d discarded=%d released=%d", id, s.accepted, s.executed.Load(), s.discarded.Load(), s.released.Load())
		}
	}
	return nil
}

// State contains only counters, never job/payload references. Allocate all IDs
// before the first heap point so accounting storage is constant across windows.
type perfMemoryJob struct {
	inf.IMailboxJob
	state    *perfMemoryState
	released *atomic.Int64
}

func (j *perfMemoryJob) Release() {
	j.IMailboxJob.Release()
	j.state.released.Add(1)
	j.released.Add(1)
}
func (j *perfMemoryJob) GetRWMode() def.RWMode  { return j.IMailboxJob.(inf.IRWModeJob).GetRWMode() }
func (j *perfMemoryJob) SetRWMode(m def.RWMode) { j.IMailboxJob.(inf.IRWModeJob).SetRWMode(m) }

type perfMemoryInvoker struct {
	mockInvoker
	entered   chan struct{}
	resume    chan struct{}
	once      sync.Once
	executed  atomic.Int64
	discarded atomic.Int64
}

func (i *perfMemoryInvoker) ExecuteJob(_ context.Context, job inf.IMailboxJob) error {
	i.once.Do(func() { close(i.entered) })
	<-i.resume
	job.(*perfMemoryJob).state.executed.Add(1)
	i.executed.Add(1)
	return nil
}
func (i *perfMemoryInvoker) OnJobDiscarded(job inf.IMailboxJob, _ error) {
	job.(*perfMemoryJob).state.discarded.Add(1)
	i.discarded.Add(1)
}

// A blocked real worker provides reproducible increasing offered backlog. This is
// retained Go heap evidence, not RSS or an application deployment attestation.
func TestMailboxPerformanceMemory(t *testing.T) {
	if os.Getenv("MAILBOX_PERF_MEMORY") != "1" {
		t.Skip("explicit memory experiment")
	}
	candidate := os.Getenv("PERF_CANDIDATE")
	if candidate != "baseline" && candidate != "unbounded" && candidate != "bounded" {
		t.Fatal("invalid candidate")
	}
	conf := newSimpleConf(1)
	conf.EnableRWMode = false
	backend := "unbounded"
	if candidate == "bounded" {
		backend = "bounded"
	}
	if err := json.Unmarshal([]byte(`{"QueueBackend":"`+backend+`","DualQueueCapacity":{"System":4096,"User":4096},"MaxQueueStorageBytes":16777216}`), conf); err != nil {
		t.Fatal(err)
	}
	inv := &perfMemoryInvoker{entered: make(chan struct{}), resume: make(chan struct{})}
	mb, err := NewMailbox(conf, &perfLogger{}, inv, nil)
	if err != nil {
		t.Fatal(err)
	}
	mb.Start()
	var unblock sync.Once
	defer mb.Stop()
	defer unblock.Do(func() { close(inv.resume) })
	var released atomic.Int64
	states := make([]perfMemoryState, 32769)
	nextID := 0
	post := func() error {
		state := &states[nextID]
		nextID++
		err := mb.PostJob(&perfMemoryJob{IMailboxJob: newMetricsTestJob("hot"), state: state, released: &released})
		state.accepted = err == nil
		return err
	}
	if err := post(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-inv.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not enter")
	}
	type point struct {
		Offered, Accepted, Rejected, Occupancy int
		HeapAlloc, HeapObjects                 uint64
	}
	points := []point{}
	accepted, rejected := 1, 0
	for window := 0; window <= 8; window++ {
		if window > 0 {
			for j := 0; j < 4096; j++ {
				err := post()
				switch {
				case err == nil:
					accepted++
				case errors.Is(err, def.ErrMailboxWorkerIsFull):
					rejected++
				default:
					t.Fatal(err)
				}
			}
		}
		occupancy := 0
		for _, w := range mb.workerPool.snap.Load().workers {
			occupancy += w.GetJobLen()
		}
		runtime.GC()
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		points = append(points, point{1 + window*4096, accepted, rejected, occupancy, mem.HeapAlloc, mem.HeapObjects})
		if occupancy != accepted-1 {
			t.Fatalf("occupancy=%d accepted=%d", occupancy, accepted)
		}
		if candidate == "bounded" && occupancy > 4096 {
			t.Fatal("capacity exceeded")
		}
	}
	unblock.Do(func() { close(inv.resume) })
	deadline := time.Now().Add(30 * time.Second)
	for released.Load() != 32769 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	mb.Stop()
	if err := validatePerfMemoryStates(states); err != nil {
		t.Fatal(err)
	}
	if released.Load() != 32769 || inv.executed.Load() != int64(accepted) || inv.discarded.Load() != int64(rejected) {
		t.Fatalf("conservation/recovery failed released=%d executed=%d discarded=%d", released.Load(), inv.executed.Load(), inv.discarded.Load())
	}
	if candidate == "bounded" && (accepted != 4097 || rejected != 28672) {
		t.Fatalf("bounded totals %d/%d", accepted, rejected)
	}
	data, err := json.MarshalIndent(points, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if path := os.Getenv("PERF_MEMORY_OUTPUT"); path != "" {
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("candidate=%s recovered=true executed=%d rejected=%d points=%s", candidate, accepted, rejected, data)
}
