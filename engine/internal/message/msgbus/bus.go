// Package msgbus
// @Title  消息总线
// @Description  所有的消息都通过该模块进行发送
// @Author  yr  2024/11/12
// @Update  yr  2024/11/12
package msgbus

import (
	"context"
	"fmt"
	"github.com/njtc406/emberengine/engine/pkg/utils/timelib"
	"reflect"
	"time"

	"github.com/njtc406/emberengine/engine/internal/message/msgenvelope"
	"github.com/njtc406/emberengine/engine/internal/monitor"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/asynclib"
	"github.com/njtc406/emberengine/engine/pkg/utils/errorlib"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
)

// TODO 这里有个东西可以优化,就是如果是cast消息,那么可以预先将消息创建好,避免每个客户端都重新封装一遍
// 但是需要考虑到如果是不同的连接方式,可能消息格式不同,需要做兼容处理

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

var busPool = pool.NewPoolEx(make(chan pool.IPoolData, 2048), func() pool.IPoolData {
	return &MessageBus{}
})

func NewMessageBus(sender inf.IRpcDispatcher, receiver inf.IRpcDispatcher, err error) *MessageBus {
	mb := busPool.Get().(*MessageBus)
	mb.sender = sender
	mb.receiver = receiver
	mb.err = err
	return mb
}

func ReleaseMessageBus(mb *MessageBus) {
	busPool.Put(mb)
}

