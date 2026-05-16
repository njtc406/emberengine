package gate

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/sysModule/gate/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// mock adapter
// ---------------------------------------------------------------------------

type mockAdapter struct {
	callCount atomic.Int64
	errFunc   func(int64) error // errFunc(callN) → error to return
	serveCh   chan struct{}     // closed after ListenAndServe returns
}

func (m *mockAdapter) ListenAndServe(_ inf.IModule, _ interface{}) error {
	n := m.callCount.Add(1)
	err := m.errFunc(n)
	if m.serveCh != nil {
		select {
		case <-m.serveCh:
		default:
			close(m.serveCh)
		}
	}
	return err
}

func (m *mockAdapter) Shutdown(_ context.Context) error    { return nil }
func (m *mockAdapter) SetSessionMgr(_ inf.ISessionManager) {}
func (m *mockAdapter) GetSessionMgr() inf.ISessionManager  { return nil }

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

func newTestGate(adapter *mockAdapter, policy RestartPolicy) *Gate {
	g := NewGate()
	g.restartPolicy = policy
	g.adapter = adapter
	return g
}

func TestSupervisor_TemporaryError_Restarts(t *testing.T) {
	tempErr := errors.New("temporary network glitch")
	calls := make(chan int64, 10)

	adapter := &mockAdapter{
		errFunc: func(n int64) error {
			calls <- n
			return tempErr
		},
	}

	policy := RestartPolicy{
		Enable:         true,
		MaxRestart:     3,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     50 * time.Millisecond,
	}

	g := newTestGate(adapter, policy)
	g.ctx, g.cancel = context.WithCancel(context.Background())
	g.serveDone = make(chan struct{})

	go g.superviseServe(nil)

	// Wait for supervisor to exhaust retries (initial + 3 restarts = 4 calls)
	select {
	case <-g.serveDone:
	case <-time.After(5 * time.Second):
		t.Fatal("supervisor did not stop after max restarts")
	}

	// 1 initial + MaxRestart(3) attempts = 4 total calls
	assert.Equal(t, int64(4), adapter.callCount.Load())
	assert.False(t, g.IsServing())
	assert.Equal(t, tempErr, g.LastServeError())
}

func TestSupervisor_PermanentError_NoRestart(t *testing.T) {
	// Simulate "address already in use" permanent error
	permErr := &net.OpError{Op: "listen", Err: errors.New("address already in use")}

	adapter := &mockAdapter{
		errFunc: func(_ int64) error { return permErr },
	}

	policy := RestartPolicy{
		Enable:         true,
		MaxRestart:     5,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     50 * time.Millisecond,
	}

	g := newTestGate(adapter, policy)
	g.ctx, g.cancel = context.WithCancel(context.Background())
	g.serveDone = make(chan struct{})

	go g.superviseServe(nil)

	select {
	case <-g.serveDone:
	case <-time.After(2 * time.Second):
		t.Fatal("supervisor did not stop for permanent error")
	}

	// Only 1 call, no restart for permanent errors
	assert.Equal(t, int64(1), adapter.callCount.Load())
}

func TestSupervisor_NormalShutdown_NoRestart(t *testing.T) {
	adapter := &mockAdapter{
		errFunc: func(_ int64) error { return nil },
	}

	policy := DefaultRestartPolicy()

	g := newTestGate(adapter, policy)
	g.ctx, g.cancel = context.WithCancel(context.Background())
	g.serveDone = make(chan struct{})

	go g.superviseServe(nil)

	select {
	case <-g.serveDone:
	case <-time.After(2 * time.Second):
		t.Fatal("supervisor did not stop on nil error")
	}

	assert.Equal(t, int64(1), adapter.callCount.Load())
	assert.Nil(t, g.LastServeError())
}

func TestSupervisor_CancelStopsLoop(t *testing.T) {
	// Adapter blocks until context is cancelled
	adapter := &mockAdapter{
		errFunc: func(_ int64) error {
			time.Sleep(100 * time.Millisecond)
			return errors.New("oops")
		},
	}

	policy := RestartPolicy{
		Enable:         true,
		MaxRestart:     100, // many retries
		InitialBackoff: 50 * time.Millisecond,
		MaxBackoff:     100 * time.Millisecond,
	}

	g := newTestGate(adapter, policy)
	g.ctx, g.cancel = context.WithCancel(context.Background())
	g.serveDone = make(chan struct{})

	go g.superviseServe(nil)

	// Let one attempt happen
	time.Sleep(200 * time.Millisecond)
	g.cancel()

	select {
	case <-g.serveDone:
	case <-time.After(2 * time.Second):
		t.Fatal("supervisor did not stop after cancel")
	}
}

func TestSupervisor_RestartDisabled(t *testing.T) {
	tempErr := errors.New("some error")
	adapter := &mockAdapter{
		errFunc: func(_ int64) error { return tempErr },
	}

	policy := RestartPolicy{
		Enable: false,
	}

	g := newTestGate(adapter, policy)
	g.ctx, g.cancel = context.WithCancel(context.Background())
	g.serveDone = make(chan struct{})

	go g.superviseServe(nil)

	select {
	case <-g.serveDone:
	case <-time.After(2 * time.Second):
		t.Fatal("supervisor did not stop when restart disabled")
	}

	assert.Equal(t, int64(1), adapter.callCount.Load())
}

func TestStart_NilAdapter(t *testing.T) {
	g := NewGate()
	err := g.Start(&config.GateService{Type: "ws", WSServerConf: &config.WSServerConf{}})
	require.NoError(t, err) // nil adapter → no-op
}

func TestStart_NilConf(t *testing.T) {
	g := NewGate()
	g.adapter = &mockAdapter{errFunc: func(_ int64) error { return nil }}
	err := g.Start(nil)
	require.Error(t, err)
}
