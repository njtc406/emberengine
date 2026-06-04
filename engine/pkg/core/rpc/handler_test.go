package rpc

import (
	"context"
	"testing"

	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
)

type HandlerReadOnlyDeclarerModule struct {
	inf.IModule
}

func (m *HandlerReadOnlyDeclarerModule) Debugf(format string, args ...interface{}) {}
func (m *HandlerReadOnlyDeclarerModule) Warnf(format string, args ...interface{})  {}

func (m *HandlerReadOnlyDeclarerModule) RpcGetUser(ctx context.Context) error { return nil }

func (m *HandlerReadOnlyDeclarerModule) ReadOnlyMethods() []string {
	return []string{"RpcGetUser", "RpcMissing"}
}

func TestIReadOnlyDeclarerMarksOnlyRegisteredMethods(t *testing.T) {
	logger, err := log.NewDefaultLogger(nil)
	if err != nil {
		t.Fatalf("NewDefaultLogger error: %v", err)
	}
	defer logger.Close()

	methodMgr := NewMethodMgr(logger, nil)
	module := &HandlerReadOnlyDeclarerModule{}

	if _, err := NewHandler(module).Init(methodMgr); err != nil {
		t.Fatalf("handler init failed: %v", err)
	}

	readOnlyMgr, ok := methodMgr.(inf.IReadOnlyMethodMgr)
	if !ok {
		t.Fatalf("method manager should implement IReadOnlyMethodMgr")
	}
	if !readOnlyMgr.IsReadOnly("RpcGetUser") {
		t.Fatalf("registered declared method should be marked read-only")
	}
	if readOnlyMgr.IsReadOnly("RpcMissing") {
		t.Fatalf("missing declared method should not be marked read-only")
	}
}
