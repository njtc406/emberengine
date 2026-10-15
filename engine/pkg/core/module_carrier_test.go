package core

import "testing"

type testEmbeddedModule struct {
	Module
}

func TestAsCoreModuleNil(t *testing.T) {
	m, ok := asCoreModule(nil)
	if ok || m != nil {
		t.Fatalf("expected nil,false for nil module")
	}
}

func TestAsCoreModuleDirect(t *testing.T) {
	base := &Module{}
	m, ok := asCoreModule(base)
	if !ok {
		t.Fatalf("expected direct *Module to be recognized")
	}
	if m != base {
		t.Fatalf("expected returned pointer to equal input module")
	}
}

func TestAsCoreModuleEmbedded(t *testing.T) {
	embedded := &testEmbeddedModule{}
	m, ok := asCoreModule(embedded)
	if !ok {
		t.Fatalf("expected embedded module to satisfy coreModuleCarrier")
	}
	if m != &embedded.Module {
		t.Fatalf("expected embedded core module pointer")
	}
}

func TestRootCoreModule(t *testing.T) {
	parent := &Module{}
	root := &testEmbeddedModule{}
	parent.root = root

	m, ok := parent.rootCoreModule()
	if !ok {
		t.Fatalf("expected root core module to be resolved")
	}
	if m != &root.Module {
		t.Fatalf("expected resolved root to be embedded core module")
	}
}

func TestRootCoreModuleNilRoot(t *testing.T) {
	parent := &Module{}
	m, ok := parent.rootCoreModule()
	if ok || m != nil {
		t.Fatalf("expected nil,false when root is nil")
	}
}