func (mb *MessageBus) call(ctx context.Context, method string, in, out interface{}) error {
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
	deadline, ok := ctx.Deadline()
	if !ok {
		timeout = def.DefaultRpcTimeout
	} else {
		timeout = timelib.Now().Sub(deadline)
	}

	mt := monitor.GetRpcMonitor()

	// 创建请求
	envelope := msgenvelope.NewMsgEnvelope()
	envelope.WithContext(ctx)
	envelope.SetMethod(method)
	envelope.SetSenderPid(mb.sender.GetPid())
	envelope.SetReceiverPid(mb.receiver.GetPid())
	envelope.SetDispatcher(mb.sender)
	envelope.SetRequest(in)
	envelope.SetResponse(nil) // 容错
	envelope.SetReqId(mt.GenSeq())
	envelope.SetNeedResponse(true)
	envelope.SetTimeout(timeout)

	//log.SysLogger.Debugf("call envelope: %+v", envelope)

	// 加入等待队列
	mt.Add(envelope)

	// 发送消息
	if err := mb.receiver.SendRequest(envelope); err != nil {
		// 发送失败,释放资源
		mt.Remove(envelope.GetReqId())
		envelope.Release()
		log.SysLogger.WithContext(envelope.GetContext()).Errorf("service[%s] send message[%s] request to client failed, error: %v", mb.sender.GetPid().GetName(), envelope.GetMethod(), err)
		return def.RPCCallFailed
	}

	// 等待回复
	envelope.Wait()

	mt.Remove(envelope.GetReqId()) // 容错,不管有没有释放,都释放一次(实际上在所有设置done之前都会释放)

	if err := envelope.GetError(); err != nil {
		envelope.Release()
		return err
	}

	resp := envelope.GetResponse()

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

// Call 同步调用服务
func (mb *MessageBus) Call(ctx context.Context, method string, in, out interface{}) error {
	defer ReleaseMessageBus(mb)
	return mb.call(ctx, method, in, out)
}

// AsyncCall 异步调用服务
func (mb *MessageBus) AsyncCall(ctx context.Context, method string, in interface{}, param *dto.AsyncCallParams, callbacks ...dto.CompletionFunc) (dto.CancelRpc, error) {
	defer ReleaseMessageBus(mb)
	if mb.err != nil {
		// 这里可能是从MultiBus中产生的
		return nil, mb.err
	}
	if mb.sender == nil || mb.receiver == nil {
		return nil, fmt.Errorf("sender or receiver is nil")
	}
	if len(callbacks) == 0 {
		return nil, def.CallbacksIsEmpty
	}

	if mb.err != nil {
		// 这里可能是从MultiBus中产生的
		return nil, mb.err
	}

	var timeout time.Duration
	deadline, ok := ctx.Deadline()
	if !ok {
		timeout = def.DefaultRpcTimeout
	} else {
		timeout = timelib.Now().Sub(deadline)
	}

	mt := monitor.GetRpcMonitor()

	// 创建请求
	envelope := msgenvelope.NewMsgEnvelope()
	envelope.WithContext(ctx)
	envelope.SetMethod(method)
	envelope.SetSenderPid(mb.sender.GetPid())
	envelope.SetReceiverPid(mb.receiver.GetPid())
	envelope.SetDispatcher(mb.sender)
	envelope.SetRequest(in)
	envelope.SetResponse(nil) // 容错
	envelope.SetReqId(mt.GenSeq())
	envelope.SetNeedResponse(true)
	envelope.SetTimeout(timeout)
	envelope.SetCallback(callbacks)
	envelope.SetCallbackParams(param.Params)

	// 加入等待队列
	mt.Add(envelope)

	// 发送消息,最终callback调用将在response中被执行,所以envelope会在callback执行完后自动回收
	if err := mb.receiver.SendRequest(envelope); err != nil {
		// 发送失败,释放资源
		mt.Remove(envelope.GetReqId())
		envelope.Release()
		log.SysLogger.WithContext(envelope.GetContext()).Errorf("service[%s] send message[%s] request to client failed, error: %v", mb.sender.GetPid().GetName(), envelope.GetMethod(), err)
		return nil, def.RPCCallFailed
	}

	return mt.NewCancel(envelope.GetReqId()), nil
}

// Send 无返回调用
func (mb *MessageBus) Send(ctx context.Context, method string, in interface{}) error {
	defer ReleaseMessageBus(mb)
	if mb.err != nil {
		// 这里可能是从MultiBus中产生的
		return mb.err
	}
	if mb.receiver == nil {
		return fmt.Errorf("sender or receiver is nil")
	}
	if mb.err != nil {
		return mb.err
	}
	mt := monitor.GetRpcMonitor()

	// 创建请求
	envelope := msgenvelope.NewMsgEnvelope()
	envelope.SetMethod(method)
	envelope.WithContext(ctx)
	envelope.SetReceiverPid(mb.receiver.GetPid())
	envelope.SetDispatcher(mb.sender)
	envelope.SetRequest(in)
	envelope.SetResponse(nil) // 容错
	envelope.SetReqId(mt.GenSeq())
	envelope.SetNeedResponse(false) // 不需要回复

	// 如果是远程调用, 则由远程调用释放资源,如果是本地调用,则由接收者自行回收
	return mb.receiver.SendRequestAndRelease(envelope)
}

func (mb *MessageBus) Cast(ctx context.Context, method string, in interface{}) {
	if err := mb.Send(ctx, method, in); err != nil {
		log.SysLogger.WithContext(ctx).Errorf("cast service[%s] failed, error: %v", method, err)
	}
}

// TODO 这个还需要修改

// MultiBus 多节点调用
type MultiBus []inf.IBus

func (m MultiBus) Call(ctx context.Context, method string, in, out interface{}) error {
	if len(m) == 0 {
		log.SysLogger.WithContext(ctx).Errorf("===========select empty service to call %s", method)
		return def.ServiceIsUnavailable
	}

	if len(m) > 1 {
		// 释放所有节点
		for _, bus := range m {
			ReleaseMessageBus(bus.(*MessageBus))
		}
		return fmt.Errorf("only one node can be called at a time, now got %v", len(m))
	}

	// call只允许调用一个节点
	return m[0].Call(ctx, method, in, out)
}

func (m MultiBus) AsyncCall(ctx context.Context, method string, in interface{}, param *dto.AsyncCallParams, callbacks ...dto.CompletionFunc) (dto.CancelRpc, error) {
	if len(m) == 0 {
		log.SysLogger.WithContext(ctx).Errorf("===========select empty service to async call %s", method)
		return nil, def.ServiceIsUnavailable
	}
	if len(m) > 1 {
		// 释放所有节点
		for _, bus := range m {
			ReleaseMessageBus(bus.(*MessageBus))
		}
		return dto.EmptyCancelRpc, fmt.Errorf("only one node can be called at a time, now got %v", len(m))
	}
	// call只允许调用一个节点
	return m[0].AsyncCall(ctx, method, in, param, callbacks...)
}

func (m MultiBus) Send(ctx context.Context, method string, in interface{}) error {
	if len(m) == 0 {
		log.SysLogger.WithContext(ctx).Errorf("===========select empty service to send %s", method)
		return def.ServiceIsUnavailable
	}
	var errs []error
	for _, bus := range m {
		if err := bus.Send(ctx, method, in); err != nil {
			errs = append(errs, err)
		}
	}

	return errorlib.CombineErr(errs...)
}

func (m MultiBus) Cast(ctx context.Context, method string, in interface{}) {
	if len(m) == 0 {
		log.SysLogger.WithContext(ctx).Errorf("===========select empty service to send %s", method)
		return
	}

	_ = asynclib.Go(func() {
		for _, bus := range m {
			if err := bus.Send(ctx, method, in); err != nil {
				log.SysLogger.WithContext(ctx).Errorf("cast service[%s] failed, error: %v", method, err)
			}
		}
	})

	return
}
