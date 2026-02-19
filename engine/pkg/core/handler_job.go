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
	"time"

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
// 超时控制由 job.GetDeadline() 决定，通过 context 传导给 handler
//
// 【同步执行】handler 在当前 goroutine 同步执行，返回时 handler 已完全完成。
// 这保证了：
// 1. InvokeJob 返回后 Job 可安全释放（无 use-after-free）
// 2. 外层 rwMu 锁的保护范围覆盖 handler 的完整执行生命周期
// 3. handler 必须 respect context cancellation/timeout 以避免无限阻塞
//
// 【事务机制】由 Service.ExecuteJob 管理事务钩子：
// handler 成功 → 执行 commit 钩子；handler 失败 → 执行 rollback 钩子。
// 钩子在 Service.Init 阶段一次性注册，内部自行根据脏标记决定实际操作。
func (r *jobHandlerRegistry) InvokeJob(ctx context.Context, mJob inf.IMailboxJob) error {
	handler, ok := r.handlers[mJob.GetType()]
	if !ok {
		return def.ErrJobHandlerNotFound
	}

	deadline := mJob.GetDeadline()
	if deadline > 0 {
		deadlineTime := time.Unix(deadline, 0)
		timeout := deadlineTime.Sub(timelib.Now())
		if timeout <= 0 {
			return def.ErrJobTimeout
		}
		ctxx, cancel := xcontext.NewWithTimeout(ctx, timeout)
		defer cancel()
		ctx = ctxx
	}

	// 同步执行 handler
	return r.safeExec(func() error {
		return handler(ctx, mJob)
	})
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
	defer func() {
		envelope.Release()
	}()
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
//
// 事务语义：
//   - handler 成功 → 执行所有 commit 钩子（如：清除脏标记、刷盘等）
//   - handler 失败 → 执行所有 rollback 钩子（如：恢复快照数据）
//
// 钩子在 Init 阶段注册，内部自行根据脏标记决定是否需要实际操作。
// 此时 Job 仍存活、外层 rwMu 仍持有，业务状态可安全操作。
func (s *Service) ExecuteJob(ctx context.Context, job inf.IMailboxJob) error {
	err := s.jobRegistry.InvokeJob(ctx, job)

	if err != nil {
		// handler 失败（含 panic→error）：执行 rollback 钩子
		if s.txHookMgr.HasRollback() {
			s.txHookMgr.Rollback()
		}
		s.WithContext(ctx).Errorf("invoke job[%+v] error: %v\nstack:%s", job, err, string(debug.Stack()))
		return err
	}

	// handler 成功：执行 commit 钩子
	if s.txHookMgr.HasCommit() {
		s.txHookMgr.Commit()
	}
	return nil
}

// RegisterCommit 注册 commit 钩子（Init 阶段调用，注册后不可修改）。
//
// 每次写操作(Write) Job handler 成功后，框架按 LIFO 顺序调用所有 commit 钩子。
// 钩子内部自行根据脏标记等条件判断是否需要执行实际操作（如清除脏标记、刷盘等）。
// 读操作(Read)路径并发执行，不触发事务钩子。
//
// 使用示例（在 Init/OnInit 中注册）：
//
//	func (s *MyService) OnInit() error {
//	    s.RegisterCommit(func() {
//	        if s.balanceDirty {
//	            s.balanceDirty = false  // 清除脏标记，确认本次修改
//	        }
//	    })
//	    return nil
//	}
func (s *Service) RegisterCommit(fn TxHookFunc) {
	s.txHookMgr.RegisterCommit(fn)
}

// RegisterRollback 注册 rollback 钩子（Init 阶段调用，注册后不可修改）。
//
// 每次写操作(Write) Job handler 失败后，框架按 LIFO 顺序调用所有 rollback 钩子。
// 钩子内部自行根据脏标记等条件判断是否需要执行实际回滚（如恢复快照数据）。
// 读操作(Read)路径并发执行，不触发事务钩子。
//
// 使用示例（在 Init/OnInit 中注册）：
//
//	func (s *MyService) OnInit() error {
//	    s.RegisterRollback(func() {
//	        if s.balanceDirty {
//	            s.balance = s.balanceSnapshot  // 恢复到修改前的快照
//	            s.balanceDirty = false
//	        }
//	    })
//	    return nil
//	}
func (s *Service) RegisterRollback(fn TxHookFunc) {
	s.txHookMgr.RegisterRollback(fn)
}
