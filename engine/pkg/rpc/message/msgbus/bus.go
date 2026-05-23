// Package msgbus
// @Title  消息总线
// @Description  所有的消息都通过该模块进行发送
// @Author  yr  2024/11/12
// @Update  yr  2024/11/12
package msgbus

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/monitor"
	"github.com/njtc406/emberengine/engine/pkg/rpc/message/msgenvelope"
	"github.com/njtc406/emberengine/engine/pkg/utils/errorlib"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
	"github.com/njtc406/emberengine/engine/pkg/utils/timelib"
	"github.com/njtc406/emberengine/engine/pkg/utils/xcontext"
)

type MessageBusFactory struct {
	pool       pool.IPool[*MessageBus]
	logger     log.ILoggerX
	rpcMonitor *monitor.RpcMonitor
	rpcTimeout time.Duration
	metrics    rpcMetricsCollector
}

func NewMessageBusFactory(poolSize int, logger log.ILoggerX, rm *monitor.RpcMonitor, timeout time.Duration) *MessageBusFactory {
	if poolSize <= 0 {
		poolSize = 10000
	}
	if timeout <= 0 {
		timeout = def.DefaultRpcTimeout
	}
	f := &MessageBusFactory{
		logger:     logger,
		rpcMonitor: rm,
		rpcTimeout: timeout,
	}
	f.pool = newBusPoolWithFactory(poolSize, f)
	return f
}

func (f *MessageBusFactory) New(sender inf.IRpcDispatcher, receiver inf.IRpcDispatcher, err error) *MessageBus {
	mb := f.pool.Get()
	mb.factory = f
	mb.sender = sender
	mb.receiver = receiver
	mb.err = err
	return mb
}

func (f *MessageBusFactory) Put(mb *MessageBus) {
	if mb == nil {
		return
	}
	f.pool.Put(mb)
}

// GetRpcMetrics 返回当前 RPC 聚合指标快照。
func (f *MessageBusFactory) GetRpcMetrics() RpcMetrics {
	return f.metrics.snapshot()
}

// newBusPool 创建 MessageBus 对象池实例
func newBusPool(poolSize int) pool.IPool[*MessageBus] {
	return newBusPoolWithFactory(poolSize, nil)
}

func newBusPoolWithFactory(poolSize int, factory *MessageBusFactory) pool.IPool[*MessageBus] {
	return pool.NewPerPPoolWrapper(
		poolSize,
		func() *MessageBus {
			return &MessageBus{factory: factory}
		},
		pool.NewStatsRecorder("busPool"),
		pool.WithPRef(func(t *MessageBus) {
			t.Ref()
		}),
		pool.WithPUnref(func(t *MessageBus) bool {
			return t.UnRef()
		}),
		pool.WithPReset(func(mb *MessageBus) {
			mb.Reset()
		}),
	)
}

type MessageBus struct {
	dto.DataRef
	sender   inf.IRpcDispatcher
	receiver inf.IRpcDispatcher
	err      error
	factory  *MessageBusFactory
}

func (mb *MessageBus) Reset() {
	mb.sender = nil
	mb.receiver = nil
	mb.err = nil
}

// NewMessageBus 创建一个消息总线
//
// tips: 非常重要! 请勿跨协程传递,否则会导致pool错误,具体参考NewPerPPoolWrapper注释
func NewMessageBus(sender inf.IRpcDispatcher, receiver inf.IRpcDispatcher, err error) *MessageBus {
	return &MessageBus{sender: sender, receiver: receiver, err: err}
}

func ReleaseMessageBus(mb *MessageBus) {
	if mb != nil && mb.factory != nil {
		mb.factory.Put(mb)
	}
}

func (mb *MessageBus) getLogger() log.ILoggerX {
	if mb != nil && mb.factory != nil && mb.factory.logger != nil {
		return mb.factory.logger
	}
	return nil
}

