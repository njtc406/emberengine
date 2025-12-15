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
	"sync"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/monitor"
	"github.com/njtc406/emberengine/engine/pkg/rpc/message/msgenvelope"
	"github.com/njtc406/emberengine/engine/pkg/utils/errorlib"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
)

var busPool pool.IPool[*MessageBus]
var busPoolOnce sync.Once

func getBusPool() pool.IPool[*MessageBus] {
	busPoolOnce.Do(func() {
		busPool = pool.NewPerPPoolWrapper(
			config.Conf.NodeConf.BusPoolSize,
			func() *MessageBus {
				return &MessageBus{}
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
	})
	return busPool
}

type MessageBus struct {
	dto.DataRef
	sender   inf.IRpcDispatcher
	receiver inf.IRpcDispatcher
	err      error
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
	mb := getBusPool().Get()
	mb.sender = sender
	mb.receiver = receiver
	mb.err = err
	return mb
}

func ReleaseMessageBus(mb *MessageBus) {
	getBusPool().Put(mb)
}

func (mb *MessageBus) GetReceiverPid() *actor.PID {
	return mb.receiver.GetPid()
}

func (mb *MessageBus) call(ctx context.Context, data inf.IEnvelopeData, out interface{}) error {
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

	if out != nil {
		switch out.(type) {
		case []interface{}:
			// 远程调用都是固定的proto消息,不会出现这个类型的参数
			// 本地调用,接收多参数返回值,那么所有的接收参数都必须是指针或者引用类型
			for i, v := range out.([]interface{}) {
				kd := reflect.TypeOf(v).Kind()
				if kd != reflect.Ptr && kd != reflect.Interface &&
					kd != reflect.Func && kd != reflect.Map &&
					kd != reflect.Slice && kd != reflect.Chan {
					return fmt.Errorf("multi out call: all out params must be pointer, but the %v one got %v", i, kd)
				}
			}
		default:
			kd := reflect.TypeOf(out).Kind()
			if kd != reflect.Ptr && kd != reflect.Interface &&
				kd != reflect.Func && kd != reflect.Map &&
				kd != reflect.Slice && kd != reflect.Chan {
				return fmt.Errorf("single out call: out param must be pointer, but got:%v", kd)
			}
		}
	}

	var timeout time.Duration
	if ctx != nil {
		deadline, ok := ctx.Deadline()
		if ok {
			timeout = time.Until(deadline)
		}
	}

	if timeout <= 0 {
		timeout = def.DefaultRpcTimeout
	}

	mt := monitor.GetRpcMonitor()
	reqId := mt.GenSeq()
	state := monitor.NewCallState(ctx, reqId, data.GetMethod(), timeout, mb.sender, nil, nil)

	// 创建请求
	envelope := msgenvelope.NewMsgEnvelope()
	envelope.SetData(data)

	meta := msgenvelope.NewMeta()
	meta.SetReqId(reqId)
	meta.SetSenderPid(mb.sender.GetPid())
	meta.SetReceiverPid(mb.receiver.GetPid())
	meta.SetDispatcher(mb.sender)
	envelope.SetMeta(meta)

	//log.SysLogger.Debugf("call envelope: %+v", envelope)

	// 加入等待队列（仅保存 CallState，不再跨 goroutine 传递可释放 envelope）
	mt.Add(state)

	// 发送消息：调用后 envelope 所有权转移，由对端 mailbox 或 sender 负责 Release
	if err := mb.receiver.Deliver(ctx, envelope); err != nil {
		_ = mt.Remove(reqId)
		state.Release()
		log.SysLogger.WithContext(ctx).Errorf("service[%s] send message[%s] request to client failed, error: %v", mb.sender.GetPid().GetName(),
			data.GetMethod(), err)
		return def.ErrRPCCallFailed
	}

	// 等待回复（由 CallState 唤醒）
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

	// 有返回值
	// 先判断是否时多返回值
	switch resp.(type) {
	case []interface{}:
		respList := resp.([]interface{})
		// 多返回值,那么接收者也必须时多返回值
		if outs, ok := out.([]interface{}); !ok {
			return fmt.Errorf("call: type not match, expected %v but got %v", reflect.TypeOf(resp), reflect.TypeOf(out))
		} else {
			for idx, v := range outs {
				respType := reflect.TypeOf(respList[idx])
				respKd := respType.Kind()
				if respKd == reflect.Ptr {
					respType = respType.Elem()
				}
				outType := reflect.TypeOf(v)
				outKd := outType.Kind()
				if outKd == reflect.Ptr {
					outType = outType.Elem()
				}
				if outType != respType {
					return fmt.Errorf("call: type not match2, expected %v but got %v", respType, outType)
				}
				respVal := reflect.ValueOf(respList[idx])
				if respVal.Kind() == reflect.Ptr {
					respVal = respVal.Elem()
				}

				reflect.ValueOf(v).Elem().Set(respVal)
			}
		}
	default:
		// 单返回值,那么接收者也必须是单返回值
		respType := reflect.TypeOf(resp)
		respKd := respType.Kind()
		if respKd == reflect.Ptr {
			respType = respType.Elem()
		}
		outType := reflect.TypeOf(out)
		outKd := outType.Kind()
		if outKd == reflect.Ptr {
			outType = outType.Elem()
		}
		if outType != respType {
			return fmt.Errorf("call: type not match3, expected %v but got %v", respType, outType)
		}
		respVal := reflect.ValueOf(resp)
		if respVal.Kind() == reflect.Ptr {
			respVal = respVal.Elem()
		}

		reflect.ValueOf(out).Elem().Set(respVal)
	}

	return nil
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
	return mb.call(ctx, data, out)
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
	return mb.call(option.Ctx, data, option.Out)
}

// callInternal 供MultiBus使用的内部方法（会自动释放）
func (mb *MessageBus) callInternal(ctx context.Context, method string, in, out interface{}, recycle bool) error {
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
	return mb.call(ctx, data, out)
}

func (mb *MessageBus) asyncCall(ctx context.Context, data inf.IEnvelopeData, param *dto.AsyncCallParams, callbacks ...dto.CompletionFunc) (uint64, error) {
	var timeout time.Duration
	if ctx != nil {
		deadline, ok := ctx.Deadline()
		if ok {
			timeout = time.Until(deadline)
		}
	}

	if timeout <= 0 {
		timeout = config.GetDefaultRpcTimeout()
	}

	mt := monitor.GetRpcMonitor()
	reqId := mt.GenSeq()
	var cbParams []interface{}
	if param != nil {
		cbParams = param.Params
	}
	state := monitor.NewCallState(ctx, reqId, data.GetMethod(), timeout, mb.sender, callbacks, cbParams)

	// 创建请求
	envelope := msgenvelope.NewMsgEnvelope()
	envelope.SetData(data)

	meta := msgenvelope.NewMeta()
	meta.SetReqId(reqId)
	meta.SetSenderPid(mb.sender.GetPid())
	meta.SetReceiverPid(mb.receiver.GetPid())
	meta.SetDispatcher(mb.sender)

	envelope.SetMeta(meta)

	//log.SysLogger.Debugf("call envelope: %+v", envelope)

	// 加入等待队列（仅保存 CallState）
	mt.Add(state)

	// 发送消息：调用后 envelope 所有权转移，由对端 mailbox 或 sender 负责 Release
	if err := mb.receiver.Deliver(ctx, envelope); err != nil {
		_ = mt.Remove(reqId)
		state.Release()
		log.SysLogger.WithContext(ctx).Errorf("service[%s] send message[%s] request to client failed, error: %v", mb.sender.GetPid().GetName(), data.GetMethod(), err)
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

	reqId, err := mb.asyncCall(ctx, data, param, callbacks...)
	if err != nil {
		return dto.EmptyCancelRpc, err
	}
	return monitor.GetRpcMonitor().NewCancel(reqId), nil
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

	reqId, err := mb.asyncCall(option.Ctx, data, option.CallbackParams, option.Callbacks...)
	if err != nil {
		return dto.EmptyCancelRpc, err
	}
	return monitor.GetRpcMonitor().NewCancel(reqId), nil
}

// asyncCallInternal 供MultiBus使用的内部方法，recycle参数控制是否释放Bus
func (mb *MessageBus) asyncCallInternal(ctx context.Context, data inf.IEnvelopeData, recycle bool, param *dto.AsyncCallParams, callbacks ...dto.CompletionFunc) (uint64, error) {
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

	return mb.asyncCall(ctx, data, param, callbacks...)
}

// send 内部发送方法
func (mb *MessageBus) send(ctx context.Context, method string, in interface{}) error {
	if mb.err != nil {
		return mb.err
	}
	if mb.receiver == nil {
		return fmt.Errorf("receiver is nil")
	}

	// 创建请求
	envelope := msgenvelope.NewMsgEnvelope()

	data := msgenvelope.NewData()
	data.SetMethod(method)
	data.SetRequest(in)
	data.SetResponse(nil)
	data.SetNeedResponse(false)
	envelope.SetData(data)

	meta := msgenvelope.NewMeta()
	meta.SetReqId(monitor.GetRpcMonitor().GenSeq()) // 必须创建reqId，否则会导致重复调用
	meta.SetReceiverPid(mb.receiver.GetPid())
	meta.SetDispatcher(mb.sender)
	envelope.SetMeta(meta)

	// 调用后 envelope 所有权转移，由对端 mailbox 或 sender 负责 Release
	return mb.receiver.Deliver(ctx, envelope)
}

// Send 无返回调用
func (mb *MessageBus) Send(ctx context.Context, method string, in interface{}) error {
	defer ReleaseMessageBus(mb)
	return mb.send(ctx, method, in)
}

func (mb *MessageBus) SendWithOpt(ctx context.Context, opts ...dto.BusOptionBuilder) error {
	option := dto.NewBusOption(opts...)
	option.Ctx = ctx
	if !option.NotRecycle {
		defer ReleaseMessageBus(mb)
	}
	return mb.send(option.Ctx, option.Method, option.In)
}

// sendInternal 供MultiBus使用的内部方法，recycle参数控制是否释放Bus
func (mb *MessageBus) sendInternal(ctx context.Context, data inf.IEnvelopeData, recycle bool) error {
	if recycle {
		defer ReleaseMessageBus(mb)
	}
	if mb.err != nil {
		return mb.err
	}
	if mb.receiver == nil {
		return fmt.Errorf("receiver is nil")
	}

	// 创建请求
	envelope := msgenvelope.NewMsgEnvelope()
	envelope.SetData(data)

	meta := msgenvelope.NewMeta()
	// Send() internal fire-and-forget：不需要 ReqId
	meta.SetReqId(0)
	meta.SetReceiverPid(mb.receiver.GetPid())
	meta.SetDispatcher(mb.sender)
	envelope.SetMeta(meta)

	// 调用后 envelope 所有权转移，由对端 mailbox 或 sender 负责 Release
	return mb.receiver.Deliver(ctx, envelope)
}

func (mb *MessageBus) Release() {
	ReleaseMessageBus(mb)
}

type internalBus interface {
	inf.IBus
	callInternal(ctx context.Context, method string, in, out interface{}, recycle bool) error
	asyncCallInternal(ctx context.Context, data inf.IEnvelopeData, recycle bool, params *dto.AsyncCallParams, callbacks ...dto.CompletionFunc) (uint64, error)
	sendInternal(ctx context.Context, data inf.IEnvelopeData, recycle bool) error
}

// MultiBus 多节点调用
type MultiBus []internalBus

func (m MultiBus) Call(ctx context.Context, method string, in, out interface{}) error {
	if len(m) == 0 {
		log.SysLogger.WithContext(ctx).Warnf("===========select empty service to call %s", method)
		return def.ErrSelectEmptyResult
	}

	// 依次尝试调用每个服务，找到第一个成功的就返回
	// 注意：call方法只在成功时才会修改out，失败时不会修改，因此这里是安全的
	var errs []error
	for _, bus := range m {
		if err := bus.callInternal(ctx, method, in, out, true); err != nil {
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
		log.SysLogger.WithContext(ctx).Warnf("===========select empty service to call %s", option.Method)
		return def.ErrSelectEmptyResult
	}

	// 根据 CallMode 决定调用策略
	switch option.CallMode {
	case dto.CallModeAll:
		// 模式1: 所有节点都调用，收集所有结果
		var errs []error
		successCount := 0
		for _, bus := range m {
			if err := bus.callInternal(option.Ctx, option.Method, option.In, option.Out, !option.NotRecycle); err != nil {
				errs = append(errs, err)
			} else {
				successCount++
			}
		}
		// 返回组合错误，即使所有调用都成功，也让调用者知道有多少个成功
		if len(errs) > 0 {
			log.SysLogger.WithContext(ctx).Warnf("call %s with CallModeAll: %d/%d succeeded", option.Method, successCount, len(m))
		}
		return errorlib.CombineErr(errs...)

	default: // dto.CallModeAny 或未设置
		// 模式0: 依次尝试调用每个服务，找到第一个成功的就返回
		// 注意：call方法只在成功时才会修改out，失败时不会修改，因此这里是安全的
		var errs []error
		for _, bus := range m {
			if err := bus.callInternal(option.Ctx, option.Method, option.In, option.Out, !option.NotRecycle); err != nil {
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
		log.SysLogger.WithContext(ctx).Warnf("===========select empty service to async call %s", method)
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
		if reqId, err := bus.asyncCallInternal(ctx, data, true, param, callbacks...); err != nil {
			errs = append(errs, err)
		} else {
			reqIds = append(reqIds, reqId)
		}
	}
	return monitor.GetRpcMonitor().NewMultiCancel(reqIds...), errorlib.CombineErr(errs...)
}

func (m MultiBus) AsyncCallWithOpt(ctx context.Context, opts ...dto.BusOptionBuilder) (dto.CancelRpc, error) {
	option := dto.NewBusOption(opts...)
	option.Ctx = ctx
	if len(m) == 0 {
		log.SysLogger.WithContext(ctx).Warnf("===========select empty service to async call %s", option.Method)
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
		if reqId, err := bus.asyncCallInternal(option.Ctx, data, !option.NotRecycle, option.CallbackParams, option.Callbacks...); err != nil {
			errs = append(errs, err)
		} else {
			reqIds = append(reqIds, reqId)
		}
	}

	if len(errs) > 0 {
		log.SysLogger.WithContext(ctx).Warnf("async call %s: %d/%d nodes sent successfully", option.Method, len(reqIds), len(m))
	}

	// 返回 MultiCancel,可以取消所有节点的回调
	return monitor.GetRpcMonitor().NewMultiCancel(reqIds...), errorlib.CombineErr(errs...)
}

// TODO send这里需要考虑一下所有的都公用一个ctx会不会有什么问题

func (m MultiBus) Send(ctx context.Context, method string, in interface{}) error {
	if len(m) == 0 {
		log.SysLogger.WithContext(ctx).Warnf("===========select empty service to send %s", method)
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
		if err := bus.sendInternal(ctx, envelopeData, true); err != nil {
			errs = append(errs, err)
		}
	}

	return errorlib.CombineErr(errs...)
}

func (m MultiBus) SendWithOpt(ctx context.Context, opts ...dto.BusOptionBuilder) error {
	option := dto.NewBusOption(opts...)
	option.Ctx = ctx
	if len(m) == 0 {
		log.SysLogger.WithContext(ctx).Warnf("===========select empty service to send %s", option.Method)
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
		if err := bus.sendInternal(option.Ctx, envelopeData, !option.NotRecycle); err != nil {
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
