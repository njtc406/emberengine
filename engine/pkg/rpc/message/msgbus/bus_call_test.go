package msgbus

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/monitor"
	"github.com/njtc406/emberengine/engine/pkg/utils/asynclib"
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

// testEnv bundles a MessageBusFactory with a real RpcMonitor + TimingWheel.
type testEnv struct {
	factory *MessageBusFactory
	monitor *monitor.RpcMonitor
	tw      *timingwheel.TimingWheel
	pool    *asynclib.Pool
}

func newTestEnv(t *testing.T, rpcTimeout time.Duration) *testEnv {
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

	f := NewMessageBusFactory(100, logger, rm, rpcTimeout)

	t.Cleanup(func() {
		rm.Stop()
		p.Release()
		tw.Stop()
	})
	return &testEnv{factory: f, monitor: rm, tw: tw, pool: p}
}

// replyDispatcher simulates a receiver that immediately replies with the given
// value through the RpcMonitor.
type replyDispatcher struct {
	pid     *actor.PID
	monitor *monitor.RpcMonitor
	reply   interface{} // response value
	replyOk bool        // whether to reply
}

func (d *replyDispatcher) PostJob(job inf.IMailboxJob) error { return nil }

func (d *replyDispatcher) DeliverRequest(_ context.Context, envelope inf.IEnvelope) error {
	if d.replyOk {
		reqId := envelope.GetMeta().GetReqId()
		state := d.monitor.Remove(reqId)
		if state != nil {
			state.SetResult(d.reply, nil)
			state.Complete()
		}
	}
	envelope.Release()
	return nil
}

func (d *replyDispatcher) DeliverResponse(_ context.Context, envelope inf.IEnvelope) error {
	return nil
}

func (d *replyDispatcher) SetPid(pid *actor.PID) { d.pid = pid }
func (d *replyDispatcher) GetPid() *actor.PID    { return d.pid }
func (d *replyDispatcher) Close()                {}
func (d *replyDispatcher) IsClosed() bool        { return false }

// errorDispatcher simulates a receiver whose DeliverRequest always fails.
type errorDispatcher struct {
	pid *actor.PID
	err error
}

func (d *errorDispatcher) PostJob(job inf.IMailboxJob) error { return nil }

func (d *errorDispatcher) DeliverRequest(_ context.Context, _ inf.IEnvelope) error {
	return d.err
}

func (d *errorDispatcher) DeliverResponse(_ context.Context, _ inf.IEnvelope) error {
	return nil
}

func (d *errorDispatcher) SetPid(pid *actor.PID) { d.pid = pid }
func (d *errorDispatcher) GetPid() *actor.PID    { return d.pid }
func (d *errorDispatcher) Close()                {}
func (d *errorDispatcher) IsClosed() bool        { return false }

func testPID(name string) *actor.PID {
	return actor.NewPID("127.0.0.1:0", "node1", 1, "svc1", "GameService", name, 1, "grpc")
}

// ---------------------------------------------------------------------------
// P3-1: Call tests
// ---------------------------------------------------------------------------

func TestCall_NormalReply(t *testing.T) {
	env := newTestEnv(t, time.Second)

	sender := &mockDispatcher{pid: testPID("Sender")}
	receiver := &replyDispatcher{
		pid:     testPID("Receiver"),
		monitor: env.monitor,
		reply:   42,
		replyOk: true,
	}

	mb := env.factory.New(sender, receiver, nil)
	var out int
	err := mb.CallWithOpt(context.Background(),
		dto.WithMethod("RpcSum"),
		dto.WithIn(nil),
		dto.WithOut(&out),
	)
	require.NoError(t, err)
	assert.Equal(t, 42, out)

	// Verify metrics
	m := env.factory.GetRpcMetrics()
	assert.Equal(t, int64(1), m.CallTotal)
	assert.Equal(t, int64(0), m.CallErrors)
	assert.Equal(t, int64(0), m.CallInFlight) // call completed
}

