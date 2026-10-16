package mailbox

import (
	"testing"
	"time"
)

func TestMailboxPerformanceRejectsUnresolvedCurrentLatency(t *testing.T) {
	var base, current []mailboxPerfRow
	for _, s := range mailboxPerfScenarios() {
		for round := 0; round < 5; round++ {
			r := mailboxPerfRow{Protocol: "task2-v3-qpc", Scenario: s.Name, Round: round, Workers: s.Workers, EffectiveRW: s.RW, Samples: 100, Success: 100, Wall: time.Second, CPU: time.Second, Drained: true, Candidate: "baseline"}
			r.Machine.CPU = "cpu"
			r.Machine.OS = "windows"
			r.Machine.Commit = "73ecd28"
			r.Completion.P99 = time.Millisecond
			r.Completion.P999 = time.Millisecond
			base = append(base, r)
			for _, candidate := range []string{"unbounded", "bounded"} {
				r.Candidate = candidate
				current = append(current, r)
			}
		}
	}
	if failures := compareMailboxPerf(base, current); len(failures) != 0 {
		t.Fatal(failures)
	}
	for i := range current {
		current[i].Completion.P99 = 0
		current[i].Completion.P999 = 0
	}
	if failures := compareMailboxPerf(base, current); len(failures) == 0 {
		t.Fatal("unresolved current latency was accepted as an improvement")
	}
}
