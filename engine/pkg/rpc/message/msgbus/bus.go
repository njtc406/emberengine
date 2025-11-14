// Package msgbus
// @Title  消息总线
// @Description  所有的消息都通过该模块进行发送
// @Author  yr  2024/11/12
// @Update  yr  2024/11/12
package msgbus

import (
	"context"
	"fmt"
	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/monitor"
	"github.com/njtc406/emberengine/engine/pkg/rpc/message/msgenvelope"
	"github.com/njtc406/emberengine/engine/pkg/utils/timelib"
	"reflect"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/errorlib"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
)

type MessageBus struct {
	dto.DataRef
	sender   inf.IRpcDispatcher
	receiver inf.IRpcDispatcher
	err      error
}

func (mb *MessageBus) Reset() {
	mb.sender = nil
	mb.receiver = nil
}

var busPool = pool.NewSyncPoolWrapper(
	func() *MessageBus {
		return &MessageBus{}
	},
	pool.NewStatsRecorder("busPool"),
	pool.WithRef(func(t *MessageBus) {
		t.Ref()
	}),
	pool.WithUnRef(func(t *MessageBus) {
		t.UnRef()
	}),
	pool.WithReset(func(mb *MessageBus) {
		mb.Reset()
	}),
)

func NewMessageBus(sender inf.IRpcDispatcher, receiver inf.IRpcDispatcher, err error) *MessageBus {
	mb := busPool.Get()
	mb.sender = sender
	mb.receiver = receiver
	mb.err = err
	return mb
}

func ReleaseMessageBus(mb *MessageBus) {
	busPool.Put(mb)
}

func GetMessageBusPoolStats() *pool.Stats {
	return busPool.Stats()
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
			timeout = deadline.Sub(timelib.Now())
		}
	}

	if timeout <= 0 {
		timeout = def.DefaultRpcTimeout
	}

	mt := monitor.GetRpcMonitor()

	// 创建请求
	envelope := msgenvelope.NewMsgEnvelope(ctx)
	envelope.SetData(data)

	meta := msgenvelope.NewMeta()
	meta.SetReqId(mt.GenSeq())
	meta.SetSenderPid(mb.sender.GetPid())
	meta.SetReceiverPid(mb.receiver.GetPid())
	meta.SetDispatcher(mb.sender)
	meta.SetTimeout(timeout)
	envelope.SetMeta(meta)

	//log.SysLogger.Debugf("call envelope: %+v", envelope)

	// 加入等待队列
	mt.Add(envelope)

	// 发送消息
	if err := mb.receiver.SendRequest(envelope); err != nil {
		// 发送失败,释放资源
		mt.Remove(meta.GetReqId())

		log.SysLogger.WithContext(envelope.GetContext()).Errorf("service[%s] send message[%s] request to client failed, error: %v", envelope.GetMeta().GetSenderPid().GetName(), data.GetMethod(), err)
		envelope.Release()
		return def.ErrRPCCallFailed
	}

	// 等待回复
	envelope.Wait()

	mt.Remove(meta.GetReqId()) // 容错,不管有没有释放,都释放一次(实际上在所有设置done之前都会释放)

	if err := data.GetError(); err != nil {
		envelope.Release()
		return err
	}

	resp := data.GetResponse()

	// 获取到返回后直接释放
	envelope.Release()

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

func (mb *MessageBus) callWithCtl(ctx context.Context, data inf.IEnvelopeData, out interface{}, recycle bool) error {
	if recycle {
		defer ReleaseMessageBus(mb)
	}

	if mb.err != nil {
		return mb.err
	}

	return mb.call(ctx, data, out)
}

// Call 同步调用服务
func (mb *MessageBus) Call(ctx context.Context, method string, in, out interface{}) error {
	if mb.err != nil {
		// 这里可能是从MultiBus中产生的
		return mb.err
	}
	data := msgenvelope.NewData()
	data.SetMethod(method)
	data.SetRequest(in)
	data.SetResponse(nil) // 容错
	data.SetNeedResponse(true)
	return mb.callWithCtl(ctx, data, out, true) // 默认需要回收
}

