// Package core
// 模块名: SysCtl 命令注册中心
// 功能描述: Service 内部系统控制命令的注册/分发，配套 SysCtlJob 的同步串行执行
// 作者:  yr  2026/4/27
// 最后更新:  yr  2026/4/27
package core

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/njtc406/emberengine/engine/pkg/actor/mailbox/job"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/dto"
)

// SysCtlHandler 系统控制命令处理函数。
//   - ctx: 与普通 Job 一致，可携带 traceId / 业务上下文；
//   - args: 与 dto.SysCmd.Args 一一对应，由调用方与处理方约定语义；
//   - 返回 error 仅用于日志记录与可观测性，不影响 Job 释放与 mailbox 状态。
type SysCtlHandler func(ctx context.Context, args []any) error

// SysCtlDispatcherKey 所有 SysCtl 投递使用的固定 dispatcher key，
// 保证全部命令落到同一 worker 串行执行（与 mailbox 内部状态变更顺序一致）。
const SysCtlDispatcherKey = "__sysctl__"

// 内置命令名称（外部可通过 PostSysCtl 调用）。
const (
	SysCtlCmdSuspend     = "mailbox.suspend"     // 挂起 mailbox
	SysCtlCmdResume      = "mailbox.resume"      // 恢复 mailbox
	SysCtlCmdHealthCheck = "service.healthcheck" // 输出健康检查日志
)

// sysCtlRegistry SysCtl 命令注册中心。
//
// 并发模型：
//   - Register* 通常发生在 Service.Init 同步阶段，但允许运行时追加（带 RLock 保护）；
//   - lookup 由 mailbox worker 串行执行（Mailbox 单 worker 即天然串行；多 worker 时
//     固定 dispatcher key 也保证落到同一 worker），仅需保护与 Register* 的并发可见性。
type sysCtlRegistry struct {
	mu       sync.RWMutex
	handlers map[string]SysCtlHandler
}

// newSysCtlRegistry 创建空的注册中心。
func newSysCtlRegistry() *sysCtlRegistry {
	return &sysCtlRegistry{handlers: make(map[string]SysCtlHandler)}
}

// register 注册或覆盖命令处理函数。返回旧的处理函数（若存在），便于上层做装饰链。
func (r *sysCtlRegistry) register(name string, h SysCtlHandler) SysCtlHandler {
	if name == "" || h == nil {
		return nil
	}
	r.mu.Lock()
	old := r.handlers[name]
	r.handlers[name] = h
	r.mu.Unlock()
	return old
}

// lookup 查询命令处理函数。
func (r *sysCtlRegistry) lookup(name string) (SysCtlHandler, bool) {
	r.mu.RLock()
	h, ok := r.handlers[name]
	r.mu.RUnlock()
	return h, ok
}

// names 返回当前已注册的命令名称（仅用于调试/健康检查输出）。
func (r *sysCtlRegistry) names() []string {
	r.mu.RLock()
	out := make([]string, 0, len(r.handlers))
	for k := range r.handlers {
		out = append(out, k)
	}
	r.mu.RUnlock()
	return out
}

// initSysCtlRegistry 初始化注册中心并注册内置命令。
// 在 Service.initJobHandlers 之后调用，依赖 s.mailbox / s.eventProcessor 已就绪。
func (s *Service) initSysCtlRegistry() {
	s.sysCtlRegistry = newSysCtlRegistry()
	s.registerBuiltinSysCtlCommands()
}

// registerBuiltinSysCtlCommands 注册框架内置的 mailbox 控制命令。
// 用户可通过 RegisterSysCtl 覆盖（同名后注册者生效）。
func (s *Service) registerBuiltinSysCtlCommands() {
	s.sysCtlRegistry.register(SysCtlCmdSuspend, func(ctx context.Context, _ []any) error {
		if s.mailbox == nil {
			return def.ErrServiceIsUnavailable
		}
		changed := s.mailbox.Suspend()
		s.WithContext(ctx).Infof("sysctl[%s] mailbox.Suspend changed=%v", SysCtlCmdSuspend, changed)
		return nil
	})
	s.sysCtlRegistry.register(SysCtlCmdResume, func(ctx context.Context, _ []any) error {
		if s.mailbox == nil {
			return def.ErrServiceIsUnavailable
		}
		changed := s.mailbox.Resume()
		s.WithContext(ctx).Infof("sysctl[%s] mailbox.Resume changed=%v", SysCtlCmdResume, changed)
		return nil
	})
	s.sysCtlRegistry.register(SysCtlCmdHealthCheck, func(ctx context.Context, _ []any) error {
		status := atomic.LoadInt32(&s.status)
		rwEnabled := false
		if s.mailbox != nil {
			rwEnabled = s.mailbox.IsRWEnabled()
		}
		s.WithContext(ctx).Infof("sysctl[%s] service=%s status=%d rw_enabled=%v handlers=%v",
			SysCtlCmdHealthCheck, s.GetName(), status, rwEnabled, s.sysCtlRegistry.names())
		return nil
	})
}

// RegisterSysCtl 对外暴露的注册接口；运行时（含 Init 之后）允许追加。
// 同名命令后注册者覆盖前者，便于用户对内置命令做装饰或替换。
func (s *Service) RegisterSysCtl(name string, handler SysCtlHandler) {
	if s.sysCtlRegistry == nil {
		s.Errorf("RegisterSysCtl called before Init: cmd=%s", name)
		return
	}
	s.sysCtlRegistry.register(name, handler)
}

// PostSysCtl 提交一条系统控制命令到本 Service 的 mailbox。
//
// 投递语义：
//   - Priority = PrioritySys（最高），可穿越 SuspendPolicy/RateLimit/Sentinel skip 高优先级保护；
//   - DispatcherKey 固定为 SysCtlDispatcherKey，保证多 worker 时所有 sysctl 命令串行；
//   - 失败路径（mailbox 关闭 / dispatch 错误）走 ADR-4 由 mailbox 内化 OnJobDiscarded + Release。
func (s *Service) PostSysCtl(ctx context.Context, cmd string, args ...any) error {
	if s.mailbox == nil {
		return def.ErrServiceIsUnavailable
	}
	if cmd == "" {
		return fmt.Errorf("PostSysCtl: empty cmd")
	}
	j := job.NewSysCtlJob()
	j.SetContext(ctx)
	j.SetPriority(def.PrioritySys)
	j.SetDispatcherKey(SysCtlDispatcherKey)
	j.SetPayload(dto.SysCmd{Cmd: cmd, Args: args})
	// 框架内部投递，绕过 PostJob 中的 ReadOnly 自投递检查（PrioritySys 命令本就是控制面）。
	// PostJob 拥有 Job 所有权：err 路径 mailbox 已 Release+OnJobDiscarded。
	return s.mailbox.PostJob(j)
}
