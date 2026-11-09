package actor

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestPIDIdentity(t *testing.T) {
	pid := NewPID("address", "node-1", "7", "logic", "Gate", 1, "grpc")
	if got := pid.GetServiceUid(); got != "Nw.R2F0ZQ.bm9kZS0x" {
		t.Fatalf("UID = %q, want 7.Gate.node-1", got)
	}
	if got := pid.GetPrimarySecondaryKey(); got != "R2F0ZQ.Nw" {
		t.Fatalf("election key = %q, want Gate.7", got)
	}
}

func TestPIDRemovesServiceIDSchemaMetadata(t *testing.T) {
	descriptor := (&PID{}).ProtoReflect().Descriptor()
	if descriptor.Fields().ByName("ServiceId") != nil {
		t.Error("removed ServiceId field must not be present")
	}
	fields := descriptor.Fields()
	for i := 0; i < fields.Len(); i++ {
		if got, want := fields.Get(i).Number(), protoreflect.FieldNumber(i+1); got != want {
			t.Errorf("field %s number = %d, want %d", fields.Get(i).Name(), got, want)
		}
	}
	if descriptor.ReservedRanges().Has(4) {
		t.Error("removed field number must not remain reserved")
	}
	if descriptor.ReservedNames().Has(protoreflect.Name("ServiceId")) {
		t.Error("removed field name must not remain reserved")
	}
}