func (mb *MessageBus) CallWithOpt(opts ...dto.BusOptionBuilder) error {
	option := dto.NewBusOption(opts...)
	data := msgenvelope.NewData()
	data.SetMethod(option.Method)
	data.SetRequest(option.In)
	data.SetResponse(nil) // 容错
	data.SetNeedResponse(true)
	return mb.callWithCtl(option.Ctx, data, option.Out, !option.NotRecycle)
}

func (mb *MessageBus) callDirect(ctx context.Context, data inf.IEnvelopeData, out interface{}) error {
	defer ReleaseMessageBus(mb)
	return mb.call(ctx, data, out)
}

func (mb *MessageBus) asyncCallWithCancel(ctx context.Context, data inf.IEnvelopeData, param *dto.AsyncCallParams, callbacks ...dto.CompletionFunc) (dto.CancelRpc, error) {
	reqId, err := mb.asyncCall(ctx, data, param, callbacks...)
	if err != nil {
		return dto.EmptyCancelRpc, err
	}
	return monitor.GetRpcMonitor().NewCancel(reqId), nil
}

func (mb *MessageBus) asyncCall(ctx context.Context, data inf.IEnvelopeData, param *dto.AsyncCallParams, callbacks ...dto.CompletionFunc) (uint64, error) {
	var timeout time.Duration
	if ctx != nil {
		deadline, ok := ctx.Deadline()
		if ok {
			timeout = deadline.Sub(timelib.Now())
		}
	}

	if timeout <= 0 {
		timeout = def.DefaultRpcTimeout
	}

	mt := monitor.GetRpcMonitor()

	// 创建请求
	envelope := msgenvelope.NewMsgEnvelope(ctx)
	envelope.SetData(data)

	meta := msgenvelope.NewMeta()
	meta.SetReqId(mt.GenSeq())
	meta.SetSenderPid(mb.sender.GetPid())
	meta.SetReceiverPid(mb.receiver.GetPid())
	meta.SetDispatcher(mb.sender)
	meta.SetTimeout(timeout)
	meta.SetCallbackParams(param.Params)
	meta.SetCallback(callbacks)
	envelope.SetMeta(meta)

	//log.SysLogger.Debugf("call envelope: %+v", envelope)

	// 加入等待队列
	mt.Add(envelope)

	// 发送消息,最终callback调用将在response中被执行,所以envelope会在callback执行完后自动回收
	if err := mb.receiver.SendRequest(envelope); err != nil {
		// 发送失败,释放资源
		mt.Remove(meta.GetReqId())
		envelope.Release()
		log.SysLogger.WithContext(envelope.GetContext()).Errorf("service[%s] send message[%s] request to client failed, error: %v", mb.sender.GetPid().GetName(), data.GetMethod(), err)
		return 0, def.ErrRPCCallFailed
	}

	return meta.GetReqId(), nil
}

func (mb *MessageBus) asyncCallWithCtl(ctx context.Context, data inf.IEnvelopeData, recycle bool, param *dto.AsyncCallParams, callbacks ...dto.CompletionFunc) (dto.CancelRpc, error) {
	if recycle {
		defer ReleaseMessageBus(mb)
	}

	if mb.err != nil {
		// 这里可能是从MultiBus中产生的
		return nil, mb.err
	}
	if mb.sender == nil || mb.receiver == nil {
		return nil, fmt.Errorf("sender or receiver is nil")
	}
	if len(callbacks) == 0 {
		return nil, def.ErrCallbacksIsEmpty
	}

	return mb.asyncCallWithCancel(ctx, data, param, callbacks...)
}

// AsyncCall 异步调用服务
func (mb *MessageBus) AsyncCall(ctx context.Context, method string, in interface{}, param *dto.AsyncCallParams, callbacks ...dto.CompletionFunc) (dto.CancelRpc, error) {
	data := msgenvelope.NewData()
	data.SetMethod(method)
	data.SetRequest(in)
	data.SetResponse(nil) // 容错
	data.SetNeedResponse(true)
	return mb.asyncCallWithCtl(ctx, data, true, param, callbacks...)
}

