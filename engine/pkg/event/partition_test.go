package event

import (
	"context"
	"github.com/njtc406/emberengine/v2/engine/pkg/def"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"strings"
	"testing"
)

func TestPartitionSubjectAndWire(t *testing.T) {
	eb := NewEventBus()
	eb.serverPrefix = def.NatsDefaultServerPrefix
	seen := make(map[string]bool)
	for _, p := range []string{"1", "001", "a.b", "a/b", "*", ">", "", "分 区"} {
		key := eb.serverKey(def.EventType(1), p)
		parts := strings.Split(key, ".")
		if len(parts) != 4 || strings.ContainsAny(parts[3], "*> /\t\n") || parts[3] == "" {
			t.Fatalf("unsafe subject %q", key)
		}
		if seen[key] {
			t.Fatalf("subject collision: %q", key)
		}
		seen[key] = true
		be, err := eb.marshalEvent(context.Background(), def.EventType(1), p, "", wrapperspb.String("payload"))
		if err != nil {
			t.Fatal(err)
		}
		if be.event.GetPartition() != p {
			t.Fatalf("partition = %q, want %q", be.event.GetPartition(), p)
		}
	}
}
