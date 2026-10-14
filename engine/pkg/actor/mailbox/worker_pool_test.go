package mailbox

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

func TestWorkerPoolAdmissionShrinkOwnership(t *testing.T) {
	inv := &releaseTrackingInvoker{}
	p, err := NewWorkerPool(newSimpleConf(2), &testLogger{t: t}, inv)
	if err != nil {
		t.Fatal(err)
	}
	p.rw.stopTimeout = 10 * time.Millisecond
	survivor := newWorker(0, p.conf, p.workerEnv(), DrainExecute).(*Worker)
	victim := newWorker(1, p.conf, p.workerEnv(), DrainDiscard).(*Worker)
	q := &admissionQueue{IQueueManager: NewDualQueueManager(), entered: make(chan struct{}), resume: make(chan struct{})}
	victim.queueManager = q
	p.workerCount.Store(2)
	p.publishSnapshotLocked(map[int32]inf.IMailboxWorker{0: survivor, 1: victim}, newDispatchRing([]int32{0, 1}), map[int32]*atomic.Uint64{})
	survivor.Start()
	victim.Start()
	defer survivor.Stop()
	j := &admissionOwnershipJob{IMailboxJob: newJob()}
	result := make(chan error, 1)
	go func() { result <- victim.SubmitJob(j) }()
	awaitAdmission(t, q.entered)
	started, done := make(chan struct{}), make(chan struct{})
	go func() { close(started); p.resizeWorkers(1); close(done) }()
	awaitAdmission(t, started)
	select {
	case <-done:
		t.Error("shrink removed Worker with publication in flight")
	case <-time.After(5 * p.rw.stopTimeout):
	}
	close(q.resume)
	if err := <-result; err != nil {
		t.Errorf("admitted publication rejected: %v", err)
	}
	awaitAdmission(t, done)
	if _, ok := p.snap.Load().workers[1]; ok {
		t.Error("victim remains in snapshot")
	}
	if j.releases.Load() != 1 || inv.discardCount.Load() != 1 {
		t.Error("shrink lost terminal ownership")
	}
	rejected := &admissionOwnershipJob{IMailboxJob: newJob()}
	if err := victim.SubmitJob(rejected); err != def.ErrMailboxWorkerClosed {
		t.Errorf("old snapshot worker returned %v", err)
	}
	if rejected.releases.Load() != 0 {
		t.Error("rejected ownership consumed")
	}
	rejected.Release()
	if rejected.releases.Load() != 1 {
		t.Error("rejected job release mismatch")
	}
}
