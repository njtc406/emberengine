package msgbus

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/rpc/message/msgenvelope"
)

type mockDispatcher struct {
	pid    *actor.PID
	closed bool
}

func (m *mockDispatcher) PostJob(job inf.IMailboxJob) error { return nil }

func (m *mockDispatcher) DeliverRequest(ctx context.Context, envelope inf.IEnvelope) error {
	return nil
}

func (m *mockDispatcher) DeliverResponse(ctx context.Context, envelope inf.IEnvelope) error {
	return nil
}

func (m *mockDispatcher) SetPid(pid *actor.PID) { m.pid = pid }

func (m *mockDispatcher) GetPid() *actor.PID { return m.pid }

func (m *mockDispatcher) Close() { m.closed = true }

func (m *mockDispatcher) IsClosed() bool { return m.closed }

func TestValidateSingleOutParam(t *testing.T) {
	if _, _, err := validateSingleOutParam(1); err == nil {
		t.Fatalf("expected error for non-pointer out")
	}

	var p *int
	if _, _, err := validateSingleOutParam(p); err == nil {
		t.Fatalf("expected error for nil pointer out")
	}

	val := 0
	elem, typ, err := validateSingleOutParam(&val)
	if err != nil {
		t.Fatalf("expected valid pointer out, got err=%v", err)
	}
	if typ.Kind() != elem.Kind() {
		t.Fatalf("unexpected type/elem mismatch: typ=%v elem=%v", typ, elem.Kind())
	}
}

func TestAssignSingleOutCachedNilResponseSetsZero(t *testing.T) {
	v := 123
	elem, typ, err := validateSingleOutParam(&v)
	if err != nil {
		t.Fatalf("prepare out failed: %v", err)
	}

	if err := assignSingleOutCached(elem, typ, nil); err != nil {
		t.Fatalf("assign nil response failed: %v", err)
	}
	if v != 0 {
		t.Fatalf("expected zero value after nil response, got %d", v)
	}
}

func TestAssignCallResponseMulti(t *testing.T) {
	a, b := 0, ""
	outs := []interface{}{&a, &b}

	err := assignCallResponse(nil, []interface{}{1, "ok"}, true, outs, reflect.Value{}, nil)
	if err != nil {
		t.Fatalf("assign multi response failed: %v", err)
	}
	if a != 1 || b != "ok" {
		t.Fatalf("unexpected assigned values: a=%d b=%s", a, b)
	}
}

func TestAssignCallResponseMultiCountMismatch(t *testing.T) {
	a := 0
	outs := []interface{}{&a}

	err := assignCallResponse(nil, []interface{}{1, 2}, true, outs, reflect.Value{}, nil)
	if err == nil {
		t.Fatalf("expected count mismatch error")
	}
}

func TestCallFailsFastForInvalidOutBeforeRpcMonitor(t *testing.T) {
	mb := NewMessageBus(&mockDispatcher{}, &mockDispatcher{}, nil)
	data := msgenvelope.NewData()
	data.SetMethod("RpcSum")
	data.SetNeedResponse(true)

	err := mb.call(context.Background(), data, def.PriorityNormal, "", 123)
	if err == nil {
		t.Fatalf("expected out validation error")
	}
	if strings.Contains(err.Error(), "rpc monitor") {
		t.Fatalf("expected out validation to fail before rpc monitor check, got err=%v", err)
	}
}

func TestCallValidOutReturnsRpcMonitorError(t *testing.T) {
	mb := NewMessageBus(&mockDispatcher{}, &mockDispatcher{}, nil)
	data := msgenvelope.NewData()
	data.SetMethod("RpcSum")
	data.SetNeedResponse(true)
	out := 0

	err := mb.call(context.Background(), data, def.PriorityNormal, "", &out)
	if err == nil {
		t.Fatalf("expected rpc monitor not initialized error")
	}
	if !strings.Contains(err.Error(), "rpc monitor") {
		t.Fatalf("expected rpc monitor error, got %v", err)
	}
}

type fakeInternalBus struct {
	callErr    error
	callCount  int
	asyncErr   error
	asyncReqID uint64
	sendErr    error
}

func (f *fakeInternalBus) Call(ctx context.Context, method string, in, out interface{}) error {
	return f.callInternal(ctx, method, in, out, def.PriorityNormal, "", true)
}

func (f *fakeInternalBus) CallWithOpt(ctx context.Context, opts ...dto.BusOptionBuilder) error {
	option := dto.NewBusOption(opts...)
	return f.callInternal(ctx, option.Method, option.In, option.Out, option.Priority, option.DispatchKey, !option.NotRecycle)
}

func (f *fakeInternalBus) AsyncCall(ctx context.Context, method string, in interface{}, params *dto.AsyncCallParams, callbacks ...dto.CompletionFunc) (dto.CancelRpc, error) {
	if f.asyncErr != nil {
		return dto.EmptyCancelRpc, f.asyncErr
	}
	return dto.EmptyCancelRpc, nil
}

func (f *fakeInternalBus) AsyncCallWithOpt(ctx context.Context, opts ...dto.BusOptionBuilder) (dto.CancelRpc, error) {
	if f.asyncErr != nil {
		return dto.EmptyCancelRpc, f.asyncErr
	}
	return dto.EmptyCancelRpc, nil
}

func (f *fakeInternalBus) Send(ctx context.Context, method string, in interface{}) error {
	return f.sendErr
}

func (f *fakeInternalBus) SendWithOpt(ctx context.Context, opts ...dto.BusOptionBuilder) error {
	return f.sendErr
}

