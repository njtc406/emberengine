package mailbox

import (
	"context"
	"testing"

	mbjob "github.com/njtc406/emberengine/engine/pkg/actor/mailbox/job"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

func TestMailboxMetricsCollector_Snapshot(t *testing.T) {
	var c mailboxMetricsCollector
	c.postTotal.Add(10)
	c.suspendedTotal.Add(2)
	c.rejectedTotal.Add(3)
	c.dispatchFailedTotal.Add(1)

	m := c.snapshot()
	if m.PostTotal != 10 {
		t.Errorf("PostTotal = %d, want 10", m.PostTotal)
	}
	if m.SuspendedTotal != 2 {
		t.Errorf("SuspendedTotal = %d, want 2", m.SuspendedTotal)
	}
	if m.RejectedTotal != 3 {
		t.Errorf("RejectedTotal = %d, want 3", m.RejectedTotal)
	}
	if m.DispatchFailedTotal != 1 {
		t.Errorf("DispatchFailedTotal = %d, want 1", m.DispatchFailedTotal)
	}
}

func TestMailboxMetricsCollector_ZeroSnapshot(t *testing.T) {
	var c mailboxMetricsCollector
	m := c.snapshot()
	if m.PostTotal != 0 || m.SuspendedTotal != 0 || m.RejectedTotal != 0 || m.DispatchFailedTotal != 0 {
		t.Errorf("zero snapshot should be all zeros, got %+v", m)
	}
}

func TestMailboxMetricsCollector_Concurrent(t *testing.T) {
	var c mailboxMetricsCollector
	done := make(chan struct{})
	for i := 0; i < 100; i++ {
		go func() {
			for j := 0; j < 1000; j++ {
				c.postTotal.Add(1)
				c.suspendedTotal.Add(1)
				c.rejectedTotal.Add(1)
				c.dispatchFailedTotal.Add(1)
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 100; i++ {
		<-done
	}
	m := c.snapshot()
	if m.PostTotal != 100000 {
		t.Errorf("PostTotal = %d, want 100000", m.PostTotal)
	}
	if m.SuspendedTotal != 100000 {
		t.Errorf("SuspendedTotal = %d, want 100000", m.SuspendedTotal)
	}
}

func newMetricsTestJob(key string) inf.IMailboxJob {
	j := mbjob.NewEventBusJob()
	j.SetContext(context.Background())
	j.SetPriority(def.PriorityNormal)
	j.SetDispatcherKey(key)
	return j
}

func TestMailbox_GetMailboxMetrics_PostJob(t *testing.T) {
	invoker := &countingInvoker{mockInvoker: mockInvoker{name: "test-metrics"}}
	mb, err := NewMailbox(newSimpleConf(1), &testLogger{t: t}, invoker, nil)
	if err != nil {
		t.Fatalf("NewMailbox: %v", err)
	}
	mb.workerPool.Start()
	defer mb.Stop()

	if err := mb.PostJob(newMetricsTestJob("k1")); err != nil {
		t.Fatalf("PostJob: %v", err)
	}

	m := mb.GetMailboxMetrics()
	if m.PostTotal < 1 {
		t.Errorf("PostTotal = %d, want >= 1", m.PostTotal)
	}
}

func TestMailbox_GetMailboxMetrics_Suspended(t *testing.T) {
	invoker := &countingInvoker{mockInvoker: mockInvoker{name: "test-suspend-metrics"}}
	mb, err := NewMailbox(newSimpleConf(1), &testLogger{t: t}, invoker, nil)
	if err != nil {
		t.Fatalf("NewMailbox: %v", err)
	}
	mb.workerPool.Start()
	defer mb.Stop()

	mb.Suspend()

	_ = mb.PostJob(newMetricsTestJob("k1"))

	m := mb.GetMailboxMetrics()
	if m.PostTotal < 1 {
		t.Errorf("PostTotal = %d, want >= 1", m.PostTotal)
	}
	if m.SuspendedTotal < 1 {
		t.Errorf("SuspendedTotal = %d, want >= 1", m.SuspendedTotal)
	}
}
