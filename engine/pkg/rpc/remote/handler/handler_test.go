package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/authz"
	"github.com/njtc406/emberengine/engine/pkg/config"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/monitor"
	"github.com/njtc406/emberengine/engine/pkg/utils/asynclib"
	"github.com/njtc406/emberengine/engine/pkg/utils/errorx"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// test helpers
// ---------------------------------------------------------------------------

func newTestLogger(t *testing.T) log.ILoggerX {
	t.Helper()
	l, err := log.NewLogger(&log.LoggerConf{Stdout: true, Caller: false, Color: false, Level: "debug"}, true)
	require.NoError(t, err)
	return l
}

func newTestMonitor(t *testing.T) (*monitor.RpcMonitor, func()) {
	t.Helper()
	logger := newTestLogger(t)
	tw := timingwheel.NewTimingWheel(time.Millisecond, 64, logger)
	tw.Start()
	p, err := asynclib.NewPool(32)
	require.NoError(t, err)
	rm := monitor.NewRpcMonitor().Init(
		&config.RpcMonitorConf{MonitorTimerSize: 1000, MonitorBucketSize: 20},
		logger, tw, p,
	)
	require.NoError(t, rm.Start())
	return rm, func() { rm.Stop(); p.Release(); tw.Stop() }
}

func testPID(name string) *actor.PID {
	return actor.NewPID("127.0.0.1:0", "node1", 1, "svc1", "GameService", name, 1, "grpc")
}

// mockDedup implements IDeDuplicator for testing.
type mockDedup struct {
	seen    map[string]map[uint64]bool
	seenKey map[string]bool
}

func newMockDedup() *mockDedup {
	return &mockDedup{seen: make(map[string]map[uint64]bool), seenKey: make(map[string]bool)}
}

func (d *mockDedup) Seen(serviceUid string, id uint64) bool {
	if d.seen[serviceUid] == nil {
		d.seen[serviceUid] = make(map[uint64]bool)
	}
	if d.seen[serviceUid][id] {
		return true
	}
	d.seen[serviceUid][id] = true
	return false
}

func (d *mockDedup) SeenKey(key string) bool {
	if d.seenKey[key] {
		return true
	}
	d.seenKey[key] = true
	return false
}

func (d *mockDedup) Close() {}

// mockDispatcher captures DeliverRequest calls.
type mockDispatcher struct {
	pid       *actor.PID
	delivered []inf.IEnvelope
	err       error
}

func (m *mockDispatcher) PostJob(_ inf.IMailboxJob) error { return nil }
func (m *mockDispatcher) DeliverRequest(_ context.Context, env inf.IEnvelope) error {
	if m.err != nil {
		return m.err
	}
	m.delivered = append(m.delivered, env)
	return nil
}
func (m *mockDispatcher) DeliverResponse(_ context.Context, _ inf.IEnvelope) error { return nil }
func (m *mockDispatcher) SetPid(pid *actor.PID)                                    { m.pid = pid }
func (m *mockDispatcher) GetPid() *actor.PID                                       { return m.pid }
func (m *mockDispatcher) Close()                                                   {}
func (m *mockDispatcher) IsClosed() bool                                           { return false }

// mockSenderFactory returns dispatchers by PID.
type mockSenderFactory struct {
	dispatchers map[string]inf.IRpcDispatcher
}

func (f *mockSenderFactory) GetDispatcher(pid *actor.PID) inf.IRpcDispatcher {
	if pid == nil {
		return nil
	}
	return f.dispatchers[pid.GetServiceUid()]
}

// ---------------------------------------------------------------------------
// P3-2: Reply tests
// ---------------------------------------------------------------------------

func TestReply_MatchesCallState(t *testing.T) {
	rm, cleanup := newTestMonitor(t)
	defer cleanup()

	h := NewHandler(rm, newTestLogger(t), nil)

	// Create a pending call state
	reqId := rm.GenSeq()
	senderPid := testPID("Sender")
	state := monitor.NewCallState(context.Background(), reqId, "TestMethod", time.Second, nil, nil, nil)
	rm.Add(state)

	// Build reply message
	msg := &actor.Message{
		SenderPid:   testPID("Receiver"),
		ReceiverPid: senderPid,
		Reply:       true,
		ReqId:       reqId,
		Method:      "TestMethod",
	}

	err := h.RpcMessageHandler(nil, msg)
	require.NoError(t, err)

	// state.Wait should return immediately since Complete was called
	state.Wait()
	assert.NoError(t, state.Error())
	state.Release()
}

