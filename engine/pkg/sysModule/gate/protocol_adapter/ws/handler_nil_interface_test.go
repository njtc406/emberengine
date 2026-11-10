package ws

import "testing"

func TestGetProcessorNilInterface(t *testing.T) {
	var h Handler
	if got := h.GetProcessor(); got != nil {
		t.Fatalf("uninitialized processor = %T, want nil interface", got)
	}
	h.processor = NewProcessor(false, false)
	if h.GetProcessor() != h.processor {
		t.Fatal("getter must preserve the existing processor")
	}
}
