package mailbox

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/boundedmpsc"
	"github.com/stretchr/testify/require"
)

type lifecycleJob struct {
	inf.IMailboxJob
	mu       sync.Mutex
	events   []string
	releases atomic.Int64
	executes atomic.Int64
	entered  chan struct{}
	resume   <-chan struct{}
}

func (j *lifecycleJob) SetRWMode(mode def.RWMode) { j.IMailboxJob.(inf.IRWModeJob).SetRWMode(mode) }
func (j *lifecycleJob) GetRWMode() def.RWMode     { return j.IMailboxJob.(inf.IRWModeJob).GetRWMode() }

func (j *lifecycleJob) record(event string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.events = append(j.events, event)
}
func (j *lifecycleJob) Release() {
	j.record("release")
	if j.releases.Add(1) == 1 {
		j.IMailboxJob.Release()
	}
}
func (j *lifecycleJob) history() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]string(nil), j.events...)
}
func trackedLifecycleJob() *lifecycleJob { return &lifecycleJob{IMailboxJob: newJob()} }

type lifecycleInvoker struct{ mockInvoker }

func (*lifecycleInvoker) ExecuteJob(_ context.Context, job inf.IMailboxJob) error {
	j := job.(*lifecycleJob)
	j.executes.Add(1)
	j.record("execute")
	if j.entered != nil {
		close(j.entered)
		<-j.resume
	}
	return nil
}
func (*lifecycleInvoker) OnJobDiscarded(job inf.IMailboxJob, _ error) {
	job.(*lifecycleJob).record("discard")
}

type lifecycleMiddleware struct{}

func (*lifecycleMiddleware) Name() string { return "lifecycle" }
func (*lifecycleMiddleware) OnStart()     {}
func (*lifecycleMiddleware) OnStop()      {}
func (*lifecycleMiddleware) OnReceive(inf.IMiddlewareContext) dto.MiddlewareResult {
	return dto.Continue()
}
func (*lifecycleMiddleware) OnComplete(ctx inf.IMiddlewareContext, _ error, _ interface{}) {
	ctx.Job().(*lifecycleJob).record("complete")
}
func (*lifecycleMiddleware) OnFrameworkCleanup(ctx inf.IMiddlewareContext, _ error, _ interface{}) {
	ctx.Job().(*lifecycleJob).record("cleanup")
}

func lifecycleConf(backend string) *config.MailboxConf {
	c := newSimpleConf(1)
	c.QueueBackend = backend
	if backend == "bounded" {
		c.DualQueueCapacity = &config.DualQueueCapacityConf{System: 2, User: 2}
		c.MaxQueueStorageBytes = 1 << 20
	}
	return c
}
func startLifecycleMailbox(t *testing.T, c *config.MailboxConf, policy DrainPolicy) (*Mailbox, *Worker) {
	t.Helper()
	mb, err := NewMailbox(c, &testLogger{t: t}, &lifecycleInvoker{}, []inf.IMailboxMiddleware{&lifecycleMiddleware{}}, WithDrainPolicy(policy))
	require.NoError(t, err)
	mb.workerPool.statsEnabled = true
	mb.Start()
	t.Cleanup(mb.Stop)
	for _, w := range mb.workerPool.snap.Load().workers {
		return mb, w.(*Worker)
	}
	panic("started mailbox has no workers")
}
func blockLifecycleConsumer(t *testing.T, mb *Mailbox) (*lifecycleJob, func()) {
	t.Helper()
	resume := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(resume) }) }
	t.Cleanup(unblock)
	j := trackedLifecycleJob()
	j.entered, j.resume = make(chan struct{}), resume
	require.NoError(t, mb.PostJob(j))
	awaitAdmission(t, j.entered)
	return j, unblock
}

func TestPostJobBoundedFullOwnership(t *testing.T) {
	mb, w := startLifecycleMailbox(t, lifecycleConf("bounded"), DrainExecute)
	first, unblock := blockLifecycleConsumer(t, mb)
	second, third, rejected := trackedLifecycleJob(), trackedLifecycleJob(), trackedLifecycleJob()
	require.NoError(t, mb.PostJob(second))
	require.NoError(t, mb.PostJob(third))
	require.Equal(t, int64(3), w.count.Load())
	require.ErrorIs(t, mb.PostJob(rejected), def.ErrMailboxWorkerIsFull)
	require.Equal(t, []string{"discard", "complete", "release"}, rejected.history())
	require.Equal(t, int64(1), rejected.releases.Load())
	require.Zero(t, rejected.executes.Load())
	require.Equal(t, int64(3), w.count.Load())
	require.Zero(t, w.env.rw.drainDiscardTotal.Load())
	unblock()
	mb.Stop()
	for _, j := range []*lifecycleJob{first, second, third} {
		require.Equal(t, []string{"execute", "complete", "release"}, j.history())
	}
	require.Zero(t, w.GetJobLen())
}