func (mb *MessageBus) logErrorf(ctx context.Context, format string, args ...interface{}) {
	if l := mb.getLogger(); l != nil {
		l.WithContext(ctx).Errorf(format, args...)
	}
}

func (mb *MessageBus) logWarnf(ctx context.Context, format string, args ...interface{}) {
	if l := mb.getLogger(); l != nil {
		l.WithContext(ctx).Warnf(format, args...)
	}
}

func (mb *MessageBus) getRpcTimeout() time.Duration {
	if mb != nil && mb.factory != nil && mb.factory.rpcTimeout > 0 {
		return mb.factory.rpcTimeout
	}
	return def.DefaultRpcTimeout
}

func (mb *MessageBus) requireRpcMonitor(ctx context.Context) (*monitor.RpcMonitor, error) {
	if mb != nil && mb.factory != nil && mb.factory.rpcMonitor != nil {
		return mb.factory.rpcMonitor, nil
	}
	err := fmt.Errorf("msgbus rpc monitor not initialized")
	if l := mb.getLogger(); l != nil {
		l.WithContext(ctx).Error(err.Error())
	}
	return nil, err
}

func (mb *MessageBus) GetReceiverPid() *actor.PID {
	return mb.receiver.GetPid()
}

func validateSingleOutParam(out interface{}) (reflect.Value, reflect.Type, error) {
	outVal := reflect.ValueOf(out)
	if !outVal.IsValid() || outVal.Kind() != reflect.Ptr || outVal.IsNil() {
		return reflect.Value{}, nil, fmt.Errorf("single out call: out param must be non-nil pointer, but got %T", out)
	}
	outElem := outVal.Elem()
	return outElem, outElem.Type(), nil
}

func validateMultiOutParams(outs []interface{}) error {
	for idx := range outs {
		if _, _, err := validateSingleOutParam(outs[idx]); err != nil {
			return fmt.Errorf("multi out call: invalid out param at index %d: %w", idx, err)
		}
	}
	return nil
}

func assignSingleOutCached(outElem reflect.Value, outType reflect.Type, resp interface{}) error {
	respVal := reflect.ValueOf(resp)
	if !respVal.IsValid() {
		outElem.Set(reflect.Zero(outType))
		return nil
	}

	if respVal.Kind() == reflect.Ptr {
		if respVal.IsNil() {
			outElem.Set(reflect.Zero(outType))
			return nil
		}
		respVal = respVal.Elem()
	}

	if outType != respVal.Type() {
		return fmt.Errorf("call: type not match3, expected %v but got %v", respVal.Type(), outType)
	}

	outElem.Set(respVal)
	return nil
}

func assignSingleOut(out interface{}, resp interface{}) error {
	outElem, outType, err := validateSingleOutParam(out)
	if err != nil {
		return err
	}
	return assignSingleOutCached(outElem, outType, resp)
}

func assignCallResponse(out interface{}, resp interface{}, isMulti bool, multiOuts []interface{}, singleOutElem reflect.Value, singleOutType reflect.Type) error {
	if isMulti {
		respList, ok := resp.([]interface{})
		if !ok {
			return fmt.Errorf("call: type not match, expected %v but got %v", reflect.TypeOf(resp), reflect.TypeOf(out))
		}
		if len(multiOuts) != len(respList) {
			return fmt.Errorf("call: multi out count not match, expected %d but got %d", len(respList), len(multiOuts))
		}
		for idx := range multiOuts {
			if err := assignSingleOut(multiOuts[idx], respList[idx]); err != nil {
				return fmt.Errorf("multi out call: invalid out param at index %d: %w", idx, err)
			}
		}
		return nil
	}

	return assignSingleOutCached(singleOutElem, singleOutType, resp)
}

