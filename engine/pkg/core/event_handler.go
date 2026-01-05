// Package core
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/7/20 0020 10:18
// 最后更新:  yr  2025/7/20 0020 10:18
package core

import (
	"context"
	"fmt"

	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/profiler"
)

// TODO 这个函数需要修改,如果pprof做成了模块,那么这里就不需要什么open这些字段了
// EventHandler 定义事件处理函数类型
type EventHandler func(ctx context.Context, ev inf.IEvent, open bool, analyzer *profiler.Analyzer)

// 初始化事件处理器
func (s *Service) initEventHandlers() {
	s.eventHandlers = make(map[int32]EventHandler)
	// 注册事件处理器
	s.RegisterUserHandler(event.ServiceSuspended, s.handleServiceSuspended)
	s.RegisterUserHandler(event.ServiceResumed, s.handleServiceResumed)
	s.RegisterUserHandler(event.SysEventServiceClose, s.handleServiceClose)
	s.RegisterUserHandler(event.ServiceHeartbeat, s.handleServiceHeartbeat)
	s.RegisterUserHandler(event.ServiceTimerCallback, s.handleTimerCallback)
	s.RegisterUserHandler(event.ServiceConcurrentCallback, s.handleConcurrentCallback)
	s.RegisterUserHandler(event.RpcMsg, s.handleUserRpcMsg)
}

// RegisterUserHandler 注册事件处理器
func (s *Service) RegisterUserHandler(tp int32, handler EventHandler) {
	s.eventHandlers[tp] = handler
}

// InvokeMessage 处理事件(这个函数是在mailbox的线程中被调用的)
func (s *Service) InvokeMessage(ctx context.Context, ev inf.IEvent) {
	if !ev.IsRef() {
		// 前面的超时之后导致后面的已经被丢弃
		return
	}
	defer ev.Release()

	for _, hook := range s.msgHooks {
		if !hook(ev) {
			break
		}
	}

	tp := ev.GetType()
	//s.logger.WithContext(ctx).Debugf(">>>>>>>>>>>>>>>>>>>>>>>>>>>>>>service[%s] receive user event[%d]", s.GetName(), tp)

	var analyzer *profiler.Analyzer
	open := s.profiler != nil
	defer func() {
		if analyzer != nil {
			analyzer.Pop()
		}
	}()

	// 查找注册的处理器
	if handler, ok := s.eventHandlers[tp]; ok {
		s.safeExec(func() {
			if open {
				analyzer = s.profiler.Push(fmt.Sprintf("[EVENT] type:%d", tp))
			}
			handler(ctx, ev, open, analyzer)
		})
	} else {
		// 默认处理器
		s.safeExec(func() {
			if open {
				analyzer = s.profiler.Push(fmt.Sprintf("[OTHER_EVENT] type:%d", tp))
			}
			s.eventProcessor.EventHandler(ctx, ev)
		})
	}
}

// 具体的系统消息处理器实现
func (s *Service) handleServiceSuspended(ctx context.Context, ev inf.IEvent, _ bool, _ *profiler.Analyzer) {
	// 服务挂起
	s.mailbox.Suspend()
}

func (s *Service) handleServiceResumed(ctx context.Context, ev inf.IEvent, _ bool, _ *profiler.Analyzer) {
	// 服务恢复
	s.mailbox.Resume()
}

func (s *Service) handleServiceClose(ctx context.Context, ev inf.IEvent, _ bool, _ *profiler.Analyzer) {
	// 服务关闭
	go s.Stop()
}

func (s *Service) handleServiceHeartbeat(ctx context.Context, ev inf.IEvent, _ bool, _ *profiler.Analyzer) {
	// 服务健康检查
	// TODO 需要回复服务负载等等信息
}

// handleUserRpcMsg 处理用户RPC消息事件
func (s *Service) handleUserRpcMsg(ctx context.Context, ev inf.IEvent, open bool, analyzer *profiler.Analyzer) {
	c := ev.(inf.IEnvelope)
	meta := c.GetMeta()
	data := c.GetData()

	if meta == nil || data == nil {
		s.logger.WithContext(ctx).Errorf("service[%s] receive rpc msg call error, meta or data is nil", s.GetName())
		s.logger.WithContext(ctx).Errorf("meta: %v", meta)
		s.logger.WithContext(ctx).Errorf("data: %v", data)
		return
	}

	if data.IsReply() {
		if open {
			analyzer = s.profiler.Push(fmt.Sprintf("[USER_RPC_RESP] service:%s method:%s",
				meta.GetReceiverPid().GetServiceUid(), data.GetMethod()))
		}
		s.HandleResponse(ctx, c)
	} else {
		if open {
			analyzer = s.profiler.Push(fmt.Sprintf("[USER_RPC_REQ] service:%s method:%s",
				meta.GetReceiverPid().GetServiceUid(), data.GetMethod()))
		}
		s.HandleRequest(ctx, c)
	}
}

// handleTimerCallback 处理定时器回调事件
func (s *Service) handleTimerCallback(ctx context.Context, ev inf.IEvent, open bool, analyzer *profiler.Analyzer) {
	evt := ev.(*event.TimerEnvelope)
	t := evt.Payload
	if open {
		analyzer = s.profiler.Push(fmt.Sprintf("[USER_TIME_CB] name:%s", t.GetName()))
	}

	if err := t.Do(); err != nil {
		s.WithContext(ctx).Errorf("timer callback error: %v", err)
	}
}

// handleConcurrentCallback 处理并发回调事件
func (s *Service) handleConcurrentCallback(ctx context.Context, ev inf.IEvent, open bool, analyzer *profiler.Analyzer) {
	evt := ev.(*event.CallbackEnvelope)
	cb := evt.Payload
	if open {
		analyzer = s.profiler.Push(fmt.Sprintf("[USER_ASYNC_CB] name:%s", cb.GetName()))
	}
	cb.DoCallback(ctx)
}