func (f *fakeInternalBus) Release() {}

func (f *fakeInternalBus) callInternal(ctx context.Context, method string, in, out interface{}, priority def.Priority, dispatchKey string, recycle bool) error {
	f.callCount++
	return f.callErr
}

func (f *fakeInternalBus) asyncCallInternal(ctx context.Context, data inf.IEnvelopeData, priority def.Priority, dispatchKey string, recycle bool, params *dto.AsyncCallParams, callbacks ...dto.CompletionFunc) (uint64, error) {
	if f.asyncErr != nil {
		return 0, f.asyncErr
	}
	if f.asyncReqID == 0 {
		f.asyncReqID = 1
	}
	return f.asyncReqID, nil
}

func (f *fakeInternalBus) sendInternal(ctx context.Context, data inf.IEnvelopeData, priority def.Priority, dispatchKey string, recycle bool) error {
	return f.sendErr
}

func TestMultiBusCallEmpty(t *testing.T) {
	var m MultiBus
	err := m.Call(context.Background(), "Api", nil, nil)
	if !errors.Is(err, def.ErrSelectEmptyResult) {
		t.Fatalf("expected ErrSelectEmptyResult, got %v", err)
	}
}

func TestMultiBusCallAnyStopsAfterSuccess(t *testing.T) {
	b1 := &fakeInternalBus{callErr: errors.New("first failed")}
	b2 := &fakeInternalBus{callErr: nil}
	b3 := &fakeInternalBus{callErr: nil}
	m := MultiBus{b1, b2, b3}

	err := m.Call(context.Background(), "Api", nil, nil)
	if err != nil {
		t.Fatalf("expected success when one bus succeeds, got %v", err)
	}
	if b1.callCount != 1 || b2.callCount != 1 {
		t.Fatalf("expected first two buses called once, got b1=%d b2=%d", b1.callCount, b2.callCount)
	}
	if b3.callCount != 0 {
		t.Fatalf("expected third bus not called after success, got %d", b3.callCount)
	}
}

func TestMultiBusCallWithOptAllAggregatesErrors(t *testing.T) {
	b1 := &fakeInternalBus{callErr: errors.New("e1")}
	b2 := &fakeInternalBus{callErr: nil}
	b3 := &fakeInternalBus{callErr: errors.New("e3")}
	m := MultiBus{b1, b2, b3}

	err := m.CallWithOpt(context.Background(), dto.WithMethod("ApiX"), dto.WithCallModeAll())
	if err == nil {
		t.Fatalf("expected combined error when some buses fail in CallModeAll")
	}
	if b1.callCount != 1 || b2.callCount != 1 || b3.callCount != 1 {
		t.Fatalf("expected all buses called once, got b1=%d b2=%d b3=%d", b1.callCount, b2.callCount, b3.callCount)
	}
}

func TestMultiBusCallWithOptAllSuccess(t *testing.T) {
	b1 := &fakeInternalBus{}
	b2 := &fakeInternalBus{}
	m := MultiBus{b1, b2}

	err := m.CallWithOpt(context.Background(), dto.WithMethod("ApiX"), dto.WithCallModeAll())
	if err != nil {
		t.Fatalf("expected nil when all buses succeed in CallModeAll, got %v", err)
	}
}

// --- P1-3.3: 错误传播补充测试 ---

func TestMultiBusAsyncCallEmpty(t *testing.T) {
	var m MultiBus
	_, err := m.AsyncCall(context.Background(), "Api", nil, nil)
	if !errors.Is(err, def.ErrSelectEmptyResult) {
		t.Fatalf("expected ErrSelectEmptyResult, got %v", err)
	}
}

func TestMultiBusAsyncCallWithOptEmpty(t *testing.T) {
	var m MultiBus
	_, err := m.AsyncCallWithOpt(context.Background(), dto.WithMethod("Api"))
	if !errors.Is(err, def.ErrSelectEmptyResult) {
		t.Fatalf("expected ErrSelectEmptyResult, got %v", err)
	}
}

func TestMultiBusSendEmpty(t *testing.T) {
	var m MultiBus
	err := m.Send(context.Background(), "Api", nil)
	if err != nil {
		t.Fatalf("Send on empty MultiBus should return nil, got %v", err)
	}
}

func TestMultiBusSendWithOptEmpty(t *testing.T) {
	var m MultiBus
	err := m.SendWithOpt(context.Background(), dto.WithMethod("Api"))
	if err != nil {
		t.Fatalf("SendWithOpt on empty MultiBus should return nil, got %v", err)
	}
}

func TestMultiBusSendAggregatesErrors(t *testing.T) {
	b1 := &fakeInternalBus{sendErr: errors.New("e1")}
	b2 := &fakeInternalBus{sendErr: nil}
	b3 := &fakeInternalBus{sendErr: errors.New("e3")}
	m := MultiBus{b1, b2, b3}

	err := m.Send(context.Background(), "Api", nil)
	if err == nil {
		t.Fatal("expected combined error")
	}
	if !strings.Contains(err.Error(), "e1") || !strings.Contains(err.Error(), "e3") {
		t.Fatalf("expected combined errors containing e1 and e3, got %v", err)
	}
}

func TestMultiBusSendAllSuccess(t *testing.T) {
	b1 := &fakeInternalBus{}
	b2 := &fakeInternalBus{}
	m := MultiBus{b1, b2}

	err := m.Send(context.Background(), "Api", nil)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}
