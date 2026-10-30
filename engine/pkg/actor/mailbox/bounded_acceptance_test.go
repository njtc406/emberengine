package mailbox

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/njtc406/emberengine/v2/engine/pkg/config"
	"github.com/njtc406/emberengine/v2/engine/pkg/def"
	inf "github.com/njtc406/emberengine/v2/engine/pkg/interfaces"
	"github.com/stretchr/testify/require"
)

type acceptanceObserver struct {
	lifecycleInvoker
	lifecycleMiddleware
	mu             sync.Mutex
	discardErrors  map[*lifecycleJob]error
	completeErrors map[*lifecycleJob]error
}

func (o *acceptanceObserver) ExecuteJob(ctx context.Context, j inf.IMailboxJob) error {
	return o.lifecycleInvoker.ExecuteJob(ctx, j)
}

func (o *acceptanceObserver) OnJobDiscarded(j inf.IMailboxJob, err error) {
	o.mu.Lock()
	o.discardErrors[j.(*lifecycleJob)] = err
	o.mu.Unlock()
	o.lifecycleInvoker.OnJobDiscarded(j, err)
}

func (o *acceptanceObserver) OnComplete(ctx inf.IMiddlewareContext, err error, result interface{}) {
	o.mu.Lock()
	o.completeErrors[ctx.Job().(*lifecycleJob)] = err
	o.mu.Unlock()
	o.lifecycleMiddleware.OnComplete(ctx, err, result)
}

func acceptanceConf(mode string) *config.MailboxConf {
	c := lifecycleConf("bounded")
	if mode == "priority" {
		c = priorityQueueConfig()
		c.StopTimeout = 5 * time.Second
		c.SchedulePolicy.InitialWorkerNum = 1
		for _, p := range c.SchedulePolicy.MultiLevelQueueConf.PriorityBatches {
			p.Capacity = 2
		}
	}
	return c
}

func acceptanceMailbox(t *testing.T, c *config.MailboxConf, policy DrainPolicy) (*Mailbox, *acceptanceObserver) {
	t.Helper()
	o := &acceptanceObserver{discardErrors: make(map[*lifecycleJob]error), completeErrors: make(map[*lifecycleJob]error)}
	mb, err := NewMailbox(c, &testLogger{t: t}, o, []inf.IMailboxMiddleware{o}, WithDrainPolicy(policy))
	require.NoError(t, err)
	mb.Start()
	t.Cleanup(mb.Stop)
	return mb, o
}

func acceptanceRejected(t *testing.T, o *acceptanceObserver, j *lifecycleJob, want error) {
	t.Helper()
	require.Equal(t, []string{"discard", "complete", "release"}, j.history())
	require.EqualValues(t, 1, j.releases.Load())
	require.Zero(t, j.executes.Load())
	o.mu.Lock()
	defer o.mu.Unlock()
	require.ErrorIs(t, o.discardErrors[j], want)
	require.ErrorIs(t, o.completeErrors[j], want)
}

func TestMutexRingMailboxAcceptanceRejection(t *testing.T) {
	for _, mode := range []string{"dual", "priority"} {
		for _, priority := range []def.Priority{def.PrioritySys, def.PriorityUrgent, def.PriorityHigh, def.PriorityNormal, def.PriorityLow, def.Priority(100)} {
			t.Run(fmt.Sprintf("%s/%d", mode, priority), func(t *testing.T) {
				mb, observer := acceptanceMailbox(t, acceptanceConf(mode), DrainExecute)
				first, unblock := blockLifecycleConsumer(t, mb)
				jobs := []*lifecycleJob{first}
				for i := 0; i < 2; i++ {
					j := trackedLifecycleJob()
					j.SetPriority(priority)
					require.NoError(t, mb.PostJob(j))
					jobs = append(jobs, j)
				}
				rejected := trackedLifecycleJob()
				rejected.SetPriority(priority)
				require.ErrorIs(t, mb.PostJob(rejected), def.ErrMailboxWorkerIsFull)
				acceptanceRejected(t, observer, rejected, def.ErrMailboxWorkerIsFull)
				unblock()
				mb.Stop()
				for _, j := range jobs {
					require.Equal(t, []string{"execute", "complete", "release"}, j.history())
					require.EqualValues(t, 1, j.releases.Load())
				}
			})
		}
	}
}

