package mailbox

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/njtc406/emberengine/v2/engine/pkg/def"
	inf "github.com/njtc406/emberengine/v2/engine/pkg/interfaces"
)

type wakeupJob struct {
	inf.IMailboxJob
	run      func()
	releases atomic.Int32
}

func (j *wakeupJob) Release() { j.releases.Add(1) }

type wakeupInvoker struct{ perfInvoker }

func (*wakeupInvoker) ExecuteJob(_ context.Context, j inf.IMailboxJob) error {
	j.(*wakeupJob).run()
	return nil
}

func wakeupAwait(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("state transition timed out")
		}
		runtime.Gosched()
	}
}

// No sleeps establish ordering: a read is held before the pipeline starts,
// then both registered reads are held while the dequeued writer waits.
func TestRWWakeupRegistrationAndExclusion(t *testing.T) {
	conf := newRWConf(4, 1)
	conf.MaxJobExecutionTime = 0
	mb, err := NewMailbox(conf, &perfLogger{}, &wakeupInvoker{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	p := mb.workerPool
	w := newWorker(1, p.conf, p.queueSpec, p.workerEnv(), p.drainPolicy).(*Worker)
	w.readCh = make(chan inf.IMailboxJob, 2)
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var readers, writes, bad atomic.Int32
	jobs := make([]*wakeupJob, 3)
	for i := range jobs {
		mode := def.RWModeRead
		if i == 2 {
			mode = def.RWModeWrite
		}
		j := &wakeupJob{IMailboxJob: newRWJob(mode, "hot")}
		defer j.IMailboxJob.Release()
		if i < 2 {
			j.run = func() {
				readers.Add(1)
				if writes.Load() != 0 {
					bad.Add(1)
				}
				entered <- struct{}{}
				<-release
				readers.Add(-1)
			}
		} else {
			j.run = func() {
				if readers.Load() != 0 {
					bad.Add(1)
				}
				writes.Add(1)
			}
		}
		jobs[i] = j
	}
	// Use actual dispatch, read pipeline, write implementation and Release path,
	// but control startup to deterministically hold the registration boundary.
	w.dispatchRead(jobs[0])
	w.dispatchRead(jobs[1])
	done := make(chan struct{})
	go func() { w.execWrite(jobs[2]); close(done) }()
	if w.readsLaunched.Load() != 0 || writes.Load() != 0 {
		t.Fatal("writer bypassed registration")
	}
	w.readPipelineWg.Add(1)
	go w.runReadPipeline()
	wakeupAwait(t, func() bool { return len(entered) == 1 })
	if w.readsLaunched.Load() != 1 || writes.Load() != 0 {
		t.Fatal("writer bypassed predecessor")
	}
	release <- struct{}{}
	wakeupAwait(t, func() bool { return len(entered) == 2 })
	if writes.Load() != 0 {
		t.Fatal("writer overlapped second read")
	}
	close(release)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("lost progress")
	}
	close(w.readCh)
	w.readPipelineWg.Wait()
	w.inflightReads.Wait()
	if bad.Load() != 0 || writes.Load() != 1 {
		t.Fatalf("bad=%d writes=%d", bad.Load(), writes.Load())
	}
	for _, j := range jobs {
		if j.releases.Load() != 1 {
			t.Fatal("release not exactly once")
		}
	}
}

func TestRWWakeupLifecycle(t *testing.T) {
	for _, action := range []string{"stop", "disable"} {
		t.Run(action, func(t *testing.T) {
			conf := newRWConf(4, 2)
			conf.MaxJobExecutionTime = 0
			mb, err := NewMailbox(conf, &perfLogger{}, &wakeupInvoker{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			mb.Start()
			defer mb.Stop()
			gate := make(chan struct{})
			var entered, executed, active, bad atomic.Int32
			jobs := make([]*wakeupJob, 128)
			for i := range jobs {
				mode := def.RWModeRead
				if i%4 == 3 {
					mode = def.RWModeWrite
				}
				read := mode == def.RWModeRead
				j := &wakeupJob{IMailboxJob: newRWJob(mode, "hot")}
				defer j.IMailboxJob.Release()
				j.run = func() {
					if read {
						if active.Add(1) <= 0 {
							bad.Add(1)
						}
						entered.Add(1)
						<-gate
						if active.Add(-1) < 0 {
							bad.Add(1)
						}
					} else {
						if !active.CompareAndSwap(0, -10000) {
							bad.Add(1)
						}
						if !active.CompareAndSwap(-10000, 0) {
							bad.Add(1)
						}
					}
					executed.Add(1)
				}
				jobs[i] = j
				if err := mb.PostJob(j); err != nil {
					t.Fatal(err)
				}
			}
			wakeupAwait(t, func() bool { return entered.Load() > 0 })
			done := make(chan error, 1)
			if action == "stop" {
				mb.workerPool.BeginStop()
				go func() { mb.workerPool.Wait(); done <- nil }()
			} else {
				go func() { done <- mb.workerPool.SetRWEnabled(false) }()
				wakeupAwait(t, func() bool { return mb.workerPool.rw.IsDisabling() })
			}
			close(gate)
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(6 * time.Second):
				t.Fatal("lifecycle stalled")
			}
			mb.Stop()
			if bad.Load() != 0 || executed.Load() != 128 {
				t.Fatalf("bad=%d executed=%d", bad.Load(), executed.Load())
			}
			for _, j := range jobs {
				if j.releases.Load() != 1 {
					t.Fatal("release not exactly once")
				}
			}
		})
	}
}
