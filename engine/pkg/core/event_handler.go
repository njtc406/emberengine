// Package core
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/7/20 0020 10:18
// 最后更新:  yr  2025/7/20 0020 10:18
package core

import (
	"context"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

// TODO 这个函数需要修改,如果pprof做成了模块,那么这里就不需要什么open这些字段了
// EventHandler 定义事件处理函数类型
type EventHandler func(ctx context.Context, ev inf.IEvent) error

// 初始化事件处理器
func (s *Service) initEventHandlers() {
	s.eventHandlers = make(map[int32]EventHandler)
	// 注册事件处理器
	s.RegisterUserHandler(event.ServiceSuspended, s.handleServiceSuspended)
	s.RegisterUserHandler(event.ServiceResumed, s.handleServiceResumed)
	s.RegisterUserHandler(event.SysEventServiceClose, s.handleServiceClose)
	s.RegisterUserHandler(event.ServiceFinalize, s.handleServiceFinalize)
	s.RegisterUserHandler(event.ServiceHeartbeat, s.handleServiceHeartbeat)
	s.RegisterUserHandler(event.ServiceTimerCallback, s.handleTimerCallback)
	s.RegisterUserHandler(event.ServiceConcurrentCallback, s.handleConcurrentCallback)
	s.RegisterUserHandler(event.RpcMsg, s.handleUserRpcMsg)
}

// RegisterUserHandler 注册事件处理器(非并发安全!)
func (s *Service) RegisterUserHandler(tp int32, handler EventHandler) {
	s.eventHandlers[tp] = handler
}

// InvokeMessage 处理事件(这个函数是在mailbox的线程中被调用的)
func (s *Service) InvokeMessage(ctx context.Context, ev inf.IEvent) error {
	if !ev.IsRef() {
		// 前面的超时之后导致后面的已经被丢弃
		return def.ErrEventIsUnRef
	}
	defer ev.Release()

	//for _, hook := range s.msgHooks {
	//	if !hook(ev) {
	//		break
	//	}
	//}

	tp := ev.GetType()
	//s.logger.WithContext(ctx).Debugf(">>>>>>>>>>>>>>>>>>>>>>>>>>>>>>service[%s] receive user event[%d]", s.GetName(), tp)

	//var analyzer *profiler.Analyzer
	//open := s.profiler != nil
	//defer func() {
	//	if analyzer != nil {
	//		analyzer.Pop()
	//	}
	//}()

	// 查找注册的处理器
	begin := time.Now() // 需要使用真实时间
	defer func() {
		cost := time.Since(begin)
		if cost > time.Millisecond*10 {
			s.Slow().WithContext(ctx).Infof("event[%d] cost %v", tp, cost)
		}
	}()
	if handler, ok := s.eventHandlers[tp]; ok {
		return handler(ctx, ev)
	} else {
		// 默认处理器
		err := s.safeExec(func() {
			s.eventProcessor.EventHandler(ctx, ev)
		})
		if err != nil {
			return err
		}

	}
	return nil
}

// 具体的系统消息处理器实现
func (s *Service) handleServiceSuspended(ctx context.Context, ev inf.IEvent) error {
	// 服务挂起
	s.mailbox.Suspend()
	return nil
}

func (s *Service) handleServiceResumed(ctx context.Context, ev inf.IEvent) error {
	// 服务恢复
	s.mailbox.Resume()
	return nil
}

func (s *Service) handleServiceClose(ctx context.Context, ev inf.IEvent) error {
	// 服务关闭：请求停止，由 handleServiceFinalize 在 mailbox 内完成清理
	s.RequestStop()
	return nil
}

// handleServiceFinalize 在 mailbox worker 内执行清理（串行、无并发风险）
func (s *Service) handleServiceFinalize(ctx context.Context, ev inf.IEvent) error {
	s.doFinalize()
	return nil
}

func (s *Service) handleServiceHeartbeat(ctx context.Context, ev inf.IEvent) error {
	// 服务健康检查
	// TODO 需要回复服务负载等等信息
	return nil
}

// handleUserRpcMsg 处理用户RPC消息事件
func (s *Service) handleUserRpcMsg(ctx context.Context, ev inf.IEvent) error {
	c := ev.(inf.IEnvelope)
	meta := c.GetMeta()
	data := c.GetData()

	if meta == nil || data == nil {
		s.logger.WithContext(ctx).WithFields(map[string]interface{}{"meta": meta, "data": data}).Errorf("receive rpc msg call error, meta or data is nil")
		return def.ErrRpcMsgMetaOrDataIsNil
	}

	if data.IsReply() {
		//if open {
		//	analyzer = s.profiler.Push(fmt.Sprintf("[USER_RPC_RESP] service:%s method:%s",
		//		meta.GetReceiverPid().GetServiceUid(), data.GetMethod()))
		//}
		return s.HandleResponse(ctx, c)
	} else {
		//if open {
		//	analyzer = s.profiler.Push(fmt.Sprintf("[USER_RPC_REQ] service:%s method:%s",
		//		meta.GetReceiverPid().GetServiceUid(), data.GetMethod()))
		//}
		return s.HandleRequest(ctx, c)
	}
}

// handleTimerCallback 处理定时器回调事件
func (s *Service) handleTimerCallback(ctx context.Context, ev inf.IEvent) error {
	evt := ev.(*event.TimerEnvelope)
	t := evt.Payload
	//if open {
	//	analyzer = s.profiler.Push(fmt.Sprintf("[USER_TIME_CB] name:%s", t.GetName()))
	//}

	if err := t.Do(); err != nil {
		s.WithContext(ctx).Errorf("timer callback error: %v", err)
		return err
	}
	return nil
}

// handleConcurrentCallback 处理并发回调事件
func (s *Service) handleConcurrentCallback(ctx context.Context, ev inf.IEvent) error {
	evt := ev.(*event.CallbackEnvelope)
	cb := evt.Payload
	//if open {
	//	analyzer = s.profiler.Push(fmt.Sprintf("[USER_ASYNC_CB] name:%s", cb.GetName()))
	//}
	cb.DoCallback(ctx)
	return nil
}