func TestMailboxBoundedCondWake(t *testing.T) {
	mb, _ := startLifecycleMailbox(t, lifecycleConf("bounded"), DrainExecute)
	// Observe the actual parked worker rather than assuming a sleep reached Cond.Wait.
	waitUntil(t, func() bool {
		buf := make([]byte, 1<<20)
		stacks := string(buf[:runtime.Stack(buf, true)])
		for _, stack := range strings.Split(stacks, "\n\n") {
			if strings.Contains(stack, "sync.(*Cond).Wait") && strings.Contains(stack, "idle.(*AdaptiveController).Idle") && strings.Contains(stack, "mailbox.(*Worker).run") {
				return true
			}
		}
		return false
	}, 3*time.Second, "worker parked in Cond.Wait")
	j := trackedLifecycleJob()
	require.NoError(t, mb.PostJob(j))
	waitUntil(t, func() bool { return j.releases.Load() == 1 }, time.Second, "single submission wakes worker")
	mb.Stop()
	require.Equal(t, []string{"execute", "complete", "release"}, j.history())
}

func TestMailboxBoundedFactoryIsolation(t *testing.T) {
	for _, conf := range []*config.MailboxConf{lifecycleConf("bounded"), priorityQueueConfig()} {
		m := boundedManager(t, conf)
		switch q := m.(type) {
		case *DualQueueManager:
			require.IsType(t, (*boundedmpsc.Queue[inf.IMailboxJob])(nil), q.userMailbox)
			require.IsType(t, (*boundedmpsc.Queue[inf.IMailboxJob])(nil), q.systemMailbox)
		case *PriorityQueueManager:
			for _, physical := range q.queues {
				require.IsType(t, (*boundedmpsc.Queue[inf.IMailboxJob])(nil), physical)
			}
		default:
			t.Fatalf("unexpected manager %T", m)
		}
		conf.QueueBackend = "sequence"
		_, err := NewMailbox(conf, &testLogger{t: t}, &lifecycleInvoker{}, nil)
		require.Error(t, err)
	}
}

func TestMailboxSequenceReservationStopDrain(t *testing.T) {
	t.Skip("SequenceRing is not a production backend: future producer reservation paused beyond StopTimeout must reject publication as closed or Stop must wait for reservation completion; ownership assertions must not be relaxed")
}

func TestMailboxBoundedUnboundedStopDrainOwnership(t *testing.T) {
	for _, backend := range []string{"bounded", "unbounded"} {
		for _, policy := range []DrainPolicy{DrainExecute, DrainDiscard} {
			t.Run(fmt.Sprintf("%s/%d", backend, policy), func(t *testing.T) {
				mb, w := startLifecycleMailbox(t, lifecycleConf(backend), policy)
				first, unblock := blockLifecycleConsumer(t, mb)
				jobs := []*lifecycleJob{first, trackedLifecycleJob(), trackedLifecycleJob()}
				require.NoError(t, mb.PostJob(jobs[1]))
				require.NoError(t, mb.PostJob(jobs[2]))
				const n = 64
				results := make([]error, n)
				for i := 0; i < n; i++ {
					jobs = append(jobs, trackedLifecycleJob())
				}
				start, stopped := make(chan struct{}), make(chan struct{})
				var producers sync.WaitGroup
				for i := 0; i < n; i++ {
					producers.Add(1)
					go func(i int) { defer producers.Done(); <-start; results[i] = mb.PostJob(jobs[i+3]) }(i)
				}
				go func() { <-start; mb.BeginStop(); close(stopped) }()
				close(start)
				producers.Wait()
				awaitAdmission(t, stopped)
				unblock()
				mb.Wait()
				accepted := int64(3)
				for i, err := range results {
					if err == nil {
						accepted++
					} else {
						require.Equal(t, []string{"discard", "complete", "release"}, jobs[i+3].history())
					}
				}
				require.Equal(t, accepted, w.count.Load())
				for i, j := range jobs {
					require.Equal(t, int64(1), j.releases.Load(), "job %d", i)
					if i == 0 {
						require.Equal(t, []string{"execute", "complete", "release"}, j.history())
						continue
					}
					if i < 3 || results[i-3] == nil {
						terminal := "execute"
						if policy == DrainDiscard {
							terminal = "discard"
						}
						require.Equal(t, []string{terminal, "complete", "release"}, j.history(), "job %d", i)
					}
				}
				require.Zero(t, w.GetJobLen())
			})
		}
	}
}