func (mb *MessageBus) call(ctx context.Context, data inf.IEnvelopeData, priority def.Priority, dispatchKey string, idempotencyKey string, out interface{}) (callErr error) {
	if mb.err != nil {
		// 这里可能是从MultiBus中产生的
		return mb.err
	}
	if mb.sender == nil {
		return fmt.Errorf("sender is nil")
	}
	if mb.receiver == nil {
		return fmt.Errorf("receiver is nil")
	}

	// RPC metrics: call in-flight & total
	if mb.factory != nil {
		mb.factory.metrics.callTotal.Add(1)
		mb.factory.metrics.callInFlight.Add(1)
		defer func() {
			mb.factory.metrics.callInFlight.Add(-1)
			if callErr != nil {
				mb.factory.metrics.callErrors.Add(1)
			}
		}()
	}

	var (
		isMulti       bool
		multiOuts     []interface{}
		singleOutElem reflect.Value
		singleOutType reflect.Type
		err           error
	)
	if out != nil {
		if outs, ok := out.([]interface{}); ok {
			isMulti = true
			multiOuts = outs
			if err = validateMultiOutParams(multiOuts); err != nil {
				return err
			}
		} else {
			singleOutElem, singleOutType, err = validateSingleOutParam(out)
			if err != nil {
				return err
			}
		}
	}

	var timeout time.Duration
	rpcTimeoutValue := mb.getRpcTimeout()
	deadline := timelib.Now().Add(rpcTimeoutValue)
	ok := false
	if ctx != nil {
		deadline, ok = ctx.Deadline()
		if ok {
			timeout = time.Until(deadline)
		}
	}

	if timeout <= 0 {
		timeout = rpcTimeoutValue
	}

	newCtx := xcontext.NewWithCloneCtx(ctx)

	mt, err := mb.requireRpcMonitor(newCtx)
	if err != nil {
		return err
	}
	reqId := mt.GenSeq()
	state := monitor.NewCallState(newCtx, reqId, data.GetMethod(), timeout, mb.sender, nil, nil)

	// 创建请求
	envelope := msgenvelope.NewMsgEnvelope()
	envelope.SetData(data)
	envelope.SetPriority(priority)
	envelope.SetDispatchKey(dispatchKey)

	meta := msgenvelope.NewMeta()
	meta.SetReqId(reqId)
	meta.SetSenderPid(mb.sender.GetPid())
	meta.SetReceiverPid(mb.receiver.GetPid())
	meta.SetDispatcher(mb.sender)
	meta.SetDeadline(deadline.UnixNano())
	meta.SetIdempotencyKey(idempotencyKey)
	envelope.SetMeta(meta)

	// getLogger().WithContext(newCtx).Debugf("call envelope: %+v", envelope)

	// 加入等待队列
	mt.Add(state)

	// 发送消息：调用后 envelope 所有权转移，由对端 mailbox 或 sender 负责 Release
	if err := mb.receiver.DeliverRequest(newCtx, envelope); err != nil {
		_ = mt.Remove(reqId)
		state.Release()
		envelope.Release()
		mb.logErrorf(newCtx,
			"service[%s] send message[%s] request to client failed, error: %v",
			mb.sender.GetPid().GetName(),
			data.GetMethod(),
			err,
		)
		return def.ErrRPCCallFailed
	}

	// 等待回复
	state.Wait()

	if err := state.Error(); err != nil {
		state.Release()
		return err
	}

	resp := state.Response()
	state.Release()

	// 如果out为nil表示丢弃返回值
	if out == nil {
		return nil
	}

	return assignCallResponse(out, resp, isMulti, multiOuts, singleOutElem, singleOutType)
}

// Call 同步调用服务
func (mb *MessageBus) Call(ctx context.Context, method string, in, out interface{}) error {
	defer ReleaseMessageBus(mb)
	if mb.err != nil {
		return mb.err
	}
	data := msgenvelope.NewData()
	data.SetMethod(method)
	data.SetRequest(in)
	data.SetResponse(nil)
	data.SetNeedResponse(true)
	return mb.call(ctx, data, def.PriorityNormal, "", "", out)
}

