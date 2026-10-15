package def

import "testing"

func TestParseServiceVisibilityDefaultsToNode(t *testing.T) {
	visibility, ok := ParseServiceVisibility("")
	if !ok {
		t.Fatalf("empty visibility should be accepted")
	}
	if visibility != ServiceVisibilityNode {
		t.Fatalf("expected empty visibility to default to node, got %s", visibility.String())
	}
}

func TestParseServiceVisibilityRejectsPrivate(t *testing.T) {
	_, ok := ParseServiceVisibility("private")
	if ok {
		t.Fatalf("private visibility should not be accepted")
	}
}

func TestParseServiceVisibilityRejectsAuto(t *testing.T) {
	_, ok := ParseServiceVisibility("auto")
	if ok {
		t.Fatalf("auto visibility should not be accepted")
	}
}
