package mailbox

import (
	"sync/atomic"
	"testing"
)

func TestMailboxPerfWindowWaitsForEveryRelease(t *testing.T) {
	var released atomic.Int64
	f := newMailboxPerfWindow(2, &released)
	if err := f.prepare(mailboxPerfScenario{}); err != nil {
		t.Fatal(err)
	}
	f.jobs[0].Release()
	if _, err := f.collect(nil); err == nil {
		t.Fatal("collected partially released window")
	}
	if err := f.prepare(mailboxPerfScenario{}); err == nil {
		t.Fatal("reused partially released window")
	}
	f.jobs[1].Release()
	samples, err := f.collect(nil)
	if err != nil || len(samples) != 0 {
		t.Fatalf("discarded window: %v %v", samples, err)
	}
	for _, j := range f.jobs {
		if j.IMailboxJob != nil {
			t.Fatal("retained released pooled Job")
		}
	}
	if err := f.prepare(mailboxPerfScenario{}); err != nil {
		t.Fatal(err)
	}
	for _, j := range f.jobs {
		j.Release()
	}
	if _, err := f.collect(nil); err != nil {
		t.Fatal(err)
	}
}
