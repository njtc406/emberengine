package errorx

import (
	"errors"
	"testing"

	"github.com/njtc406/emberengine/v2/engine/pkg/actor"
	"google.golang.org/protobuf/proto"
)

func TestUnmarshalDeepChainEndsWithNilInterface(t *testing.T) {
	detail := &actor.ErrorDetail{Code: 1, Msg: "root"}
	for i := 0; i < maxChainDepth+5; i++ {
		detail = &actor.ErrorDetail{Code: int32(i + 2), Msg: "layer", Cause: detail}
	}
	data, err := proto.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	restored := UnmarshalFromBytes(data)
	count := 0
	for current := restored; current != nil; current = errors.Unwrap(current) {
		ex, ok := current.(*Error)
		if !ok || ex == nil {
			t.Fatalf("error chain contains a typed nil at depth %d: %T", count, current)
		}
		count++
	}
	if count != maxChainDepth+1 {
		t.Fatalf("chain length = %d, want %d", count, maxChainDepth+1)
	}
	if errors.Is(restored, errors.New("missing")) {
		t.Fatal("unexpected error match")
	}
	if restored.Error() == "" || restored.(*Error).Detail() == "" {
		t.Fatal("truncated chain should still be printable")
	}
}
