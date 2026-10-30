package mailbox

import (
	"runtime"
	"sync"
	"testing"

	"github.com/njtc406/emberengine/v2/engine/pkg/utils/boundedmpsc"
	"github.com/njtc406/emberengine/v2/engine/pkg/utils/mpsc"

	"github.com/njtc406/emberengine/v2/engine/pkg/config"
	"github.com/njtc406/emberengine/v2/engine/pkg/def"
	inf "github.com/njtc406/emberengine/v2/engine/pkg/interfaces"
	"github.com/stretchr/testify/require"
)

type boundedManagerJob struct {
	inf.IMailboxJob
	priority  def.Priority
	published bool
}

func (j *boundedManagerJob) GetPriority() def.Priority {
	if j.published {
		panic("job accessed after publication")
	}
	return j.priority
}
func (j *boundedManagerJob) SetPriority(p def.Priority) {
	if j.published {
		panic("job accessed after publication")
	}
	j.priority = p
}
func boundedManager(t *testing.T, conf *config.MailboxConf) (m IQueueManager) {
	t.Helper()
	spec, err := normalizeQueueSpec(conf)
	require.NoError(t, err)
	require.NotPanics(t, func() { m = createQueueManager(spec, nil) })
	require.NotNil(t, m)
	return
}
func TestDualQueueManagerBounded(t *testing.T) {
	m := boundedManager(t, boundedQueueConfig())
	for _, tc := range []struct {
		priority def.Priority
		capacity int
	}{{def.PrioritySys, 2}, {def.PriorityNormal, 4}} {
		var jobs []*boundedManagerJob
		for i := 0; i < tc.capacity; i++ {
			j := &boundedManagerJob{priority: tc.priority}
			jobs = append(jobs, j)
			require.NoError(t, m.Submit(j))
		}
		require.ErrorIs(t, m.Submit(&boundedManagerJob{priority: tc.priority}), def.ErrMailboxWorkerIsFull)
		other := def.PriorityNormal
		if tc.priority == other {
			other = def.PrioritySys
		}
		j := &boundedManagerJob{priority: other}
		require.NoError(t, m.Submit(j))
		dual := m.(*DualQueueManager)
		q := dual.systemMailbox
		if tc.priority == def.PriorityNormal {
			q = dual.userMailbox
		}
		got, ok := q.Pop()
		require.True(t, ok)
		require.Same(t, jobs[0], got)
		replacement := &boundedManagerJob{priority: tc.priority}
		require.NoError(t, m.Submit(replacement))
		for _, want := range append(jobs[1:], replacement) {
			got, ok = q.Pop()
			require.True(t, ok)
			require.Same(t, want, got)
		}
		got, ok = m.NextJob()
		require.True(t, ok)
		require.Same(t, j, got)
		require.True(t, m.IsEmpty())
		require.Zero(t, m.GetJobLen())
	}
}
func TestPriorityQueueManagerBounded(t *testing.T) {
	conf := priorityQueueConfig()
	m := boundedManager(t, conf).(*PriorityQueueManager)
	for _, p := range m.sortedPriorities {
		capacity := conf.SchedulePolicy.MultiLevelQueueConf.PriorityBatches[p].Capacity
		for i := 0; i < capacity; i++ {
			require.NoError(t, m.Submit(&boundedManagerJob{priority: p}))
		}
		rejected := &boundedManagerJob{priority: p}
		require.ErrorIs(t, m.Submit(rejected), def.ErrMailboxWorkerIsFull)
		require.Equal(t, p, rejected.priority)
		require.Equal(t, capacity, m.GetPriorityQueueLen(p))
	}
	unknown := def.Priority(100)
	rejected := &boundedManagerJob{priority: unknown}
	require.ErrorIs(t, m.Submit(rejected), def.ErrMailboxWorkerIsFull)
	require.Equal(t, unknown, rejected.priority)
	_, ok := m.queues[m.fallbackPriority].Pop()
	require.True(t, ok)
	require.NoError(t, m.Submit(rejected))
	require.Equal(t, m.fallbackPriority, rejected.priority)
	_, ok = m.queues[def.PrioritySys].Pop()
	require.True(t, ok)
	require.NoError(t, m.Submit(&boundedManagerJob{priority: def.PrioritySys}))
	count := m.GetJobLen()
	drained := 0
	m.DrainAll(func(inf.IMailboxJob) { drained++ })
	require.Equal(t, count, drained)
	require.True(t, m.IsEmpty())
}
func TestPriorityQueueManagerBoundedWorkerFallbackRestore(t *testing.T) {
	m := boundedManager(t, priorityQueueConfig()).(*PriorityQueueManager)
	w := &Worker{queueManager: m, env: &WorkerEnv{}}
	w.state.Store(workerStateRunning)
	for i := 0; i < 32; i++ {
		require.NoError(t, w.SubmitJob(&boundedManagerJob{priority: def.PriorityLow}))
	}
	j := &boundedManagerJob{priority: def.Priority(100)}
	require.ErrorIs(t, w.SubmitJob(j), def.ErrMailboxWorkerIsFull)
	require.Equal(t, def.Priority(100), j.priority)
}