func (mb *MessageBus) CallWithOpt(ctx context.Context, opts ...dto.BusOptionBuilder) error {
	option := dto.NewBusOption(opts...)
	if !option.NotRecycle {
		defer ReleaseMessageBus(mb)
	}
	option.Ctx = ctx

	if mb.err != nil {
		return mb.err
	}
	data := msgenvelope.NewData()
	data.SetMethod(option.Method)
	data.SetRequest(option.In)
	data.SetResponse(nil)
	data.SetNeedResponse(true)

	return mb.call(option.Ctx, data, option.Priority, option.DispatchKey, option.IdempotencyKey, option.Out)
}

// callInternal 供MultiBus使用的内部方法（会自动释放）
func (mb *MessageBus) callInternal(ctx context.Context, method string, in, out interface{}, priority def.Priority, dispatchKey string, idempotencyKey string, recycle bool) error {
	if recycle {
		defer ReleaseMessageBus(mb)
	}
	if mb.err != nil {
		return mb.err
	}
	data := msgenvelope.NewData()
	data.SetMethod(method)
	data.SetRequest(in)
	data.SetResponse(nil)
	data.SetNeedResponse(true)
	return mb.call(ctx, data, priority, dispatchKey, idempotencyKey, out)
}

func (mb *MessageBus) asyncCall(ctx context.Context, data inf.IEnvelopeData, priority def.Priority, dispatchKey string, idempotencyKey string, param *dto.AsyncCallParams, callbacks ...dto.CompletionFunc) (_ uint64,
	asyncErr error) {
	// RPC metrics: async call total & errors
	if mb.factory != nil {
		mb.factory.metrics.asyncCallTotal.Add(1)
		defer func() {
			if asyncErr != nil {
				mb.factory.metrics.asyncCallErrors.Add(1)
			}
		}()
	}

	var timeout time.Duration
	rpcTimeoutValue := mb.getRpcTimeout()
	deadline := timelib.Now().Add(rpcTimeoutValue)
	ok := false
	if ctx != nil {
		deadline, ok = ctx.Deadline()
		if ok {
			timeout = time.Until(deadline)
		}
	}

	if timeout <= 0 {
		timeout = rpcTimeoutValue
	}

	// 处理ctx，只保留携带信息
	newCtx := xcontext.NewWithCloneCtx(ctx)

	mt, err := mb.requireRpcMonitor(newCtx)
	if err != nil {
		return 0, err
	}
	reqId := mt.GenSeq()
	var cbParams []interface{}
	if param != nil {
		cbParams = param.Params
	}
	state := monitor.NewCallState(newCtx, reqId, data.GetMethod(), timeout, mb.sender, callbacks, cbParams)

	// 创建请求
	envelope := msgenvelope.NewMsgEnvelope()
	envelope.SetData(data)
	envelope.SetPriority(priority)
	envelope.SetDispatchKey(dispatchKey)

	meta := msgenvelope.NewMeta()
	meta.SetReqId(reqId)
	meta.SetSenderPid(mb.sender.GetPid())
	meta.SetReceiverPid(mb.receiver.GetPid())
	meta.SetDispatcher(mb.sender)
	meta.SetDeadline(deadline.UnixNano())
	meta.SetIdempotencyKey(idempotencyKey)

	envelope.SetMeta(meta)

	// getLogger().WithContext(newCtx).Debugf("call envelope: %+v", envelope)

	// 加入等待队列（仅保存 CallState）
	mt.Add(state)

	// 发送消息：调用后 envelope 所有权转移，由对端 mailbox 或 sender 负责 Release
	if err := mb.receiver.DeliverRequest(newCtx, envelope); err != nil {
		_ = mt.Remove(reqId)
		state.Release()
		envelope.Release()
		mb.logErrorf(newCtx, "service[%s] send message[%s] request to client failed, error: %v", mb.sender.GetPid().GetName(), data.GetMethod(), err)
		return 0, def.ErrRPCCallFailed
	}

	return reqId, nil
}