func TestReply_LateReplyDropped(t *testing.T) {
	rm, cleanup := newTestMonitor(t)
	defer cleanup()

	h := NewHandler(rm, newTestLogger(t), nil)

	// Reply for a non-existent state (already timed out / removed)
	msg := &actor.Message{
		Reply: true,
		ReqId: 99999,
	}

	err := h.RpcMessageHandler(nil, msg)
	require.NoError(t, err) // should not error, just drop
}

func TestReply_WithErrorBytes(t *testing.T) {
	rm, cleanup := newTestMonitor(t)
	defer cleanup()

	h := NewHandler(rm, newTestLogger(t), nil)

	reqId := rm.GenSeq()
	state := monitor.NewCallState(context.Background(), reqId, "TestErr", time.Second, nil, nil, nil)
	rm.Add(state)

	// Serialize an error using errorx
	original := errorx.New(1234, "test error")
	errBytes := errorx.MarshalToBytes(original)

	msg := &actor.Message{
		Reply: true,
		ReqId: reqId,
		Err:   errBytes,
	}

	err := h.RpcMessageHandler(nil, msg)
	require.NoError(t, err)

	state.Wait()
	assert.Error(t, state.Error())
	assert.Contains(t, state.Error().Error(), "test error")
	state.Release()
}

// ---------------------------------------------------------------------------
// P3-2: Request tests
// ---------------------------------------------------------------------------

func TestRequest_Normal(t *testing.T) {
	rm, cleanup := newTestMonitor(t)
	defer cleanup()

	receiverPid := testPID("Receiver")
	receiver := &mockDispatcher{pid: receiverPid}

	sf := &mockSenderFactory{
		dispatchers: map[string]inf.IRpcDispatcher{
			receiverPid.GetServiceUid(): receiver,
		},
	}

	h := NewHandler(rm, newTestLogger(t), newMockDedup())

	senderPid := testPID("Sender")
	msg := &actor.Message{
		SenderPid:   senderPid,
		ReceiverPid: receiverPid,
		Reply:       false,
		ReqId:       rm.GenSeq(),
		Method:      "RpcSum",
		NeedResp:    true,
		ContextHeaders: map[string]string{
			"ember.traceId": "abc123",
		},
	}

	err := h.RpcMessageHandler(sf, msg)
	require.NoError(t, err)
	assert.Len(t, receiver.delivered, 1)
}

func TestRequest_DedupHit(t *testing.T) {
	rm, cleanup := newTestMonitor(t)
	defer cleanup()

	receiverPid := testPID("Receiver")
	receiver := &mockDispatcher{pid: receiverPid}

	sf := &mockSenderFactory{
		dispatchers: map[string]inf.IRpcDispatcher{
			receiverPid.GetServiceUid(): receiver,
		},
	}

	dedup := newMockDedup()
	h := NewHandler(rm, newTestLogger(t), dedup)

	senderPid := testPID("Sender")
	msg := &actor.Message{
		SenderPid:      senderPid,
		ReceiverPid:    receiverPid,
		Reply:          false,
		ReqId:          rm.GenSeq(),
		Method:         "RpcSum",
		NeedResp:       true,
		IdempotencyKey: "rpc:sum:42",
	}

	// First call: should be delivered
	err := h.RpcMessageHandler(sf, msg)
	require.NoError(t, err)
	assert.Len(t, receiver.delivered, 1)

	// Second call with a different ReqId but the same idempotency key: should be deduped.
	msg2 := &actor.Message{
		SenderPid:      senderPid,
		ReceiverPid:    receiverPid,
		Reply:          false,
		ReqId:          rm.GenSeq(),
		Method:         "RpcSum",
		NeedResp:       true,
		IdempotencyKey: "rpc:sum:42",
	}
	err = h.RpcMessageHandler(sf, msg2)
	require.NoError(t, err)
	assert.Len(t, receiver.delivered, 1) // still 1, second was dropped
}

func TestRequest_SendSkipsDedup(t *testing.T) {
	rm, cleanup := newTestMonitor(t)
	defer cleanup()

	receiverPid := testPID("Receiver")
	receiver := &mockDispatcher{pid: receiverPid}

	sf := &mockSenderFactory{
		dispatchers: map[string]inf.IRpcDispatcher{
			receiverPid.GetServiceUid(): receiver,
		},
	}

	h := NewHandler(rm, newTestLogger(t), newMockDedup())
	senderPid := testPID("Sender")

	msg := &actor.Message{
		SenderPid:   senderPid,
		ReceiverPid: receiverPid,
		Reply:       false,
		ReqId:       0,
		Method:      "FireEvent",
		NeedResp:    false,
	}

	// Two sends without idempotency key should both be delivered.
	err := h.RpcMessageHandler(sf, msg)
	require.NoError(t, err)
	err = h.RpcMessageHandler(sf, msg)
	require.NoError(t, err)
	assert.Len(t, receiver.delivered, 2)
}

