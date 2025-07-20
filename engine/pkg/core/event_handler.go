// Package core
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/7/20 0020 10:18
// 最后更新:  yr  2025/7/20 0020 10:18
package core

import (
	"fmt"
	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/profiler"
	"github.com/njtc406/emberengine/engine/pkg/utils/concurrent"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
)

// 定义事件处理函数类型
type eventHandler func(ev inf.IEvent, open bool, analyzer *profiler.Analyzer)

// 初始化事件处理器
func (s *Service) initEventHandlers() {
	s.userEventHandlers = make(map[int32]eventHandler)
	s.sysEventHandlers = make(map[int32]eventHandler)

	// 注册系统事件处理器
	s.registerSystemHandler(event.ServiceSuspended, s.handleServiceSuspended)
	s.registerSystemHandler(event.ServiceResumed, s.handleServiceResumed)
	s.registerSystemHandler(event.SysEventServiceClose, s.handleServiceClose)
	s.registerSystemHandler(event.ServiceHeartbeat, s.handleServiceHeartbeat)
	s.registerSystemHandler(event.ServiceGlobalEventTrigger, s.handleSystemGlobalEvent)
	s.registerSystemHandler(event.RpcMsg, s.handleSystemRpcMsg)

	// 注册各种用户事件处理器
	s.registerUserHandler(event.RpcMsg, s.handleUserRpcMsg)
	s.registerUserHandler(event.ServiceTimerCallback, s.handleTimerCallback)
	s.registerUserHandler(event.ServiceConcurrentCallback, s.handleConcurrentCallback)
	s.registerUserHandler(event.ServiceGlobalEventTrigger, s.handleUserGlobalEvent)
}

// 注册事件处理器
func (s *Service) registerUserHandler(tp int32, handler eventHandler) {
	s.userEventHandlers[tp] = handler
}

// 注册系统消息处理器
func (s *Service) registerSystemHandler(tp int32, handler eventHandler) {
	s.sysEventHandlers[tp] = handler
}

// InvokeSystemMessage 处理系统事件(这个函数是在mailbox的线程中被调用的)
func (s *Service) InvokeSystemMessage(ev inf.IEvent) {
	if !ev.IsRef() {
		return
	}
	defer ev.Release()

	for _, hook := range s.sysMsgHooks {
		if !hook(ev) {
			break
		}
	}

	tp := ev.GetType()
	//s.logger.WithContext(ev.GetContext()).Debugf("service[%s] receive system event[%d]", s.GetName(), tp)

	var analyzer *profiler.Analyzer
	open := s.profiler != nil

	defer func() {
		if analyzer != nil {
			analyzer.Pop()
		}
	}()

	// 查找注册的处理器
	if handler, ok := s.sysEventHandlers[tp]; ok {
		// 对于需要安全执行的处理器，在外层包装safeExec
		if tp == event.ServiceGlobalEventTrigger || tp == event.RpcMsg {
			s.safeExec(func() {
				if open {
					analyzer = s.profiler.Push(fmt.Sprintf("[SYS_MSG] type:%d", tp))
				}
				handler(ev, open, analyzer)
			})
		} else {
			if open {
				analyzer = s.profiler.Push(fmt.Sprintf("[SYS_MSG] type:%d", tp))
			}
			handler(ev, open, analyzer)
		}
	} else {
		// 默认处理器
		if open {
			analyzer = s.profiler.Push(fmt.Sprintf("[SYS_OTHER_EVENT] type:%d", tp))
		}
		s.eventProcessor.EventHandler(ev)
	}
}

// InvokeUserMessage 处理用户事件(这个函数是在mailbox的线程中被调用的)
func (s *Service) InvokeUserMessage(ev inf.IEvent) {
	if !ev.IsRef() {
		// 前面的超时之后导致后面的已经被丢弃
		return
	}
	defer ev.Release()

	for _, hook := range s.userMsgHooks {
		if !hook(ev) {
			break
		}
	}

	tp := ev.GetType()
	//s.logger.WithContext(ev.GetContext()).Debugf("service[%s] receive user event[%d]", s.GetName(), tp)

	var analyzer *profiler.Analyzer
	open := s.profiler != nil
	defer func() {
		if analyzer != nil {
			analyzer.Pop()
		}
	}()

	// 查找注册的处理器
	if handler, ok := s.userEventHandlers[tp]; ok {
		s.safeExec(func() {
			handler(ev, open, analyzer)
		})
	} else {
		// 默认处理器
		s.safeExec(func() {
			if open {
				analyzer = s.profiler.Push(fmt.Sprintf("[USER_OTHER_EVENT] type:%d", tp))
			}
			s.eventProcessor.EventHandler(ev)
		})
	}
}