// Simulates a consumer taking ownership before Push returns.
type ownershipQueue struct {
	queue[inf.IMailboxJob]
	target def.Priority
}

func (q *ownershipQueue) Push(job inf.IMailboxJob) bool {
	j := job.(*boundedManagerJob)
	if j.GetPriority() != q.target {
		panic("priority was not set before publication")
	}
	j.published = true
	return true
}
func TestPriorityQueueManagerBoundedNoPostPublishAccess(t *testing.T) {
	for _, worker := range []bool{false, true} {
		m := boundedManager(t, priorityQueueConfig()).(*PriorityQueueManager)
		m.queues[m.fallbackPriority] = &ownershipQueue{target: m.fallbackPriority}
		j := &boundedManagerJob{priority: def.Priority(100)}
		require.NotPanics(t, func() {
			if worker {
				w := &Worker{queueManager: m, env: &WorkerEnv{}}
				w.state.Store(workerStateRunning)
				require.NoError(t, w.SubmitJob(j))
			} else {
				require.NoError(t, m.Submit(j))
			}
		})
		require.True(t, j.published)
	}
}
func TestPriorityQueueManagerBoundedRejectMissingSystem(t *testing.T) {
	spec, err := normalizeQueueSpec(priorityQueueConfig())
	require.NoError(t, err)
	delete(spec.priority.priorities, def.PrioritySys)
	require.Panics(t, func() { createQueueManager(spec, nil) })
}
func TestQueueFactoryBackends(t *testing.T) {
	require.IsType(t, &boundedmpsc.Queue[int]{}, newQueue[int](normalizedPhysicalQueueSpec{backend: "bounded", capacity: 2}))
	require.IsType(t, &mpsc.Queue[int]{}, newQueue[int](normalizedPhysicalQueueSpec{backend: "unbounded"}))
	for _, s := range []normalizedPhysicalQueueSpec{{backend: "bogus"}, {backend: "bounded", capacity: 3}} {
		require.Panics(t, func() { newQueue[int](s) })
	}
}
func TestDualQueueManagerUnboundedConstructor(t *testing.T) {
	m := newDualQueueManager(normalizedQueueSpec{backend: "unbounded", dual: &normalizedDualQueueSpec{}})
	for i := 0; i < 100; i++ {
		require.NoError(t, m.Submit(&boundedManagerJob{priority: def.PriorityNormal}))
	}
	require.Equal(t, 100, m.GetJobLen())
}
func TestPriorityQueueManagerUnboundedConstructor(t *testing.T) {
	spec, err := normalizeQueueSpec(&config.MailboxConf{QueueBackend: "unbounded", QueueMode: QueueModePriority,
		SchedulePolicy: &config.WorkerSchedulePolicy{MultiLevelQueueConf: &config.MultiLevelQueueConf{PriorityBatches: map[def.Priority]*config.PriorityConfig{def.PriorityNormal: {Capacity: 2}}}}})
	require.NoError(t, err)
	m := newPriorityQueueManager(spec, nil)
	for i := 0; i < 100; i++ {
		require.NoError(t, m.Submit(&boundedManagerJob{priority: def.PriorityNormal}))
	}
	require.Equal(t, 100, m.GetJobLen())
}
func TestPriorityQueueManagerBoundedConcurrentFallback(t *testing.T) {
	m := boundedManager(t, priorityQueueConfig())
	const producers, each = 4, 100
	var wg sync.WaitGroup
	for p := 0; p < producers; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				j := &boundedManagerJob{priority: def.Priority(100)}
				for {
					err := m.Submit(j)
					if err == nil {
						break
					}
					if err != def.ErrMailboxWorkerIsFull || j.priority != def.Priority(100) {
						t.Error("rejection did not preserve priority")
						return
					}
					runtime.Gosched()
				}
			}
		}()
	}
	for n := 0; n < producers*each; {
		j, ok := m.NextJob()
		if !ok {
			runtime.Gosched()
			continue
		}
		require.Equal(t, def.PriorityLow, j.GetPriority())
		j.SetPriority(def.PriorityNormal)
		n++
	}
	wg.Wait()
	require.True(t, m.IsEmpty())
}