func TestRequest_SameReqIDWithoutIdempotencyKeyNotDeduped(t *testing.T) {
	rm, cleanup := newTestMonitor(t)
	defer cleanup()

	receiverPid := testPID("Receiver")
	receiver := &mockDispatcher{pid: receiverPid}
	sf := &mockSenderFactory{dispatchers: map[string]inf.IRpcDispatcher{receiverPid.GetServiceUid(): receiver}}
	h := NewHandler(rm, newTestLogger(t), newMockDedup())

	reqID := rm.GenSeq()
	msg := &actor.Message{
		SenderPid:   testPID("Sender"),
		ReceiverPid: receiverPid,
		Reply:       false,
		ReqId:       reqID,
		Method:      "RpcSum",
		NeedResp:    true,
	}

	err := h.RpcMessageHandler(sf, msg)
	require.NoError(t, err)
	err = h.RpcMessageHandler(sf, msg)
	require.NoError(t, err)
	assert.Len(t, receiver.delivered, 2)
}

func TestRequest_DedupKeyDoesNotAppendSenderPid(t *testing.T) {
	rm, cleanup := newTestMonitor(t)
	defer cleanup()

	receiverPid := testPID("Receiver")
	receiver := &mockDispatcher{pid: receiverPid}
	sf := &mockSenderFactory{dispatchers: map[string]inf.IRpcDispatcher{receiverPid.GetServiceUid(): receiver}}
	h := NewHandler(rm, newTestLogger(t), newMockDedup())

	msg1 := &actor.Message{
		SenderPid:      testPID("Sender1"),
		ReceiverPid:    receiverPid,
		Reply:          false,
		ReqId:          rm.GenSeq(),
		Method:         "RpcSum",
		NeedResp:       true,
		IdempotencyKey: "order:create:9",
	}
	msg2 := &actor.Message{
		SenderPid:      testPID("Sender2"),
		ReceiverPid:    receiverPid,
		Reply:          false,
		ReqId:          rm.GenSeq(),
		Method:         "RpcSum",
		NeedResp:       true,
		IdempotencyKey: "order:create:9",
	}

	err := h.RpcMessageHandler(sf, msg1)
	require.NoError(t, err)
	err = h.RpcMessageHandler(sf, msg2)
	require.NoError(t, err)
	assert.Len(t, receiver.delivered, 1)
}