// 具体的系统消息处理器实现
func (s *Service) handleServiceSuspended(ev inf.IEvent, _ bool, _ *profiler.Analyzer) {
	// 服务挂起
	s.mailbox.Suspend()
}

func (s *Service) handleServiceResumed(ev inf.IEvent, _ bool, _ *profiler.Analyzer) {
	// 服务恢复
	s.mailbox.Resume()
}

func (s *Service) handleServiceClose(ev inf.IEvent, _ bool, _ *profiler.Analyzer) {
	// 服务关闭
	s.Stop()
}

func (s *Service) handleServiceHeartbeat(ev inf.IEvent, _ bool, _ *profiler.Analyzer) {
	// 服务健康检查
	// TODO 需要回复服务负载等等信息
}

func (s *Service) handleSystemGlobalEvent(ev inf.IEvent, open bool, analyzer *profiler.Analyzer) {
	evt := ev.(*event.Event)
	t := evt.Data.(*actor.Event)
	if open {
		analyzer = s.profiler.Push(fmt.Sprintf("[SYS_GLB_EVENT] type:%d", t.GetType()))
	}
	s.globalEventProcessor.EventHandler(t)
}

func (s *Service) handleSystemRpcMsg(ev inf.IEvent, open bool, analyzer *profiler.Analyzer) {
	c := ev.(inf.IEnvelope)
	if !c.IsRef() {
		// 已经被释放了,可能是本地调用者取消或者超时
		return
	}

	meta := c.GetMeta()
	data := c.GetData()
	if meta == nil || data == nil {
		s.logger.WithContext(c.GetContext()).Errorf("service[%s] receive call error, meta or data is nil", s.GetName())
		s.logger.WithContext(c.GetContext()).Errorf("meta: %v", meta)
		s.logger.WithContext(c.GetContext()).Errorf("data: %v", data)
		return
	}

	if data.IsReply() {
		if open {
			analyzer = s.profiler.Push(fmt.Sprintf("[SYS_RPC_RESP] service:%s method:%s",
				meta.GetReceiverPid().GetServiceUid(), data.GetMethod()))
		}
		s.HandleResponse(c)
	} else {
		if open {
			analyzer = s.profiler.Push(fmt.Sprintf("[SYS_RPC_REQ] service:%s method:%s",
				meta.GetReceiverPid().GetServiceUid(), data.GetMethod()))
		}
		s.HandleRequest(c)
	}
}

// 具体的事件处理器实现
func (s *Service) handleUserRpcMsg(ev inf.IEvent, open bool, analyzer *profiler.Analyzer) {
	c := ev.(inf.IEnvelope)
	meta := c.GetMeta()
	data := c.GetData()

	if meta == nil || data == nil {
		s.logger.WithContext(c.GetContext()).Errorf("service[%s] receive rpc msg call error, meta or data is nil", s.GetName())
		s.logger.WithContext(c.GetContext()).Errorf("meta: %v", meta)
		s.logger.WithContext(c.GetContext()).Errorf("data: %v", data)
		return
	}

	if data.IsReply() {
		if open {
			analyzer = s.profiler.Push(fmt.Sprintf("[USER_RPC_RESP] service:%s method:%s",
				meta.GetReceiverPid().GetServiceUid(), data.GetMethod()))
		}
		s.HandleResponse(c)
	} else {
		if open {
			analyzer = s.profiler.Push(fmt.Sprintf("[USER_RPC_REQ] service:%s method:%s",
				meta.GetReceiverPid().GetServiceUid(), data.GetMethod()))
		}
		s.HandleRequest(c)
	}
}

func (s *Service) handleTimerCallback(ev inf.IEvent, open bool, analyzer *profiler.Analyzer) {
	evt := ev.(*event.Event)
	t := evt.Data.(timingwheel.ITimer)
	if open {
		analyzer = s.profiler.Push(fmt.Sprintf("[USER_TIME_CB] name:%s", t.GetName()))
	}
	t.Do()
}

func (s *Service) handleConcurrentCallback(ev inf.IEvent, open bool, analyzer *profiler.Analyzer) {
	evt := ev.(*event.Event)
	t := evt.Data.(concurrent.IConcurrentCallback)
	if open {
		analyzer = s.profiler.Push(fmt.Sprintf("[USER_ASYNC_CB] name:%s", t.GetName()))
	}
	t.DoCallback()
}

func (s *Service) handleUserGlobalEvent(ev inf.IEvent, open bool, analyzer *profiler.Analyzer) {
	evt := ev.(*event.Event)
	t := evt.Data.(*actor.Event)
	if open {
		analyzer = s.profiler.Push(fmt.Sprintf("[USER_GLB_EVENT] type:%d", t.GetType()))
	}
	s.globalEventProcessor.EventHandler(t)
}
