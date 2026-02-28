package core

import (
	"testing"

	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

type testModuleLookupHierarchy struct {
	modules map[uint32]inf.IModule
}

func (h *testModuleLookupHierarchy) AddModule(module inf.IModule) (uint32, error) { return 0, nil }
func (h *testModuleLookupHierarchy) ReleaseAllChildModule()                       {}
func (h *testModuleLookupHierarchy) ReleaseModule(moduleID uint32)                {}
func (h *testModuleLookupHierarchy) GetModule(moduleID uint32) inf.IModule {
	if h == nil {
		return nil
	}
	return h.modules[moduleID]
}
func (h *testModuleLookupHierarchy) GetRoot() inf.IModule         { return nil }
func (h *testModuleLookupHierarchy) GetBaseModule() inf.IModule   { return nil }
func (h *testModuleLookupHierarchy) GetParent() inf.IModule       { return nil }
func (h *testModuleLookupHierarchy) GetMethodMgr() inf.IMethodMgr { return nil }

type testLookupModule struct {
	Module
}

type testLookupAPI interface {
	inf.IModule
	Custom() string
}

func (m *testLookupModule) Custom() string { return "ok" }

func TestGetModule_NilHierarchy(t *testing.T) {
	_, ok := GetModule[testLookupAPI](nil, 1)
	if ok {
		t.Fatalf("expected false when hierarchy is nil")
	}
}

func TestGetModule_NotFound(t *testing.T) {
	h := &testModuleLookupHierarchy{modules: map[uint32]inf.IModule{}}
	_, ok := GetModule[testLookupAPI](h, 42)
	if ok {
		t.Fatalf("expected false when module not found")
	}
}

func TestGetModule_TypeMismatch(t *testing.T) {
	h := &testModuleLookupHierarchy{modules: map[uint32]inf.IModule{1: &Module{}}}
	_, ok := GetModule[testLookupAPI](h, 1)
	if ok {
		t.Fatalf("expected false when type mismatch")
	}
}

func TestGetModule_Success(t *testing.T) {
	mod := &testLookupModule{}
	h := &testModuleLookupHierarchy{modules: map[uint32]inf.IModule{7: mod}}
	got, ok := GetModule[testLookupAPI](h, 7)
	if !ok {
		t.Fatalf("expected true when module type matches")
	}
	if got.Custom() != "ok" {
		t.Fatalf("unexpected custom result: %s", got.Custom())
	}
}