func (mb *MessageBus) AsyncCallWithOpt(opts ...dto.BusOptionBuilder) (dto.CancelRpc, error) {
	if mb.err != nil {
		// 这里可能是从MultiBus中产生的
		return nil, mb.err
	}
	option := dto.NewBusOption(opts...)
	data := msgenvelope.NewData()
	data.SetMethod(option.Method)
	data.SetRequest(option.In)
	data.SetResponse(nil) // 容错
	data.SetNeedResponse(true)
	return mb.asyncCallWithCtl(option.Ctx, data, !option.NotRecycle, option.CallbackParams, option.Callbacks...)
}

func (mb *MessageBus) asyncCallDirect(ctx context.Context, data inf.IEnvelopeData, recycle bool, param *dto.AsyncCallParams, callbacks ...dto.CompletionFunc) (uint64, error) {
	defer ReleaseMessageBus(mb)
	if mb.err != nil {
		// 这里可能是从MultiBus中产生的
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

// Send 无返回调用
func (mb *MessageBus) Send(ctx context.Context, method string, in interface{}) error {
	defer ReleaseMessageBus(mb)
	if mb.err != nil {
		// 这里可能是从MultiBus中产生的
		return mb.err
	}
	if mb.receiver == nil {
		return fmt.Errorf("receiver is nil")
	}

	// 创建请求
	envelope := msgenvelope.NewMsgEnvelope(ctx)

	data := msgenvelope.NewData()
	data.SetMethod(method)
	data.SetRequest(in)
	data.SetResponse(nil)
	data.SetNeedResponse(false)
	envelope.SetData(data)

	meta := msgenvelope.NewMeta()
	meta.SetReqId(monitor.GetRpcMonitor().GenSeq())
	meta.SetReceiverPid(mb.receiver.GetPid())
	meta.SetDispatcher(mb.sender)
	envelope.SetMeta(meta)

	// 如果是远程调用, 则由远程调用释放资源,如果是本地调用,则由接收者自行回收
	return mb.receiver.SendRequestAndRelease(envelope)
}

func (mb *MessageBus) SendWithOpt(opts ...dto.BusOptionBuilder) error {
	defer ReleaseMessageBus(mb)
	if mb.err != nil {
		// 这里可能是从MultiBus中产生的
		return mb.err
	}
	if mb.receiver == nil {
		return fmt.Errorf("receiver is nil")
	}

	option := dto.NewBusOption(opts...)

	// 创建请求
	envelope := msgenvelope.NewMsgEnvelope(option.Ctx)

	data := msgenvelope.NewData()
	data.SetMethod(option.Method)
	data.SetRequest(option.In)
	data.SetResponse(nil)
	data.SetNeedResponse(false)
	envelope.SetData(data)

	meta := msgenvelope.NewMeta()
	meta.SetReqId(monitor.GetRpcMonitor().GenSeq())
	meta.SetReceiverPid(mb.receiver.GetPid())
	meta.SetDispatcher(mb.sender)
	envelope.SetMeta(meta)

	// 如果是远程调用, 则由远程调用释放资源,如果是本地调用,则由接收者自行回收
	return mb.receiver.SendRequestAndRelease(envelope)
}

func (mb *MessageBus) sendDirect(ctx context.Context, data inf.IEnvelopeData) error {
	defer ReleaseMessageBus(mb)
	if mb.err != nil {
		// 这里可能是从MultiBus中产生的
		return mb.err
	}
	if mb.receiver == nil {
		return fmt.Errorf("receiver is nil")
	}

	// 创建请求
	envelope := msgenvelope.NewMsgEnvelope(ctx)
	envelope.SetData(data)

	meta := msgenvelope.NewMeta()
	meta.SetReqId(monitor.GetRpcMonitor().GenSeq())
	meta.SetReceiverPid(mb.receiver.GetPid())
	meta.SetDispatcher(mb.sender)
	envelope.SetMeta(meta)

	// 如果是远程调用, 则由远程调用释放资源,如果是本地调用,则由接收者自行回收
	return mb.receiver.SendRequestAndRelease(envelope)
}

func (mb *MessageBus) Release() {
	ReleaseMessageBus(mb)
}

type internalBus interface {
	inf.IBus
	callDirect(ctx context.Context, data inf.IEnvelopeData, out interface{}) error
	callWithCtl(ctx context.Context, data inf.IEnvelopeData, out interface{}, recycle bool) error
	asyncCallDirect(ctx context.Context, data inf.IEnvelopeData, recycle bool, params *dto.AsyncCallParams, callbacks ...dto.CompletionFunc) (uint64, error)
	sendDirect(ctx context.Context, data inf.IEnvelopeData) error
}

// MultiBus 多节点调用
type MultiBus []internalBus

func (m MultiBus) Call(ctx context.Context, method string, in, out interface{}) error {
	if len(m) == 0 {
		log.SysLogger.WithContext(ctx).Warnf("===========select empty service to call %s", method)
		return def.ErrSelectEmptyResult
	}

	data := msgenvelope.NewData()
	data.SetMethod(method)
	data.SetRequest(in)
	data.SetResponse(nil) // 容错
	data.SetNeedResponse(true)

	var errs []error
	for _, bus := range m {
		if err := bus.callWithCtl(ctx, data, out, true); err != nil { // TODO 这里会有问题，如果使用同一个out接收返回值,可能后面的会覆盖前面的数据
			errs = append(errs, err) // TODO 这里后续再看要不要break,暂时先收集所有错误,可能需要做成参数
		} else {
			return nil // 找到一个就返回
		}
	}
	return errorlib.CombineErr(errs...)
}

func (m MultiBus) CallWithOpt(opts ...dto.BusOptionBuilder) error {
	option := dto.NewBusOption(opts...)
	if len(m) == 0 {
		log.SysLogger.Warnf("===========select empty service to call %s", option.Method)
		return def.ErrSelectEmptyResult
	}

	data := msgenvelope.NewData()
	data.SetMethod(option.Method)
	data.SetRequest(option.In)
	data.SetResponse(nil) // 容错
	data.SetNeedResponse(true)

	var errs []error
	for _, bus := range m {
		if err := bus.callWithCtl(option.Ctx, data, option.Out, true); err != nil { // TODO 这里会有问题，如果使用同一个out接收返回值,可能后面的会覆盖前面的数据
			errs = append(errs, err) // TODO 这里后续再看要不要break,暂时先收集所有错误,可能需要做成参数
		}

	}
	return errorlib.CombineErr(errs...)
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
		if reqId, err := bus.asyncCallDirect(ctx, data, true, param, callbacks...); err != nil {
			errs = append(errs, err)
		} else {
			reqIds = append(reqIds, reqId)
		}
	}
	return monitor.GetRpcMonitor().NewMultiCancel(reqIds...), errorlib.CombineErr(errs...)
}

func (m MultiBus) AsyncCallWithOpt(opts ...dto.BusOptionBuilder) (dto.CancelRpc, error) {
	option := dto.NewBusOption(opts...)
	if len(m) == 0 {
		log.SysLogger.Warnf("===========select empty service to async call %s", option.Method)
		return dto.EmptyCancelRpc, def.ErrSelectEmptyResult
	}
	data := msgenvelope.NewData()
	data.SetMethod(option.Method)
	data.SetRequest(option.In)
	data.SetResponse(nil) // 容错
	data.SetNeedResponse(true)

	var errs []error
	var reqIds []uint64
	for _, bus := range m {
		if reqId, err := bus.asyncCallDirect(option.Ctx, data, !option.NotRecycle, option.CallbackParams, option.Callbacks...); err != nil {
			errs = append(errs, err)
		} else {
			reqIds = append(reqIds, reqId)
		}
	}
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
		if err := bus.sendDirect(ctx, envelopeData); err != nil {
			errs = append(errs, err)
		}
	}

	return errorlib.CombineErr(errs...)
}

func (m MultiBus) SendWithOpt(opts ...dto.BusOptionBuilder) error {
	option := dto.NewBusOption(opts...)
	var errs []error
	envelopeData := msgenvelope.NewData()
	envelopeData.SetMethod(option.Method)
	envelopeData.SetRequest(option.In)
	envelopeData.SetResponse(nil)
	envelopeData.SetNeedResponse(false)
	for _, bus := range m {
		if err := bus.sendDirect(option.Ctx, envelopeData); err != nil {
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
