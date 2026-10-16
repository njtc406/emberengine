package mailbox

import "testing"

func TestMailboxFlightFreeze(t *testing.T) {
	f := newMailboxFlight(3)
	for i := 0; i < 5; i++ {
		f.record(mailboxFlightSample{Index: i}, i == 3)
	}
	got := f.ordered()
	if len(got) != 3 || got[0].Index != 1 || got[2].Index != 3 || !f.frozen {
		t.Fatalf("first rejection history: %+v", got)
	}
}

func TestMailboxFlightWrapAndAllocations(t *testing.T) {
	f := newMailboxFlight(3)
	if got := len(f.ordered()); got != 0 {
		t.Fatal(got)
	}
	if a := testing.AllocsPerRun(100, func() { f.record(mailboxFlightSample{}, false) }); a != 0 {
		t.Fatal(a)
	}
	if len(f.ordered()) != 3 || f.frozen {
		t.Fatal("bounded no-rejection history")
	}
}