func TestRequest_DedupNil(t *testing.T) {
	rm, cleanup := newTestMonitor(t)
	defer cleanup()

	receiverPid := testPID("Receiver")
	receiver := &mockDispatcher{pid: receiverPid}

	sf := &mockSenderFactory{
		dispatchers: map[string]inf.IRpcDispatcher{
			receiverPid.GetServiceUid(): receiver,
		},
	}

	// Handler with nil dedup
	h := NewHandler(rm, newTestLogger(t), nil)

	msg := &actor.Message{
		SenderPid:      testPID("Sender"),
		ReceiverPid:    receiverPid,
		Reply:          false,
		ReqId:          rm.GenSeq(),
		Method:         "RpcSum",
		NeedResp:       true,
		IdempotencyKey: "rpc:sum:dedup-nil",
	}

	err := h.RpcMessageHandler(sf, msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deduplicator is nil")
	assert.Len(t, receiver.delivered, 0)
}

func TestRequest_SenderPidRequired(t *testing.T) {
	rm, cleanup := newTestMonitor(t)
	defer cleanup()

	receiverPid := testPID("Receiver")
	receiver := &mockDispatcher{pid: receiverPid}
	sf := &mockSenderFactory{dispatchers: map[string]inf.IRpcDispatcher{receiverPid.GetServiceUid(): receiver}}
	h := NewHandler(rm, newTestLogger(t), newMockDedup())

	msg := &actor.Message{
		ReceiverPid: receiverPid,
		Reply:       false,
		Method:      "FireEvent",
		NeedResp:    false,
	}

	err := h.RpcMessageHandler(sf, msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sender pid is nil")
	assert.Len(t, receiver.delivered, 0)
}

func TestRequest_DeliverFails(t *testing.T) {
	rm, cleanup := newTestMonitor(t)
	defer cleanup()

	receiverPid := testPID("Receiver")
	receiver := &mockDispatcher{pid: receiverPid, err: errors.New("mailbox full")}

	sf := &mockSenderFactory{
		dispatchers: map[string]inf.IRpcDispatcher{
			receiverPid.GetServiceUid(): receiver,
		},
	}

	h := NewHandler(rm, newTestLogger(t), newMockDedup())

	msg := &actor.Message{
		SenderPid:   testPID("Sender"),
		ReceiverPid: receiverPid,
		Reply:       false,
		ReqId:       rm.GenSeq(),
		Method:      "RpcSum",
		NeedResp:    true,
	}

	err := h.RpcMessageHandler(sf, msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mailbox full")
}

// ---------------------------------------------------------------------------
// D1: Authorization middleware tests
// ---------------------------------------------------------------------------

func TestAuthz_Disabled_AllowsAll(t *testing.T) {
	rm, cleanup := newTestMonitor(t)
	defer cleanup()

	receiverPid := testPID("Receiver")
	receiver := &mockDispatcher{pid: receiverPid}
	sf := &mockSenderFactory{
		dispatchers: map[string]inf.IRpcDispatcher{
			receiverPid.GetServiceUid(): receiver,
		},
	}

	// Authorizer exists but not enabled → all requests pass
	a := authz.NewAuthorizer()
	h := NewHandler(rm, newTestLogger(t), newMockDedup())
	h.SetAuthorizer(a)

	msg := &actor.Message{
		SenderPid:   testPID("Sender"),
		ReceiverPid: receiverPid,
		ReqId:       0,
		Method:      "AnyMethod",
		NeedResp:    false,
	}

	err := h.RpcMessageHandler(sf, msg)
	require.NoError(t, err)
	assert.Len(t, receiver.delivered, 1)
}

func TestAuthz_NilAuthorizer_AllowsAll(t *testing.T) {
	rm, cleanup := newTestMonitor(t)
	defer cleanup()

	receiverPid := testPID("Receiver")
	receiver := &mockDispatcher{pid: receiverPid}
	sf := &mockSenderFactory{
		dispatchers: map[string]inf.IRpcDispatcher{
			receiverPid.GetServiceUid(): receiver,
		},
	}

	// No authorizer set → pass-through
	h := NewHandler(rm, newTestLogger(t), newMockDedup())

	msg := &actor.Message{
		SenderPid:   testPID("Sender"),
		ReceiverPid: receiverPid,
		ReqId:       0,
		Method:      "AnyMethod",
		NeedResp:    false,
	}

	err := h.RpcMessageHandler(sf, msg)
	require.NoError(t, err)
	assert.Len(t, receiver.delivered, 1)
}

func TestAuthz_Enabled_Denied(t *testing.T) {
	rm, cleanup := newTestMonitor(t)
	defer cleanup()

	receiverPid := testPID("Receiver")
	receiver := &mockDispatcher{pid: receiverPid}
	sf := &mockSenderFactory{
		dispatchers: map[string]inf.IRpcDispatcher{
			receiverPid.GetServiceUid(): receiver,
		},
	}

	// Enable authz but don't grant any permissions to senderPid's ServiceType
	a := authz.NewAuthorizer()
	a.Enable(true)
	h := NewHandler(rm, newTestLogger(t), newMockDedup())
	h.SetAuthorizer(a)

	msg := &actor.Message{
		SenderPid:   testPID("Sender"),
		ReceiverPid: receiverPid,
		ReqId:       0,
		Method:      "RpcSecret",
		NeedResp:    false,
	}

	err := h.RpcMessageHandler(sf, msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authz")
	assert.Len(t, receiver.delivered, 0)
}

func TestAuthz_Enabled_Allowed(t *testing.T) {
	rm, cleanup := newTestMonitor(t)
	defer cleanup()

	receiverPid := testPID("Receiver")
	receiver := &mockDispatcher{pid: receiverPid}
	sf := &mockSenderFactory{
		dispatchers: map[string]inf.IRpcDispatcher{
			receiverPid.GetServiceUid(): receiver,
		},
	}

	// Setup: sender PID has ServiceType "GameService" (from testPID helper)
	// Grant GameService access to Receiver.*
	a := authz.NewAuthorizer()
	a.Enable(true)
	a.AddRole("game", []string{receiverPid.GetName() + ".*"})
	a.BindRole("game", testPID("Sender").GetServiceType())

	h := NewHandler(rm, newTestLogger(t), newMockDedup())
	h.SetAuthorizer(a)

	msg := &actor.Message{
		SenderPid:   testPID("Sender"),
		ReceiverPid: receiverPid,
		ReqId:       0,
		Method:      "RpcSum",
		NeedResp:    false,
	}

	err := h.RpcMessageHandler(sf, msg)
	require.NoError(t, err)
	assert.Len(t, receiver.delivered, 1)
}
