package actor

import "testing"

type namedPartition int
type stringPartition struct{}

func (stringPartition) String() string { return "zone" }

func TestPIDAnyPartition(t *testing.T) {
	for _, tt := range []struct {
		value any
		want  string
	}{
		{1, "1"}, {namedPartition(1), "1"}, {"001", "001"}, {true, "true"}, {1.5, "1.5"},
		{struct{ X int }{2}, "{2}"}, {[]int{1, 2}, "[1 2]"}, {map[string]int{"a": 1}, "map[a:1]"},
		{stringPartition{}, "zone"}, {nil, ""}, {(*stringPartition)(nil), ""}, {[]int(nil), ""},
	} {
		pid := NewPID("", "node", tt.value, "type", "name", 1, "local")
		if pid.GetPartition() != tt.want {
			t.Errorf("%T partition = %q, want %q", tt.value, pid.GetPartition(), tt.want)
		}
		if pid.GetServiceUid() != CreateInstanceId(tt.want, "name", "node") || CreateInstanceId(tt.value, "name", "node") != pid.GetServiceUid() {
			t.Fatal("identity normalization mismatch")
		}
	}
}

type panicPartition struct{}

func (panicPartition) String() string { panic("broken") }
func TestPartitionStringFormattingPanic(t *testing.T) {
	if got := PartitionString(panicPartition{}); got != "%!v(PANIC=String method: broken)" {
		t.Fatalf("diagnostic = %q", got)
	}
}
