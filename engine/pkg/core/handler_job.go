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
	"github.com/njtc406/emberengine/engine/pkg/utils/codec"
	"github.com/njtc406/emberengine/engine/pkg/utils/timelib"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
	"github.com/njtc406/emberengine/engine/pkg/utils/xcontext"
)

// JobHandler 泛型 Job 处理器，T 是 payload 类型
type JobHandler[T any] func(ctx context.Context, payload T) error

// jobHandlerWrapper 包装后的统一处理器（内部使用）
type jobHandlerWrapper func(ctx context.Context, job inf.IMailboxJob) error

// jobHandlerRegistry Job 处理器注册表
type jobHandlerRegistry struct {
	handlers map[def.MailboxJobType]jobHandlerWrapper
}

// newJobHandlerRegistry 创建新的注册表
func newJobHandlerRegistry() *jobHandlerRegistry {
	return &jobHandlerRegistry{
		handlers: make(map[def.MailboxJobType]jobHandlerWrapper),
	}
}

// registerJobHandler 注册 Job 处理器
// registry: 目标注册表
// jobType: 要注册的 job 类型
// handler: 具体的处理器函数，接收 ctx 和 payload 作为参数
// 使用方式: registerJobHandler(registry, def.MailboxJobTypeRpc, func(ctx context.Context, env inf.IEnvelope) error { ... })
func registerJobHandler[T any](registry *jobHandlerRegistry, jobType def.MailboxJobType, handler JobHandler[T]) {
	registry.handlers[jobType] = func(ctx context.Context, j inf.IMailboxJob) error {
		payload := job.GetJobPayloadAs[T](j)
		return handler(ctx, payload)
	}
}

// InvokeJob 调用已注册的处理器
// ctx: 仅携带上下文信息（如 traceId），不带取消控制
// 超时控制由 job.GetDeadline() 决定，在此函数内部强制执行
func (r *jobHandlerRegistry) InvokeJob(ctx context.Context, job inf.IMailboxJob) error {
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
			// 执行 job handler 并返回结果
			return handler(ctxx, job)
		})
	}()

	// TODO 这里应该还需要一个回滚机制，如果执行失败，需要回滚数据

	// 等待完成或超时/取消
	select {
	case err := <-done:
		return err
	case <-ctxx.Done():
		// 超时或被取消
		return ctxx.Err()
	}
}

func (r *jobHandlerRegistry) safeExec(f func() error) (err error) {
	defer func() {
		if recoverErr := recover(); recoverErr != nil {
			err = fmt.Errorf("safe exec error: %v\nstack:%s", recoverErr, string(debug.Stack()))
		}
	}()

	return f()
}

// ============= Service 层集成 =============

// initJobHandlers 初始化并注册所有 job handlers
func (s *Service) initJobHandlers() {
	s.jobRegistry = newJobHandlerRegistry()

	// 注册内置的 job handlers - payload 类型在编译期确定
	registerJobHandler(s.jobRegistry, def.MailboxJobTypeRpc, s.handleRpcJob)
	registerJobHandler(s.jobRegistry, def.MailboxJobTypeEvent, s.handleEventJob)
	registerJobHandler(s.jobRegistry, def.MailboxJobTypeTimer, s.handleTimerJob)
	registerJobHandler(s.jobRegistry, def.MailboxJobTypeConcurrentCallback, s.handleConcurrentCallbackJob)
	registerJobHandler(s.jobRegistry, def.MailboxJobTypeSysCtl, s.handleSysCtlJob)
}

// RegisterCustomJobHandler 允许用户注册自定义 Job Handler
func RegisterCustomJobHandler[T any](s *Service, jobType def.MailboxJobType, handler JobHandler[T]) {
	registerJobHandler(s.jobRegistry, jobType, handler)
}

// ============= 具体 Handler 实现 =============

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

func (s *Service) handleEventJob(ctx context.Context, evt *actor.Event) error {
	return s.safeExec(func() error {
		// 将 anypb.Any 解码为具体类型
		payload := evt.GetPayload()
		data, err := codec.DecodeFromAny(payload)
		if err != nil {
			s.WithContext(ctx).Errorf("decode event payload error: %v", err)
			return err
		}
		s.eventProcessor.Trigger(ctx, evt.GetEventType(), data)
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
	if callback == nil {
		return nil
	}
	return s.safeExec(func() error {
		callback.DoCallback(ctx)
		return nil
	})
}

func (s *Service) handleSysCtlJob(ctx context.Context, cmd dto.SysCmd) error {
	// cmd 已经是断言后的 dto.SysCmd 类型
	return s.handleSysCtl(ctx, cmd)
}

// handleSysCtl 处理系统控制命令
func (s *Service) handleSysCtl(ctx context.Context, cmd dto.SysCmd) error {
	s.WithContext(ctx).Infof("sys ctl cmd: %s", cmd.Cmd)
	// TODO 这里需要实现：依然需要一个注册中心来管理这些系统控制命令
	// 1. mailbox的挂起/恢复控制
	// 2. health check 控制
	// 3. 其他系统控制命令
	return nil
}

// ExecuteJob 实现 IMessageInvoker 接口
func (s *Service) ExecuteJob(ctx context.Context, job inf.IMailboxJob) error {
	err := s.jobRegistry.InvokeJob(ctx, job)
	if err != nil {
		s.WithContext(ctx).Errorf("invoke job error: %v", err)
		return err
	}
	return nil
}