func TestCall_Timeout(t *testing.T) {
	env := newTestEnv(t, 30*time.Millisecond)

	sender := &mockDispatcher{pid: testPID("Sender")}
	// receiver accepts but never replies -> timeout
	receiver := &replyDispatcher{
		pid:     testPID("Receiver"),
		monitor: env.monitor,
		replyOk: false,
	}

	mb := env.factory.New(sender, receiver, nil)
	var out int
	err := mb.CallWithOpt(context.Background(),
		dto.WithMethod("RpcSlow"),
		dto.WithOut(&out),
	)
	require.Error(t, err)
	assert.True(t, errors.Is(err, def.ErrRPCCallTimeout), "expected ErrRPCCallTimeout, got %v", err)

	m := env.factory.GetRpcMetrics()
	assert.Equal(t, int64(1), m.CallTotal)
	assert.Equal(t, int64(1), m.CallErrors)
}

func TestCall_DeliverRequestFails(t *testing.T) {
	env := newTestEnv(t, time.Second)

	sender := &mockDispatcher{pid: testPID("Sender")}
	receiver := &errorDispatcher{
		pid: testPID("Receiver"),
		err: errors.New("connection refused"),
	}

	mb := env.factory.New(sender, receiver, nil)
	var out int
	err := mb.CallWithOpt(context.Background(),
		dto.WithMethod("RpcSum"),
		dto.WithOut(&out),
	)
	require.Error(t, err)
	assert.True(t, errors.Is(err, def.ErrRPCCallFailed), "expected ErrRPCCallFailed, got %v", err)

	m := env.factory.GetRpcMetrics()
	assert.Equal(t, int64(1), m.CallErrors)
}

