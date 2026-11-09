package actor

import (
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"strings"
	"testing"
)

func TestPartitionWireAndIdentity(t *testing.T) {
	seen := make(map[string]bool)
	for _, partition := range []string{"1", "001", "0", "", "a.b", "a/b", "分区.* >"} {
		p := NewPID("addr", "node", partition, "logic", "Service", 1, "grpc")
		if seen[p.ServiceUid] {
			t.Fatalf("UID collision for %q", partition)
		}
		seen[p.ServiceUid] = true
		if strings.ContainsAny(p.ServiceUid, "/ *>\t\n") {
			t.Fatalf("unsafe UID %q", p.ServiceUid)
		}
		wire, err := MarshalPID(p)
		if err != nil {
			t.Fatal(err)
		}
		var got PID
		if err = proto.Unmarshal(wire, &got); err != nil {
			t.Fatal(err)
		}
		if got.Partition != partition || got.ServiceUid != p.ServiceUid {
			t.Fatalf("wire round trip: %v", &got)
		}
		json, err := MarshalPIDJSON(p)
		if err != nil {
			t.Fatal(err)
		}
		if err = protojson.Unmarshal(json, &got); err != nil {
			t.Fatal(err)
		}
		if got.Partition != partition {
			t.Fatalf("discovery round trip = %q", got.Partition)
		}
	}
	a := NewPID("", "node", "a.b", "", "c", 1, "")
	b := NewPID("", "node", "a", "", "b.c", 1, "")
	if a.ServiceUid == b.ServiceUid {
		t.Fatal("ambiguous UID components")
	}
	a = NewPID("", "node", "c", "", "a.b", 1, "")
	b = NewPID("", "node", "b.c", "", "a", 1, "")
	if a.GetPrimarySecondaryKey() == b.GetPrimarySecondaryKey() {
		t.Fatal("ambiguous election key components")
	}
}
