package mailbox

import "testing"

func TestPerfMemoryAccountingRejectsCompensatingErrors(t *testing.T) {
	for _, event := range []string{"execute", "discard", "release"} {
		t.Run(event, func(t *testing.T) {
			states := make([]perfMemoryState, 2)
			for i := range states {
				states[i].accepted = event != "discard"
				if states[i].accepted {
					states[i].executed.Store(1)
				} else {
					states[i].discarded.Store(1)
				}
				states[i].released.Store(1)
			}
			if err := validatePerfMemoryStates(states); err != nil {
				t.Fatal(err)
			}
			switch event {
			case "execute":
				states[0].executed.Store(2)
				states[1].executed.Store(0)
			case "discard":
				states[0].discarded.Store(2)
				states[1].discarded.Store(0)
			case "release":
				states[0].released.Store(2)
				states[1].released.Store(0)
			}
			if err := validatePerfMemoryStates(states); err == nil {
				t.Fatal("compensating duplicate/missing accepted")
			}
		})
	}
}

func TestPerfMemoryAccountingRejectsWrongDisposition(t *testing.T) {
	states := make([]perfMemoryState, 2)
	states[0].accepted = true
	states[0].discarded.Store(1)
	states[1].executed.Store(1)
	for i := range states {
		states[i].released.Store(1)
	}
	if err := validatePerfMemoryStates(states); err == nil {
		t.Fatal("swapped acceptance disposition accepted")
	}
}