// AsyncCall 异步调用服务
func (mb *MessageBus) AsyncCall(ctx context.Context, method string, in interface{}, param *dto.AsyncCallParams, callbacks ...dto.CompletionFunc) (dto.CancelRpc, error) {
	defer ReleaseMessageBus(mb)
	if mb.err != nil {
		return nil, mb.err
	}
	if mb.sender == nil || mb.receiver == nil {
		return nil, fmt.Errorf("sender or receiver is nil")
	}
	if len(callbacks) == 0 {
		return nil, def.ErrCallbacksIsEmpty
	}

	data := msgenvelope.NewData()
	data.SetMethod(method)
	data.SetRequest(in)
	data.SetResponse(nil)
	data.SetNeedResponse(true)

	reqId, err := mb.asyncCall(ctx, data, def.PriorityNormal, "", "", param, callbacks...)
	if err != nil {
		return dto.EmptyCancelRpc, err
	}
	mt, monitorErr := mb.requireRpcMonitor(ctx)
	if monitorErr != nil {
		return dto.EmptyCancelRpc, monitorErr
	}
	return mt.NewCancel(reqId), nil
}

func (mb *MessageBus) AsyncCallWithOpt(ctx context.Context, opts ...dto.BusOptionBuilder) (dto.CancelRpc, error) {
	option := dto.NewBusOption(opts...)
	option.Ctx = ctx
	if !option.NotRecycle {
		defer ReleaseMessageBus(mb)
	}
	if mb.err != nil {
		return nil, mb.err
	}
	if mb.sender == nil || mb.receiver == nil {
		return nil, fmt.Errorf("sender or receiver is nil")
	}
	if len(option.Callbacks) == 0 {
		return nil, def.ErrCallbacksIsEmpty
	}

	data := msgenvelope.NewData()
	data.SetMethod(option.Method)
	data.SetRequest(option.In)
	data.SetResponse(nil)
	data.SetNeedResponse(true)

	reqId, err := mb.asyncCall(option.Ctx, data, option.Priority, option.DispatchKey, option.IdempotencyKey, option.CallbackParams, option.Callbacks...)
	if err != nil {
		return dto.EmptyCancelRpc, err
	}
	mt, monitorErr := mb.requireRpcMonitor(option.Ctx)
	if monitorErr != nil {
		return dto.EmptyCancelRpc, monitorErr
	}
	return mt.NewCancel(reqId), nil
}

// asyncCallInternal 供MultiBus使用的内部方法，recycle参数控制是否释放Bus
func (mb *MessageBus) asyncCallInternal(ctx context.Context, data inf.IEnvelopeData, priority def.Priority, dispatchKey string, idempotencyKey string, recycle bool, param *dto.AsyncCallParams, callbacks ...dto.CompletionFunc) (uint64, error) {
	if recycle {
		defer ReleaseMessageBus(mb)
	}
	if mb.err != nil {
		return 0, mb.err
	}
	if mb.sender == nil || mb.receiver == nil {
		return 0, fmt.Errorf("sender or receiver is nil")
	}
	if len(callbacks) == 0 {
		return 0, def.ErrCallbacksIsEmpty
	}

	return mb.asyncCall(ctx, data, priority, dispatchKey, idempotencyKey, param, callbacks...)
}

