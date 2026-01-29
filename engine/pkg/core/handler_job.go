// Package core
// 模块名: Job Handler 自动注册与断言系统
// 功能描述: 提供泛型 Job Handler 注册，自动完成 payload 类型断言
// 作者:  yr  2026/1/30 00:35
// 最后更新:  yr  2026/1/30 00:35
package core

import (
	"context"
	"fmt"
	"runtime/debug"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/actor/mailbox/job"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/dto"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/timelib"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
	"github.com/njtc406/emberengine/engine/pkg/utils/xcontext"
)

// JobHandler 泛型 Job 处理器，T 是 payload 类型
type JobHandler[T any] func(ctx context.Context, payload T) error

// jobHandlerWrapper 包装后的统一处理器（内部使用）
type jobHandlerWrapper func(ctx context.Context, job inf.IMailboxJob) error

// JobHandlerRegistry Job 处理器注册表
type JobHandlerRegistry struct {
	handlers map[def.MailboxJobType]jobHandlerWrapper
}

// NewJobHandlerRegistry 创建新的注册表
func NewJobHandlerRegistry() *JobHandlerRegistry {
	return &JobHandlerRegistry{
		handlers: make(map[def.MailboxJobType]jobHandlerWrapper),
	}
}

// RegisterJobHandler 泛型注册函数 - 自动完成类型断言绑定
// 使用方式: RegisterJobHandler(registry, def.MailboxJobTypeRpc, func(ctx context.Context, env inf.IEnvelope) error { ... })
func RegisterJobHandler[T any](registry *JobHandlerRegistry, jobType def.MailboxJobType, handler JobHandler[T]) {
	registry.handlers[jobType] = func(ctx context.Context, j inf.IMailboxJob) error {
		// 自动断言: 从 job 中提取 payload 并转换为目标类型
		payload := job.GetJobPayloadAs[T](j)
		return handler(ctx, payload)
	}
}

// InvokeJob 调用已注册的处理器
// ctx: 仅携带上下文信息（如 traceId），不带取消控制
// 超时控制由 job.GetDeadline() 决定，在此函数内部强制执行
func (r *JobHandlerRegistry) InvokeJob(ctx context.Context, job inf.IMailboxJob) error {
	handler, ok := r.handlers[job.GetType()]
	if !ok {
		// TODO 后面这些错误信息都使用errorx来包裹
		return def.ErrJobHandlerNotFound
	}

	var ctxx *xcontext.XContext
	var cancel context.CancelFunc

	deadline := job.GetDeadline()
	if !deadline.IsZero() {
		timeout := deadline.Sub(timelib.Now())
		if timeout <= 0 {
			return def.ErrJobTimeout
		}
		ctxx, cancel = xcontext.NewWithTimeout(ctx, timeout)
	} else {
		ctxx, cancel = xcontext.NewWithCancel(ctx)
	}
	defer cancel()

	// 使用 channel 获取 handler 执行结果
	done := make(chan error, 1)
	go func() {
		done <- r.safeExec(func() error {
			return handler(ctxx, job)
		})
	}()

	// 等待完成或超时/取消
	select {
	case err := <-done:
		return err
	case <-ctxx.Done():
		// 超时或被取消
		return ctxx.Err()
	}
}

func (s *JobHandlerRegistry) safeExec(f func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("safe exec error: %v\nstack:%s", r, debug.Stack())
		}
	}()
	err = f()
	return err
}

// ============= Service 层集成 =============

// initJobHandlers 初始化并注册所有 job handlers
func (s *Service) initJobHandlers() {
	s.jobRegistry = NewJobHandlerRegistry()

	// 注册内置的 job handlers - payload 类型在编译期确定
	RegisterJobHandler(s.jobRegistry, def.MailboxJobTypeRpc, s.handleRpcJob)
	RegisterJobHandler(s.jobRegistry, def.MailboxJobTypeEvent, s.handleEventBusJob)
	RegisterJobHandler(s.jobRegistry, def.MailboxJobTypeInternalEvent, s.handleInternalEventJob)
	RegisterJobHandler(s.jobRegistry, def.MailboxJobTypeTimer, s.handleTimerJob)
	RegisterJobHandler(s.jobRegistry, def.MailboxJobTypeConcurrentCallback, s.handleConcurrentCallbackJob)
	RegisterJobHandler(s.jobRegistry, def.MailboxJobSysCtl, s.handleSysCtlJob)
}

// RegisterCustomJobHandler 允许用户注册自定义 Job Handler
func RegisterCustomJobHandler[T any](s *Service, jobType def.MailboxJobType, handler JobHandler[T]) {
	RegisterJobHandler(s.jobRegistry, jobType, handler)
}

// ============= 具体 Handler 实现 =============
// 直接使用断言后的类型，无需手动断言！

func (s *Service) handleRpcJob(ctx context.Context, envelope inf.IEnvelope) error {
	meta := envelope.GetMeta()
	data := envelope.GetData()

	if meta == nil || data == nil {
		s.WithContext(ctx).WithFields(map[string]interface{}{"meta": meta, "data": data}).Errorf("receive rpc msg error, meta or data is nil")
		return def.ErrRpcMsgMetaOrDataIsNil
	}

	if data.IsReply() {
		//if open {
		//	analyzer = s.profiler.Push(fmt.Sprintf("[USER_RPC_RESP] service:%s method:%s",
		//		meta.GetReceiverPid().GetServiceUid(), data.GetMethod()))
		//}
		return s.HandleResponse(ctx, envelope)
	} else {
		//if open {
		//	analyzer = s.profiler.Push(fmt.Sprintf("[USER_RPC_REQ] service:%s method:%s",
		//		meta.GetReceiverPid().GetServiceUid(), data.GetMethod()))
		//}
		return s.HandleRequest(ctx, envelope)
	}
}

func (s *Service) handleEventBusJob(ctx context.Context, event *actor.Event) error {
	return s.safeExec(func() error {
		s.globalEventProcessor.EventHandler(ctx, event)
		return nil
	})
}

func (s *Service) handleInternalEventJob(ctx context.Context, ev inf.IEvent) error {
	return s.safeExec(func() error {
		s.eventProcessor.EventHandler(ctx, ev)
		return nil
	})
}

func (s *Service) handleTimerJob(ctx context.Context, t timingwheel.ITimer) error {
	if t == nil {
		return nil
	}

	return s.safeExec(func() error {
		return t.Do(ctx)
	})
}

func (s *Service) handleConcurrentCallbackJob(ctx context.Context, callback inf.IConcurrentCallback) error {
	if callback != nil {
		callback.DoCallback(ctx)
	}
	return nil
}

func (s *Service) handleSysCtlJob(ctx context.Context, cmd dto.SysCmd) error {
	// cmd 已经是断言后的 dto.SysCmd 类型
	return s.handleSysCtl(ctx, cmd)
}

// handleSysCtl 处理系统控制命令
func (s *Service) handleSysCtl(ctx context.Context, cmd dto.SysCmd) error {
	// TODO: 实现系统控制命令处理逻辑
	return nil
}

// InvokeJob 实现 IMessageInvoker 接口
func (s *Service) InvokeJob(ctx context.Context, job inf.IMailboxJob) error {
	err := s.jobRegistry.InvokeJob(ctx, job)
	if err != nil {
		s.WithContext(ctx).Errorf("invoke job error: %v", err)
		return err
	}
	return nil
}