func TestCall_SenderNil(t *testing.T) {
	env := newTestEnv(t, time.Second)

	mb := env.factory.New(nil, &mockDispatcher{pid: testPID("R")}, nil)
	err := mb.CallWithOpt(context.Background(), dto.WithMethod("RpcSum"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sender is nil")
}

func TestCall_ReceiverNil(t *testing.T) {
	env := newTestEnv(t, time.Second)

	mb := env.factory.New(&mockDispatcher{pid: testPID("S")}, nil, nil)
	err := mb.CallWithOpt(context.Background(), dto.WithMethod("RpcSum"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "receiver is nil")
}

func TestCall_PresetError(t *testing.T) {
	env := newTestEnv(t, time.Second)

	preset := errors.New("service not found")
	mb := env.factory.New(&mockDispatcher{pid: testPID("S")}, &mockDispatcher{pid: testPID("R")}, preset)
	err := mb.CallWithOpt(context.Background(), dto.WithMethod("RpcSum"))
	require.Error(t, err)
	assert.Equal(t, preset, err)
}

// execDispatcher simulates a dispatcher that captures PostJob calls.
type execDispatcher struct {
	pid     *actor.PID
	postCnt int
}

func (d *execDispatcher) PostJob(_ inf.IMailboxJob) error {
	d.postCnt++
	return nil
}

func (d *execDispatcher) DeliverRequest(_ context.Context, envelope inf.IEnvelope) error {
	return nil
}

func (d *execDispatcher) DeliverResponse(_ context.Context, envelope inf.IEnvelope) error {
	return nil
}

func (d *execDispatcher) SetPid(pid *actor.PID) { d.pid = pid }
func (d *execDispatcher) GetPid() *actor.PID    { return d.pid }
func (d *execDispatcher) Close()                {}
func (d *execDispatcher) IsClosed() bool        { return false }

// ---------------------------------------------------------------------------
// P3-1: AsyncCall tests
// ---------------------------------------------------------------------------

func TestAsyncCall_Normal(t *testing.T) {
	env := newTestEnv(t, time.Second)

	sender := &execDispatcher{pid: testPID("Sender")}
	receiver := &replyDispatcher{
		pid:     testPID("Receiver"),
		monitor: env.monitor,
		reply:   "hello",
		replyOk: true,
	}

	mb := env.factory.New(sender, receiver, nil)
	cancel, err := mb.AsyncCallWithOpt(context.Background(),
		dto.WithMethod("RpcAsync"),
		dto.WithIn(nil),
		dto.WithCallbacks(func(ctx context.Context, data interface{}, err error, params ...interface{}) {
			// callback would be invoked via Service mailbox, not tested here
		}),
	)
	require.NoError(t, err)
	assert.NotNil(t, cancel)
	// The reply triggers Complete() which posts the callback job to sender
	// Wait a bit for the async dispatch to complete
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, sender.postCnt, "callback job should be posted to sender")

	m := env.factory.GetRpcMetrics()
	assert.Equal(t, int64(1), m.AsyncCallTotal)
	assert.Equal(t, int64(0), m.AsyncCallErrors)
}

func TestAsyncCall_NoCallbacks(t *testing.T) {
	env := newTestEnv(t, time.Second)

	mb := env.factory.New(
		&mockDispatcher{pid: testPID("S")},
		&mockDispatcher{pid: testPID("R")},
		nil,
	)
	_, err := mb.AsyncCallWithOpt(context.Background(), dto.WithMethod("RpcAsync"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, def.ErrCallbacksIsEmpty))
}

func TestAsyncCall_DeliverRequestFails(t *testing.T) {
	env := newTestEnv(t, time.Second)

	sender := &mockDispatcher{pid: testPID("Sender")}
	receiver := &errorDispatcher{
		pid: testPID("Receiver"),
		err: errors.New("network down"),
	}

	mb := env.factory.New(sender, receiver, nil)
	_, err := mb.AsyncCallWithOpt(context.Background(),
		dto.WithMethod("RpcAsync"),
		dto.WithCallbacks(func(ctx context.Context, data interface{}, err error, params ...interface{}) {}),
	)
	require.Error(t, err)
	assert.True(t, errors.Is(err, def.ErrRPCCallFailed))

	m := env.factory.GetRpcMetrics()
	assert.Equal(t, int64(1), m.AsyncCallErrors)
}

// ---------------------------------------------------------------------------
// P3-1: Send tests
// ---------------------------------------------------------------------------

func TestSend_Normal(t *testing.T) {
	env := newTestEnv(t, time.Second)

	sender := &mockDispatcher{pid: testPID("Sender")}
	receiver := &replyDispatcher{
		pid:     testPID("Receiver"),
		monitor: env.monitor,
		replyOk: false, // send doesn't expect reply
	}

	mb := env.factory.New(sender, receiver, nil)
	err := mb.SendWithOpt(context.Background(), dto.WithMethod("FireEvent"), dto.WithIn("data"))
	require.NoError(t, err)

	m := env.factory.GetRpcMetrics()
	assert.Equal(t, int64(1), m.SendTotal)
	assert.Equal(t, int64(0), m.SendErrors)
}

func TestSend_ReceiverNil(t *testing.T) {
	env := newTestEnv(t, time.Second)

	mb := env.factory.New(&mockDispatcher{pid: testPID("S")}, nil, nil)
	err := mb.SendWithOpt(context.Background(), dto.WithMethod("Fire"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "receiver is nil")
}

func TestSend_DeliverRequestFails(t *testing.T) {
	env := newTestEnv(t, time.Second)

	sender := &mockDispatcher{pid: testPID("Sender")}
	receiver := &errorDispatcher{
		pid: testPID("Receiver"),
		err: errors.New("queue full"),
	}

	mb := env.factory.New(sender, receiver, nil)
	err := mb.SendWithOpt(context.Background(), dto.WithMethod("Fire"), dto.WithIn(nil))
	require.Error(t, err)

	m := env.factory.GetRpcMetrics()
	assert.Equal(t, int64(1), m.SendErrors)
}

func TestSend_PresetError(t *testing.T) {
	env := newTestEnv(t, time.Second)

	preset := errors.New("no target")
	mb := env.factory.New(&mockDispatcher{pid: testPID("S")}, &mockDispatcher{pid: testPID("R")}, preset)
	err := mb.SendWithOpt(context.Background(), dto.WithMethod("Fire"))
	require.Error(t, err)
	assert.Equal(t, preset, err)
}