// send 内部发送方法
func (mb *MessageBus) send(ctx context.Context, method string, priority def.Priority, dispatchKey string, idempotencyKey string, in interface{}) (sendErr error) {
	if mb.err != nil {
		return mb.err
	}
	if mb.sender == nil {
		return fmt.Errorf("sender is nil")
	}
	if mb.receiver == nil {
		return fmt.Errorf("receiver is nil")
	}

	// RPC metrics: send total & errors
	if mb.factory != nil {
		mb.factory.metrics.sendTotal.Add(1)
		defer func() {
			if sendErr != nil {
				mb.factory.metrics.sendErrors.Add(1)
			}
		}()
	}

	var deadline time.Time
	deadlineTime, ok := timelib.Now(), false
	if ctx != nil {
		deadlineTime, ok = ctx.Deadline()
	}
	if ok {
		deadline = deadlineTime
	} else {
		deadline = timelib.Now().Add(mb.getRpcTimeout())
	}

	// 创建请求
	envelope := msgenvelope.NewMsgEnvelope()

	data := msgenvelope.NewData()
	data.SetMethod(method)
	data.SetRequest(in)
	data.SetResponse(nil)
	data.SetNeedResponse(false)
	envelope.SetData(data)
	envelope.SetPriority(priority)
	envelope.SetDispatchKey(dispatchKey)

	meta := msgenvelope.NewMeta()
	meta.SetSenderPid(mb.sender.GetPid())
	meta.SetReceiverPid(mb.receiver.GetPid())
	meta.SetDispatcher(mb.sender)
	meta.SetDeadline(deadline.UnixNano())
	meta.SetIdempotencyKey(idempotencyKey)
	envelope.SetMeta(meta)

	// 调用后 envelope 所有权转移，由对端 mailbox 或 sender 负责 Release
	if err := mb.receiver.DeliverRequest(ctx, envelope); err != nil {
		envelope.Release()
		return err
	}
	return nil
}

// Send 无返回调用
func (mb *MessageBus) Send(ctx context.Context, method string, in interface{}) error {
	defer ReleaseMessageBus(mb)
	return mb.send(ctx, method, def.PriorityNormal, "", "", in)
}

func (mb *MessageBus) SendWithOpt(ctx context.Context, opts ...dto.BusOptionBuilder) error {
	option := dto.NewBusOption(opts...)
	option.Ctx = ctx
	if !option.NotRecycle {
		defer ReleaseMessageBus(mb)
	}
	return mb.send(option.Ctx, option.Method, option.Priority, option.DispatchKey, option.IdempotencyKey, option.In)
}

// sendInternal 供MultiBus使用的内部方法，recycle参数控制是否释放Bus
func (mb *MessageBus) sendInternal(ctx context.Context, data inf.IEnvelopeData, priority def.Priority, dispatchKey string, idempotencyKey string, recycle bool) error {
	if recycle {
		defer ReleaseMessageBus(mb)
	}
	if mb.err != nil {
		return mb.err
	}
	if mb.receiver == nil {
		return fmt.Errorf("receiver is nil")
	}

	return mb.send(ctx, data.GetMethod(), priority, dispatchKey, idempotencyKey, data.GetRequest())
}

func (mb *MessageBus) Release() {
	ReleaseMessageBus(mb)
}

type internalBus interface {
	inf.IBus
	callInternal(ctx context.Context, method string, in, out interface{}, priority def.Priority, dispatchKey string, idempotencyKey string, recycle bool) error
	asyncCallInternal(ctx context.Context, data inf.IEnvelopeData, priority def.Priority, dispatchKey string, idempotencyKey string, recycle bool, params *dto.AsyncCallParams, callbacks ...dto.CompletionFunc) (uint64, error)
	sendInternal(ctx context.Context, data inf.IEnvelopeData, priority def.Priority, dispatchKey string, idempotencyKey string, recycle bool) error
}

// MultiBus 多节点调用
type MultiBus []internalBus

func (m MultiBus) firstMessageBus() *MessageBus {
	if len(m) == 0 {
		return nil
	}
	if bus, ok := m[0].(*MessageBus); ok {
		return bus
	}
	return nil
}

func (m MultiBus) getLogger() log.ILoggerX {
	if mb := m.firstMessageBus(); mb != nil {
		return mb.getLogger()
	}
	return nil
}

func (m MultiBus) logWarnf(ctx context.Context, format string, args ...interface{}) {
	if l := m.getLogger(); l != nil {
		l.WithContext(ctx).Warnf(format, args...)
	}
}