func TestRWBoundedUnboundedReadChFullOwnership(t *testing.T) {
	for _, backend := range []string{"bounded", "unbounded"} {
		t.Run(backend, func(t *testing.T) {
			c := lifecycleConf(backend)
			c.EnableRWMode = true
			c.MaxConcurrentReads = 1
			c.SchedulePolicy.InitialWorkerNum = 2
			mb, _ := startLifecycleMailbox(t, c, DrainExecute)
			resume := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(resume) }) }
			t.Cleanup(unblock)
			first := trackedLifecycleJob()
			first.SetRWMode(def.RWModeRead)
			first.entered, first.resume = make(chan struct{}), resume
			require.NoError(t, mb.PostJob(first))
			awaitAdmission(t, first.entered)
			var w *Worker
			for _, worker := range mb.workerPool.snap.Load().workers {
				candidate := worker.(*Worker)
				if candidate.readsDispatched.Load() == 1 {
					w = candidate
				}
			}
			require.NotNil(t, w)
			require.True(t, w.env.rw.enabled.Load())
			require.EqualValues(t, 1, w.readsDispatched.Load(), "first read dispatched")
			jobs := []*lifecycleJob{first}
			// One read executes, one waits for the semaphore, then readCh fills.
			for i := 0; i < cap(w.readCh)+2; i++ {
				j := trackedLifecycleJob()
				j.SetRWMode(def.RWModeRead)
				jobs = append(jobs, j)
				require.NoError(t, mb.PostJob(j))
				dispatched := uint64(i + 2)
				waitUntil(t, func() bool { return w.readsDispatched.Load() == dispatched }, time.Second, "read dispatched")
				if i == 0 {
					waitUntil(t, func() bool { return len(w.readCh) == 0 }, time.Second, "pipeline waiting on semaphore")
				}
			}
			overflow := jobs[len(jobs)-1]
			waitUntil(t, func() bool { return overflow.releases.Load() == 1 }, time.Second, "asynchronous overflow cleanup")
			require.Equal(t, []string{"discard", "complete", "release"}, overflow.history())
			require.Zero(t, overflow.executes.Load())
			require.Equal(t, int64(len(jobs)), w.count.Load(), "all PostJob calls succeeded, including secondary overflow")
			require.EqualValues(t, 1, w.env.rw.drainDiscardTotal.Load())
			require.Zero(t, mb.metrics.dispatchFailedTotal.Load(), "secondary overflow is not primary admission failure")
			unblock()
			mb.Stop()
			for _, j := range jobs {
				require.Equal(t, int64(1), j.releases.Load())
			}
			require.Zero(t, w.GetJobLen())
			require.Empty(t, w.readCh)
		})
	}
}

func TestRWBoundedUnboundedUnsafeDrainOwnership(t *testing.T) {
	for _, backend := range []string{"bounded", "unbounded"} {
		t.Run(backend, func(t *testing.T) {
			c := lifecycleConf(backend)
			c.EnableRWMode = true
			c.MaxConcurrentReads = 1
			c.SchedulePolicy.InitialWorkerNum = 2
			c.StopTimeout = 20 * time.Millisecond
			mb, _ := startLifecycleMailbox(t, c, DrainExecute)
			resume := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(resume) }) }
			t.Cleanup(unblock)
			first := trackedLifecycleJob()
			first.SetRWMode(def.RWModeRead)
			first.entered, first.resume = make(chan struct{}), resume
			require.NoError(t, mb.PostJob(first))
			awaitAdmission(t, first.entered)
			var w *Worker
			for _, worker := range mb.workerPool.snap.Load().workers {
				candidate := worker.(*Worker)
				if candidate.readsDispatched.Load() == 1 {
					w = candidate
				}
			}
			require.NotNil(t, w)
			pending := trackedLifecycleJob()
			pending.SetRWMode(def.RWModeRead)
			require.NoError(t, mb.PostJob(pending))
			waitUntil(t, func() bool { return w.readsDispatched.Load() == 2 && len(w.readCh) == 0 }, time.Second, "pending read held by pipeline")
			mb.Stop()
			waitUntil(t, func() bool { return pending.releases.Load() == 1 }, time.Second, "unsafe cleanup completed")
			require.Equal(t, []string{"cleanup", "release"}, pending.history())
			require.Zero(t, pending.executes.Load())
			require.Zero(t, first.releases.Load(), "in-flight execution retains ownership")
			unblock()
			waitUntil(t, func() bool { return first.releases.Load() == 1 && w.inflightReadCnt.Load() == 0 }, time.Second, "in-flight read completed")
			require.Equal(t, []string{"execute", "complete", "release"}, first.history())
			require.EqualValues(t, 1, pending.releases.Load())
			require.Zero(t, w.GetJobLen())
			require.Empty(t, w.readCh)
		})
	}
}
