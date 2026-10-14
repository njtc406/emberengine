package mailbox

import (
	"fmt"
	"testing"
	"time"
)

const mailboxPerfReplicatedProtocol = "task2-v4-replicated"

func mailboxPerfReplication(mode string) (int, error) {
	switch mode {
	case "":
		return 1, nil
	case mailboxPerfReplicatedProtocol, "task2-v5-window":
		return 32, nil
	case "task2-v5-window-fixed128":
		return 128, nil
	default:
		return 0, fmt.Errorf("unsupported fixed replication protocol %q", mode)
	}
}

func mailboxPerfCPUResolved(cpu time.Duration) bool {
	return cpu >= 3125*time.Millisecond
}

func TestMailboxPerfReplicationProtocol(t *testing.T) {
	for _, tc := range []struct {
		mode    string
		windows int
		valid   bool
	}{
		{"", 1, true}, {"task2-v4-replicated", 32, true}, {"task2-v5-window", 32, true},
		{"task2-v5-window-fixed128", 128, true}, {"128", 0, false}, {"32", 0, false},
	} {
		windows, err := mailboxPerfReplication(tc.mode)
		if (err == nil) != tc.valid || windows != tc.windows {
			t.Fatalf("%q: %d %v", tc.mode, windows, err)
		}
	}
	for _, tc := range []struct {
		cpu   time.Duration
		valid bool
	}{
		{0, false}, {time.Second, false}, {3125*time.Millisecond - time.Nanosecond, false},
		{3125 * time.Millisecond, true}, {4 * time.Second, true},
	} {
		if got := mailboxPerfCPUResolved(tc.cpu); got != tc.valid {
			t.Errorf("CPU %v: got %v", tc.cpu, got)
		}
	}
}