func (m MultiBus) logErrorf(ctx context.Context, format string, args ...interface{}) {
	if l := m.getLogger(); l != nil {
		l.WithContext(ctx).Errorf(format, args...)
	}
}

func (m MultiBus) requireRpcMonitor(ctx context.Context) (*monitor.RpcMonitor, error) {
	if mb := m.firstMessageBus(); mb != nil {
		return mb.requireRpcMonitor(ctx)
	}
	err := fmt.Errorf("msgbus rpc monitor not initialized")
	m.logErrorf(ctx, err.Error())
	return nil, err
}

func (m MultiBus) Call(ctx context.Context, method string, in, out interface{}) error {
	if len(m) == 0 {
		m.logWarnf(ctx, "===========select empty service to call %s", method)
		return def.ErrSelectEmptyResult
	}

	// 依次尝试调用每个服务，找到第一个成功的就返回
	// 注意：call方法只在成功时才会修改out，失败时不会修改，因此这里是安全的
	var errs []error
	for _, bus := range m {
		if err := bus.callInternal(ctx, method, in, out, def.PriorityNormal, "", "", true); err != nil {
			errs = append(errs, err)
		} else {
			return nil // 找到一个成功的就返回
		}
	}
	return errorlib.CombineErr(errs...)
}

func (m MultiBus) CallWithOpt(ctx context.Context, opts ...dto.BusOptionBuilder) error {
	option := dto.NewBusOption(opts...)
	option.Ctx = ctx
	if len(m) == 0 {
		m.logWarnf(ctx, "===========select empty service to call %s", option.Method)
		return def.ErrSelectEmptyResult
	}

	// 根据 CallMode 决定调用策略
	switch option.CallMode {
	case dto.CallModeAll:
		// 模式1: 所有节点都调用，收集所有结果
		var errs []error
		successCount := 0
		for _, bus := range m {
			if err := bus.callInternal(option.Ctx, option.Method, option.In, option.Out, option.Priority, option.DispatchKey, option.IdempotencyKey, !option.NotRecycle); err != nil {
				errs = append(errs, err)
			} else {
				successCount++
			}
		}
		// 返回组合错误，即使所有调用都成功，也让调用者知道有多少个成功
		if len(errs) > 0 {
			m.logWarnf(ctx, "call %s with CallModeAll: %d/%d succeeded", option.Method, successCount, len(m))
		}
		return errorlib.CombineErr(errs...)

	default: // dto.CallModeAny 或未设置
		// 模式0: 依次尝试调用每个服务，找到第一个成功的就返回
		// 注意：call方法只在成功时才会修改out，失败时不会修改，因此这里是安全的
		var errs []error
		for _, bus := range m {
			if err := bus.callInternal(option.Ctx, option.Method, option.In, option.Out, option.Priority, option.DispatchKey, option.IdempotencyKey, !option.NotRecycle); err != nil {
				errs = append(errs, err)
			} else {
				return nil // 找到一个成功的就返回
			}
		}
		return errorlib.CombineErr(errs...)
	}
}

func (m MultiBus) AsyncCall(ctx context.Context, method string, in interface{}, param *dto.AsyncCallParams, callbacks ...dto.CompletionFunc) (dto.CancelRpc, error) {
	if len(m) == 0 {
		m.logWarnf(ctx, "===========select empty service to async call %s", method)
		return nil, def.ErrSelectEmptyResult
	}
	data := msgenvelope.NewData()
	data.SetMethod(method)
	data.SetRequest(in)
	data.SetResponse(nil) // 容错
	data.SetNeedResponse(true)

	var errs []error
	var reqIds []uint64
	for _, bus := range m {
		if reqId, err := bus.asyncCallInternal(ctx, data, def.PriorityNormal, "", "", true, param, callbacks...); err != nil {
			errs = append(errs, err)
		} else {
			reqIds = append(reqIds, reqId)
		}
	}
	mt, monitorErr := m.requireRpcMonitor(ctx)
	if monitorErr != nil {
		return dto.EmptyCancelRpc, errorlib.CombineErr(append(errs, monitorErr)...)
	}
	return mt.NewMultiCancel(reqIds...), errorlib.CombineErr(errs...)
}

