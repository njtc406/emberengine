package job

import (
	"testing"

	"github.com/njtc406/emberengine/v2/engine/pkg/actor"
)

func TestEventJobPayloadNilInterface(t *testing.T) {
	j := NewEventBusJob()
	defer j.Release()
	if got := GetJobPayload(j); got != nil {
		t.Fatalf("empty event payload = %T, want nil interface", got)
	}
	if got := GetJobPayloadAs[*actor.Event](j); got != nil {
		t.Fatal("typed payload getter must preserve the zero value")
	}
	payload := &actor.Event{}
	j.SetPayload(payload)
	if GetJobPayload(j) != payload || GetJobPayloadAs[*actor.Event](j) != payload {
		t.Fatal("payload getters must preserve non-nil payload identity")
	}
	j.SetPayload(nil)
	if GetJobPayload(j) != nil {
		t.Fatal("cleared event payload must return nil interface")
	}
}