func TestMutexRingMailboxAcceptanceReadOverflow(t *testing.T) {
	for _, mode := range []string{"dual", "priority"} {
		t.Run(mode, func(t *testing.T) {
			c := acceptanceConf(mode)
			c.EnableRWMode = true
			c.MaxConcurrentReads = 1
			c.SchedulePolicy.InitialWorkerNum = 2
			mb, observer := acceptanceMailbox(t, c, DrainExecute)
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
			for _, candidate := range mb.workerPool.snap.Load().workers {
				if candidate.(*Worker).readsDispatched.Load() == 1 {
					w = candidate.(*Worker)
				}
			}
			require.NotNil(t, w)
			jobs := []*lifecycleJob{first}
			// The blocked read owns the semaphore; the next read parks the pipeline.
			for i := 0; i < cap(w.readCh)+2; i++ {
				j := trackedLifecycleJob()
				j.SetRWMode(def.RWModeRead)
				jobs = append(jobs, j)
				require.NoError(t, mb.PostJob(j), "secondary overflow is asynchronous")
				waitUntil(t, func() bool { return w.readsDispatched.Load() == uint64(i+2) }, time.Second, "read dispatched")
				if i == 0 {
					waitUntil(t, func() bool { return len(w.readCh) == 0 }, time.Second, "pipeline holds pending read")
				}
			}
			overflow := jobs[len(jobs)-1]
			waitUntil(t, func() bool { return overflow.releases.Load() == 1 }, time.Second, "overflow released")
			acceptanceRejected(t, observer, overflow, def.ErrMailboxWorkerIsFull)
			unblock()
			mb.Stop()
			for _, j := range jobs[:len(jobs)-1] {
				require.Equal(t, []string{"execute", "complete", "release"}, j.history())
				require.EqualValues(t, 1, j.releases.Load())
			}
			require.Empty(t, w.readCh)
			require.Zero(t, w.GetJobLen())
		})
	}
}

func TestMutexRingMailboxAcceptanceConcurrentStop(t *testing.T) {
	for _, mode := range []string{"dual", "priority"} {
		for _, policy := range []DrainPolicy{DrainExecute, DrainDiscard} {
			t.Run(fmt.Sprintf("%s/%d", mode, policy), func(t *testing.T) {
				mb, observer := acceptanceMailbox(t, acceptanceConf(mode), policy)
				first, unblock := blockLifecycleConsumer(t, mb)
				queued := trackedLifecycleJob()
				require.NoError(t, mb.PostJob(queued))
				const n = 64
				jobs, results := make([]*lifecycleJob, n), make([]error, n)
				start, stopped := make(chan struct{}), make(chan struct{})
				var wg sync.WaitGroup
				for i := range jobs {
					jobs[i] = trackedLifecycleJob()
					wg.Add(1)
					go func(i int) { defer wg.Done(); <-start; results[i] = mb.PostJob(jobs[i]) }(i)
				}
				go func() { <-start; mb.BeginStop(); close(stopped) }()
				close(start)
				wg.Wait()
				awaitAdmission(t, stopped)
				late := trackedLifecycleJob()
				require.ErrorIs(t, mb.PostJob(late), def.ErrMailboxWorkerClosed)
				acceptanceRejected(t, observer, late, def.ErrMailboxWorkerClosed)
				unblock()
				mb.Wait()
				require.Equal(t, []string{"execute", "complete", "release"}, first.history())
				require.EqualValues(t, 1, first.releases.Load())
				jobs, results = append(jobs, queued), append(results, nil)
				for i, j := range jobs {
					if results[i] != nil {
						require.Contains(t, []error{def.ErrMailboxNotRunning, def.ErrMailboxWorkerClosed, def.ErrMailboxWorkerIsFull}, results[i])
						acceptanceRejected(t, observer, j, results[i])
					} else if policy == DrainDiscard {
						acceptanceRejected(t, observer, j, def.ErrMailboxNotRunning)
					} else {
						require.Equal(t, []string{"execute", "complete", "release"}, j.history())
						require.EqualValues(t, 1, j.releases.Load())
					}
				}
			})
		}
	}
}
