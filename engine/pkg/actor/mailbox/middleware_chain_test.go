package mailbox

import (
	"context"
	"errors"
	"testing"

	mbjob "github.com/njtc406/emberengine/engine/pkg/actor/mailbox/job"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

type panicMiddleware struct {
	name          string
	panicReceive  bool
	panicComplete bool
	completeCalls int
	cleanupCalls  int
}

func (m *panicMiddleware) Name() string { return m.name }
func (m *panicMiddleware) OnStart()     {}
func (m *panicMiddleware) OnStop()      {}
func (m *panicMiddleware) OnReceive(inf.IMiddlewareContext) dto.MiddlewareResult {
	if m.panicReceive {
		panic("receive boom")
	}
	return dto.Continue()
}
func (m *panicMiddleware) OnComplete(inf.IMiddlewareContext, error, interface{}) {
	m.completeCalls++
	if m.panicComplete {
		panic("complete boom")
	}
}
func (m *panicMiddleware) OnFrameworkCleanup(inf.IMiddlewareContext, error, interface{}) {
	m.cleanupCalls++
}

func TestMiddlewareChainRecoverOnReceive(t *testing.T) {
	mw := &panicMiddleware{name: "panic-receive", panicReceive: true}
	var recovered bool
	chain := NewMiddlewareChain(
		[]inf.IMailboxMiddleware{mw},
		WithPanicHandler(func(_, _ string, _ inf.IMiddlewareContext, _ interface{}) { recovered = true }),
	)

	job := mbjob.NewEventBusJob()
	defer job.Release()
	job.SetContext(context.Background())
	job.SetPriority(def.PriorityNormal)

	result, mctx := chain.ExecuteOnReceive(job, "svc")
	if result.Action != def.ActionReject {
		t.Fatalf("action = %v, want reject", result.Action)
	}
	if result.Err == nil {
		t.Fatal("expected panic converted to reject error")
	}
	if !recovered {
		t.Fatal("expected panic handler called")
	}
	chain.ExecuteOnComplete(mctx, result.Err, nil)
}

func TestMiddlewareChainRecoverOnCompleteAndContinue(t *testing.T) {
	first := &panicMiddleware{name: "first", panicComplete: true}
	second := &panicMiddleware{name: "second"}
	var recovered int
	chain := NewMiddlewareChain(
		[]inf.IMailboxMiddleware{first, second},
		WithPanicHandler(func(_, _ string, _ inf.IMiddlewareContext, _ interface{}) { recovered++ }),
	)

	job := mbjob.NewEventBusJob()
	defer job.Release()
	job.SetContext(context.Background())
	job.SetPriority(def.PriorityNormal)

	result, mctx := chain.ExecuteOnReceive(job, "svc")
	if result.Action != def.ActionContinue {
		t.Fatalf("action = %v, want continue", result.Action)
	}
	chain.ExecuteOnComplete(mctx, errors.New("exec failed"), nil)

	if recovered != 1 {
		t.Fatalf("recovered = %d, want 1", recovered)
	}
	if first.completeCalls != 1 || second.completeCalls != 1 {
		t.Fatalf("complete calls first=%d second=%d, want both 1", first.completeCalls, second.completeCalls)
	}
}

func TestMiddlewareChainFrameworkCleanupSkipsRegularOnComplete(t *testing.T) {
	mw := &panicMiddleware{name: "framework-cleanup"}
	chain := NewMiddlewareChain([]inf.IMailboxMiddleware{mw})

	job := mbjob.NewEventBusJob()
	defer job.Release()
	job.SetContext(context.Background())
	job.SetPriority(def.PriorityNormal)

	result, mctx := chain.ExecuteOnReceive(job, "svc")
	if result.Action != def.ActionContinue {
		t.Fatalf("action = %v, want continue", result.Action)
	}
	chain.ExecuteFrameworkCleanup(mctx, def.ErrMailboxNotRunning, nil)

	if mw.cleanupCalls != 1 {
		t.Fatalf("cleanupCalls = %d, want 1", mw.cleanupCalls)
	}
	if mw.completeCalls != 0 {
		t.Fatalf("completeCalls = %d, want 0", mw.completeCalls)
	}
}
