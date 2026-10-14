package mailbox

import (
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"sync"
	"testing"
)

type metricsLengthQueue struct {
	queue[inf.IMailboxJob]
	length int
}

func (q metricsLengthQueue) Len() int { return q.length }

func TestMailboxMetricsNormalizePhysicalLengths(t *testing.T) {
	for _, priority := range []bool{false, true} {
		var manager IQueueManager = &DualQueueManager{systemMailbox: metricsLengthQueue{length: -3}, userMailbox: metricsLengthQueue{length: 5}}
		if priority {
			manager = &PriorityQueueManager{queues: map[def.Priority]queue[inf.IMailboxJob]{def.PrioritySys: metricsLengthQueue{length: -3}, def.PriorityNormal: metricsLengthQueue{length: 5}}}
		}
		p := &WorkerPool{queueSpec: normalizedQueueSpec{backend: "unbounded"}}
		p.snap.Store(&workersSnapshot{workers: map[int32]inf.IMailboxWorker{0: &Worker{queueManager: manager}}})
		mb := &Mailbox{workerPool: p}
		if m := mb.GetMailboxMetrics(); m.QueueLength != 5 || m.QueueBounded || m.QueueCapacity != 0 {
			t.Fatalf("normalize: %+v", m)
		}
	}
}

func TestMailboxMetricsRingFailure(t *testing.T) {
	mb, err := NewMailbox(newSimpleConf(2), &testLogger{t: t}, &lifecycleInvoker{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	mb.workerPool.snap.Store(&workersSnapshot{count: 2, ring: &dispatchRing{}})
	if err = mb.PostJob(trackedLifecycleJob()); err != def.ErrMailboxWorkerIsFull {
		t.Fatalf("ring: %v", err)
	}
	m := mb.GetMailboxMetrics()
	if m.QueueFullRejectedTotal != 0 || m.DispatchFailedTotal != 1 {
		t.Fatalf("ring must not count physical full: %+v", m)
	}
}

func TestMailboxMetricsRWFullAndSnapshotRemoval(t *testing.T) {
	p, err := NewWorkerPool(lifecycleConf("bounded"), &testLogger{t: t}, &lifecycleInvoker{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.cancel()
	w := newWorker(0, p.conf, p.queueSpec, p.workerEnv(), DrainDiscard).(*Worker)
	w.readCh = make(chan inf.IMailboxJob, 1)
	w.dispatchRead(trackedLifecycleJob())
	w.dispatchRead(trackedLifecycleJob())
	(<-w.readCh).Release()
	p.snap.Store(&workersSnapshot{workers: map[int32]inf.IMailboxWorker{0: w}})
	mb := &Mailbox{workerPool: p}
	for i := 0; i < 2; i++ {
		if err := w.SubmitJob(trackedLifecycleJob()); err != nil {
			t.Fatal(err)
		}
	}
	rejected := trackedLifecycleJob()
	if err := w.SubmitJob(rejected); err != def.ErrMailboxWorkerIsFull {
		t.Fatal(err)
	}
	rejected.Release()
	m := mb.GetMailboxMetrics()
	if m.ReadQueueFullDiscardedTotal != 1 || m.QueueFullRejectedTotal != 1 || m.ObservedQueueHighWatermark != 2 {
		t.Fatalf("counters: %+v", m)
	}
	w.queueManager.DrainAll(func(j inf.IMailboxJob) { j.Release() })
	p.snap.Store(&workersSnapshot{})
	m = mb.GetMailboxMetrics()
	if m.QueueCapacity != 0 || m.QueueLength != 0 || m.ReadQueueFullDiscardedTotal != 1 || m.QueueFullRejectedTotal != 1 || m.ObservedQueueHighWatermark != 2 {
		t.Fatalf("removed: %+v", m)
	}
	if p.workerEnv().queueFullRejectedTotal != w.env.queueFullRejectedTotal {
		t.Fatal("new workers must share counter")
	}
}

func TestMailboxMetricsResize(t *testing.T) {
	mb, w := startLifecycleMailbox(t, lifecycleConf("bounded"), DrainExecute)
	_, unblock := blockLifecycleConsumer(t, mb)
	for i := 0; i < 2; i++ {
		if err := mb.PostJob(trackedLifecycleJob()); err != nil {
			t.Fatal(err)
		}
	}
	_ = mb.PostJob(trackedLifecycleJob())
	if mb.workerPool.observedQueueHighWatermark.Load() != 0 {
		t.Fatal("peak updated without scrape")
	}
	if m := mb.GetMailboxMetrics(); m.QueueLength != 2 {
		t.Fatalf("queued: %+v", m)
	}
	unblock()
	mb.workerPool.resizeWorkers(2)
	if m := mb.GetMailboxMetrics(); m.QueueCapacity != 8 {
		t.Fatalf("expanded: %+v", m)
	}
	for _, worker := range mb.workerPool.snap.Load().workers {
		if worker.(*Worker).env.queueFullRejectedTotal != w.env.queueFullRejectedTotal {
			t.Fatal("counter not shared")
		}
	}
	mb.workerPool.resizeWorkers(1)
	m := mb.GetMailboxMetrics()
	if m.QueueCapacity != 4 || m.QueueFullRejectedTotal != 1 || m.ObservedQueueHighWatermark != 2 {
		t.Fatalf("shrunk: %+v", m)
	}
}

func TestMailboxMetricsPriorityPushPopDrain(t *testing.T) {
	p, err := NewWorkerPool(priorityQueueConfig(), &testLogger{t: t}, &lifecycleInvoker{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.cancel()
	w := newWorker(0, p.conf, p.queueSpec, p.workerEnv(), DrainDiscard).(*Worker)
	p.snap.Store(&workersSnapshot{workers: map[int32]inf.IMailboxWorker{0: w}})
	mb := &Mailbox{workerPool: p}
	if err := w.SubmitJob(trackedLifecycleJob()); err != nil {
		t.Fatal(err)
	}
	if m := mb.GetMailboxMetrics(); m.QueueLength != 1 || m.QueueCapacity != 62 {
		t.Fatalf("priority push: %+v", m)
	}
	j, ok := w.queueManager.NextJob()
	if !ok {
		t.Fatal("pop")
	}
	j.Release()
	if m := mb.GetMailboxMetrics(); m.QueueLength != 0 || m.ObservedQueueHighWatermark != 1 {
		t.Fatalf("priority pop: %+v", m)
	}
	if err := w.SubmitJob(trackedLifecycleJob()); err != nil {
		t.Fatal(err)
	}
	w.queueManager.DrainAll(func(j inf.IMailboxJob) { j.Release() })
	if m := mb.GetMailboxMetrics(); m.QueueLength != 0 {
		t.Fatalf("priority drain: %+v", m)
	}
}

func TestMailboxMetricsConcurrentScrape(t *testing.T) {
	for _, backend := range []string{"bounded", "unbounded"} {
		mb, _ := startLifecycleMailbox(t, lifecycleConf(backend), DrainExecute)
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for n := 0; n < 100; n++ {
					_ = mb.PostJob(trackedLifecycleJob())
					m := mb.GetMailboxMetrics()
					if m.QueueLength < 0 || (m.QueueBounded && m.QueueLength > m.QueueCapacity) {
						t.Errorf("concurrent: %+v", m)
					}
				}
			}()
		}
		wg.Wait()
		mb.Stop()
		if m := mb.GetMailboxMetrics(); m.QueueLength != 0 {
			t.Fatalf("residue: %+v", m)
		}
	}
}