func (m MultiBus) AsyncCallWithOpt(ctx context.Context, opts ...dto.BusOptionBuilder) (dto.CancelRpc, error) {
	option := dto.NewBusOption(opts...)
	option.Ctx = ctx
	if len(m) == 0 {
		m.logWarnf(ctx, "===========select empty service to async call %s", option.Method)
		return dto.EmptyCancelRpc, def.ErrSelectEmptyResult
	}
	data := msgenvelope.NewData()
	data.SetMethod(option.Method)
	data.SetRequest(option.In)
	data.SetResponse(nil) // 容错
	data.SetNeedResponse(true)

	// 异步调用的语义:
	// - err 只表示消息是否成功发送,不代表执行结果
	// - 执行结果通过 callback 返回
	// - CallModeAny: 发送给所有节点,任意一个回调执行即可
	// - CallModeAll: 发送给所有节点,所有回调都会执行
	// 因此两种模式的发送逻辑是一样的,都是发送给所有节点

	var errs []error
	var reqIds []uint64
	for _, bus := range m {
		if reqId, err := bus.asyncCallInternal(option.Ctx, data, option.Priority, option.DispatchKey, option.IdempotencyKey, !option.NotRecycle, option.CallbackParams, option.Callbacks...); err != nil {
			errs = append(errs, err)
		} else {
			reqIds = append(reqIds, reqId)
		}
	}

	if len(errs) > 0 {
		m.logWarnf(ctx, "async call %s: %d/%d nodes sent successfully", option.Method, len(reqIds), len(m))
	}

	// 返回 MultiCancel,可以取消所有节点的回调
	mt, monitorErr := m.requireRpcMonitor(option.Ctx)
	if monitorErr != nil {
		return dto.EmptyCancelRpc, errorlib.CombineErr(append(errs, monitorErr)...)
	}
	return mt.NewMultiCancel(reqIds...), errorlib.CombineErr(errs...)
}

// TODO send这里需要考虑一下所有的都公用一个ctx会不会有什么问题

func (m MultiBus) Send(ctx context.Context, method string, in interface{}) error {
	if len(m) == 0 {
		m.logWarnf(ctx, "===========select empty service to send %s", method)
		return nil
		//return def.ErrSelectEmptyResult
	}
	var errs []error
	envelopeData := msgenvelope.NewData()
	envelopeData.SetMethod(method)
	envelopeData.SetRequest(in)
	envelopeData.SetResponse(nil)
	envelopeData.SetNeedResponse(false)
	for _, bus := range m {
		if err := bus.sendInternal(ctx, envelopeData, def.PriorityNormal, "", "", true); err != nil {
			errs = append(errs, err)
		}
	}

	return errorlib.CombineErr(errs...)
}

func (m MultiBus) SendWithOpt(ctx context.Context, opts ...dto.BusOptionBuilder) error {
	option := dto.NewBusOption(opts...)
	option.Ctx = ctx
	if len(m) == 0 {
		m.logWarnf(ctx, "===========select empty service to send %s", option.Method)
		return nil
		//return def.ErrSelectEmptyResult
	}
	var errs []error
	envelopeData := msgenvelope.NewData()
	envelopeData.SetMethod(option.Method)
	envelopeData.SetRequest(option.In)
	envelopeData.SetResponse(nil)
	envelopeData.SetNeedResponse(false)
	for _, bus := range m {
		if err := bus.sendInternal(option.Ctx, envelopeData, option.Priority, option.DispatchKey, option.IdempotencyKey, !option.NotRecycle); err != nil {
			errs = append(errs, err)
		}
	}
	return errorlib.CombineErr(errs...)
}

func (m MultiBus) Release() {
	for _, bus := range m {
		ReleaseMessageBus(bus.(*MessageBus))
	}
}
