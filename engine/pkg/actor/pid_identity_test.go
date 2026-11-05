package actor

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestPIDIdentity(t *testing.T) {
	pid := NewPID("address", "node-1", 7, "logic", "Gate", 1, "grpc")
	if got := pid.GetServiceUid(); got != "7.Gate.node-1" {
		t.Fatalf("UID = %q, want 7.Gate.node-1", got)
	}
	if got := pid.GetPrimarySecondaryKey(); got != "Gate.7" {
		t.Fatalf("election key = %q, want Gate.7", got)
	}
}

func TestPIDReservesRemovedField(t *testing.T) {
	descriptor := (&PID{}).ProtoReflect().Descriptor()
	if descriptor.Fields().ByNumber(4) != nil {
		t.Error("removed field 4 must not be present")
	}
	if !descriptor.ReservedRanges().Has(4) {
		t.Error("field number 4 must be reserved")
	}
	if !descriptor.ReservedNames().Has(protoreflect.Name("ServiceId")) {
		t.Error("removed field name must be reserved")
	}
}
