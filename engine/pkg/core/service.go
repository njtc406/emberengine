// Package core
// @Title  Service 服务核心实现
// @Description  Service 的生命周期管理、Mailbox/Worker 池/中间件链初始化、Job 投递入口及 SysCtl 注册中心装配。
// @Author  pc  2024/11/5
// @Update  yr  2026/4/27
package core

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/actor/mailbox"
	"github.com/njtc406/emberengine/engine/pkg/actor/mailbox/job"
	"github.com/njtc406/emberengine/engine/pkg/authz"
	"github.com/njtc406/emberengine/engine/pkg/cluster"
	"github.com/njtc406/emberengine/engine/pkg/cluster/endpoints"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/profiler"
	"github.com/njtc406/emberengine/engine/pkg/router"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
	"github.com/njtc406/emberengine/engine/pkg/utils/xcontext"
)

// TODO 还需要给部分可自定义的组件增加一个设置的入口,不然需要覆写整个init太麻烦

var (
	_ inf.IMessageInvoker = (*Service)(nil)
	_ inf.IService        = (*Service)(nil)
)

// runtimeDeps 运行时依赖包（由 ServiceManager 在 Init 前注入）
type runtimeDeps struct {
	nodeCtx          inf.INodeContext
	cluster          *cluster.Cluster
	endpointManager  *endpoints.EndpointManager
	profilerRegistry *profiler.Registry
	router           *router.Router
}

type Service struct {
	Module
	inf.IMessageInvoker

	pid  *actor.PID // 服务元数据
	name string     // 服务名称

	src                    inf.IService // 服务实现
	cfg                    any          // 服务配置
	status                 int32        // 服务状态(0初始化 1启动中 2启动  3关闭中 4关闭 5退休)
	isPrimarySecondaryMode bool         // 是否是主从模式
	visibility             def.ServiceVisibility

	mailbox *mailbox.Mailbox // 邮箱

	eventProcessor *event.Processor // 事件管理器

	profiler inf.IProfiler // 性能监控

	deps runtimeDeps // 运行时依赖（由 ServiceManager 在 Init 前注入）

	mailboxMiddlewares []inf.IMailboxMiddleware // 邮箱中间件

	jobRegistry *jobHandlerRegistry // Job 处理器注册表

	sysCtlRegistry *sysCtlRegistry // SysCtl 命令注册中心

	txHookMgr TxHookManager // 事务钩子管理器（值类型，Init 注册，写路径 WLock 串行保护）

	stopGraceTimeout time.Duration // 关闭时等待窗口
	stopRequested    atomic.Bool   // 是否已请求停止（防止重复投递 FinalizeEvent）
	initErr          error
	authorizer       *authz.Authorizer // 可选：RBAC 授权引擎
}

func (s *Service) SetRuntimeDeps(c *cluster.Cluster, em *endpoints.EndpointManager, pr *profiler.Registry, rt *router.Router) {
	s.deps.cluster = c
	s.deps.endpointManager = em
	s.deps.profilerRegistry = pr
	s.deps.router = rt
}

// SetAuthorizer 注入 RBAC 授权引擎。
func (s *Service) SetAuthorizer(a *authz.Authorizer) {
	s.authorizer = a
}

func (s *Service) SetNodeContext(ctx inf.INodeContext) {
	s.deps.nodeCtx = ctx
}

func (s *Service) GetNodeContext() inf.INodeContext {
	return s.deps.nodeCtx
}

func (s *Service) GetEndpointManager() inf.INodeEndpointManager {
	if s.deps.endpointManager != nil {
		return s.deps.endpointManager
	}
	if s.deps.nodeCtx != nil {
		return s.deps.nodeCtx.GetEndpointManager()
	}
	return nil
}

func (s *Service) GetRouter() inf.INodeRouter {
	if s.deps.router != nil {
		return s.deps.router
	}
	if s.deps.nodeCtx != nil {
		return s.deps.nodeCtx.GetRouter()
	}
	return nil
}

func (s *Service) Start() error {
	if s.initErr != nil {
		return fmt.Errorf("service[%s] init failed: %w", s.GetName(), s.initErr)
	}
	if !atomic.CompareAndSwapInt32(&s.status, def.SvcStatusInit, def.SvcStatusStarting) {
		return fmt.Errorf("service[%s] status[%d] has inited", s.GetName(), s.status)
	}

	// 启动邮箱
	s.mailbox.Start()

	// 启动监听回调
	go s.startListenCallback()

	// 主从服务需要在onstart中处理
	if s.src != nil {
		if err := s.src.OnStart(); err != nil {
			s.rollbackStart(nil, false)
			return err
		}
	}

	isCluster := s.deps.cluster != nil && s.deps.cluster.IsClusterMode()
	if !isCluster && s.deps.nodeCtx != nil {
		isCluster = s.deps.nodeCtx.IsClusterMode()
	}
	if !s.isPrimarySecondaryMode || !isCluster {
		// 没有开启主从模式或者没有开启集群/主从通道,那么直接是主服务。
		// visibility 只控制服务发现发布，不参与主从选举语义。
		s.pid.SetMaster(true)
	}

	s.setStatus(def.SvcStatusRunning)

	// 所有服务都注册到服务列表
	em := s.GetEndpointManager()
	if em == nil {
		s.rollbackStart(nil, false)
		return fmt.Errorf("service[%s] endpoint manager is nil", s.GetName())
	}
	em.AddService(s)
	//s.Infof("register service[%s] pid: %s", s.GetName(), s.pid.String())

	if s.src != nil {
		if err := s.src.OnStarted(); err != nil { // 这个阶段服务已经加入集群,需要集群操作的可以放这里完成
			s.rollbackStart(em, true)
			return err
		}
	}

	s.setStatus(def.SvcStatusReady)
	em.ServiceReady(s)

	return nil
}

func (s *Service) rollbackStart(em inf.INodeEndpointManager, registered bool) {
	if registered && em != nil {
		em.RemoveService(s)
	}
	if s.mailbox != nil {
		s.mailbox.Stop()
	}
	if s.ITimerScheduler != nil {
		s.ITimerScheduler.Stop()
	}
	if s.IConcurrent != nil {
		s.IConcurrent.Close()
	}
	s.releaseWithEndpoint(false)
	if s.enableLogging && s.logger != nil {
		releaseServiceLogger(s.logger)
	}
	atomic.StoreInt32(&s.status, def.SvcStatusClosed)
}

func (s *Service) startListenCallback() {
	for {
		select {
		case t, ok := <-s.IConcurrent.GetChannel():
			if !ok {
				return
			}
			if err := s.pushConcurrentCallback(xcontext.New(nil), t); err != nil {
				s.Errorf("submit concurrent callback error: %v", err)
			}
		case t, ok := <-s.ITimerScheduler.GetTimerCbChannel():
			if !ok {
				return
			}
			if err := s.pushTimerCallback(xcontext.New(nil), t); err != nil {
				s.Errorf("submit timer callback error: %v", err)
			}
		}
	}
}

// Stop 同步停止服务：请求停止并等待完成。
// 注意：不要在 mailbox worker 内调用此方法，会死锁！
func (s *Service) Stop() {
	// 防止重复投递
	if !s.stopRequested.CompareAndSwap(false, true) {
		return
	}

	// 标记进入关闭中状态（CAS 保护，仅从运行态转换）
	for {
		old := atomic.LoadInt32(&s.status)
		if old >= def.SvcStatusClosing {
			// 已经在关闭流程中
			return
		}
		if atomic.CompareAndSwapInt32(&s.status, old, def.SvcStatusClosing) {
			break
		}
	}

	// 挂起邮箱（只允许 Finalize 等必要消息进入）
	if s.mailbox != nil {
		s.mailbox.Suspend()
	}

	// 关闭定时器
	if s.ITimerScheduler != nil {
		s.ITimerScheduler.Stop()
	}

	// 关闭并发
	if s.IConcurrent != nil {
		s.IConcurrent.Close()
	}

	// 释放资源
	s.release()

	if s.enableLogging && s.logger != nil {
		releaseServiceLogger(s.logger)
	}

	atomic.StoreInt32(&s.status, def.SvcStatusClosed)
}

func (s *Service) release() {
	s.releaseWithEndpoint(true)
}

func (s *Service) releaseWithEndpoint(removeEndpoint bool) {
	defer func() {
		if err := recover(); err != nil {
			s.Errorf("release error: %v", err)
		}
	}()

	if s.self != nil {
		s.self.OnRelease()
	}
	s.closeProfiler()

	// 服务关闭,从服务移除(等待其他释放完再移除,防止在释放的时候有同步调用,例如db等,会导致调用失败)
	if removeEndpoint {
		if em := s.GetEndpointManager(); em != nil {
			em.RemoveService(s)
		}
	}
}

func (s *Service) PostJob(j inf.IMailboxJob) error {
	// 【RW 安全约束】检测 ReadOnly handler 中的自投递（仅同 Service）
	// ReadOnly handler 运行在读 goroutine 中（持有 RLock），如果它尝试投递
	// 新的 Write Job 到同一 Service，该 Write Job 最终需要 WLock 执行，
	// 而当前读 goroutine 正持有 RLock —— 虽然不会形成死锁（写 Job
	// 进入队列等待后续处理），但这暗示 ReadOnly handler
	// 存在副作用（触发写操作），应被标记为 Write 而非 Read。
	//
	// 通过 RWContextInfo.SourceService 区分自投递和跨服务调用：
	// 仅当源 Service 与当前 Service 相同时才拦截，允许跨服务 RPC。
	//
	// PostJob 拥有 Job 所有权：早期拒绝路径同样负责 Release + OnJobDiscarded，
	// 调用方在 err 返回后不再 Release。
	if s.mailbox.IsRWEnabled() {
		if ctx := j.GetContext(); ctx != nil {
			if rwInfo, ok := ctx.Value(def.RWContextKey).(def.RWContextInfo); ok && rwInfo.Mode == def.RWModeRead {
				if rwInfo.SourceService == s.GetServiceName() {
					s.Warnf("ReadOnly handler attempted to PostJob (self-posting detected). "+
						"This method should NOT be marked as ReadOnly. job_type=%v", j.GetType())
					s.OnJobDiscarded(j, def.ErrReadOnlyPostJob)
					j.Release()
					return def.ErrReadOnlyPostJob
				}
			}
		}
	}

	// RW 模式下，为 RPC 请求 Job 设置 RWMode
	if s.mailbox.IsRWEnabled() {
		s.setJobRWMode(j)
	}

	return s.mailbox.PostJob(j)
}

// setJobRWMode 根据 methodMgr 的只读标记为 RPC 请求 Job 注入 RWMode。
// 只有 RPC 请求（非回复）且方法被标记为 ReadOnly 时，才设置为 RWModeRead。
// 其他所有 Job（Timer、Event、Concurrent 等）一律保持零值 RWModeWrite。
func (s *Service) setJobRWMode(j inf.IMailboxJob) {
	// 只有 RPC 请求才可能是 Read，其他所有 Job 类型一律为 Write
	if j.GetType() != def.MailboxJobTypeRpc {
		return
	}

	rwJob, ok := j.(inf.IRWModeJob)
	if !ok {
		return
	}

	// 从 RPC Job 中提取 envelope，获取方法名
	envelope := job.GetJobPayloadAs[inf.IEnvelope](j)
	if envelope == nil {
		return
	}

	// 只有请求（非回复）才检查 ReadOnly
	data := envelope.GetData()
	if data == nil || data.IsReply() {
		return // 响应/异步回调 → 始终为 Write
	}

	// 查询方法是否为只读（通过 IReadOnlyMethodMgr 类型断言）
	if roMgr, ok := s.methodMgr.(inf.IReadOnlyMethodMgr); ok {
		if roMgr.IsReadOnly(data.GetMethod()) {
			rwJob.SetRWMode(def.RWModeRead)
		}
	}
	// 未匹配时 Job 零值为 RWModeWrite
}

func (s *Service) pushConcurrentCallback(ctx context.Context, evt inf.IConcurrentCallback) error {
	j := job.NewConcurrentCallbackJob()
	j.SetContext(ctx)
	j.SetPriority(def.PriorityNormal)
	j.SetDispatcherKey(evt.GetName())
	j.SetPayload(evt)
	// 显式标记为 Write：并发回调通常伴随状态更新（如写缓存、修改字段），必须独占执行。
	j.SetRWMode(def.RWModeWrite)
	// 框架内部投递，直接调用 mailbox.PostJob 而非 s.PostJob：
	// 1. ctx 是全新的（无 RWContextKey），不会触发 ReadOnly 自投递检测；
	// 2. 已显式设置 RWMode，无需经过 setJobRWMode 推断；
	// 3. 避免框架内部投递承担 PostJob 中面向用户的检查开销。
	//
	// PostJob 拥有 Job 所有权：err 返回时 mailbox 内部已 Release+OnJobDiscarded，
	// 这里不再外部 Release，避免 ref-count 下溢。
	if err := s.mailbox.PostJob(j); err != nil {
		s.Errorf("post job error: %v", err)
		return err
	}
	return nil
}

func (s *Service) pushTimerCallback(ctx context.Context, t timingwheel.ITimer) error {
	j := job.NewTimerJob()
	j.SetContext(ctx)
	j.SetPriority(def.PriorityNormal)
	j.SetDispatcherKey(t.GetName())
	j.SetPayload(t)
	// 显式标记为 Write：定时器回调通常伴随状态更新，必须独占执行。
	j.SetRWMode(def.RWModeWrite)
	// 框架内部投递，直接调用 mailbox.PostJob（理由同 pushConcurrentCallback）
	// PostJob 拥有 Job 所有权，err 路径已内化 Release+OnJobDiscarded。
	if err := s.mailbox.PostJob(j); err != nil {
		s.Errorf("post job error: %v", err)
		return err
	}
	return nil
}

func (s *Service) SetName(name string) {
	s.name = name
}

func (s *Service) GetName() string {
	return s.name
}

func (s *Service) SetPid(pid *actor.PID) {
	s.pid = pid
}

func (s *Service) GetPartition() int32 {
	return s.pid.GetPartition()
}

func (s *Service) GetPid() *actor.PID {
	return s.pid
}

func (s *Service) GetMailbox() inf.IMailbox {
	return s.mailbox
}

func (s *Service) OnInit() error {
	return nil
}

func (s *Service) OnStart() error {
	return nil
}

func (s *Service) OnStarted() error {
	return nil
}

func (s *Service) OnRelease() {}

func (s *Service) IsClosed() bool {
	return atomic.LoadInt32(&s.status) >= def.SvcStatusClosing
}

func (s *Service) GetStatus() int32 {
	return atomic.LoadInt32(&s.status)
}

func (s *Service) GetServiceCfg() interface{} {
	return s.cfg
}

func (s *Service) safeExec(f func() error) (err error) {
	defer func() {
		if err := recover(); err != nil {
			s.Errorf("safe exec error: %v\ntrace:%s", err, debug.Stack())
			err = fmt.Errorf("safe exec error: %v", err)
		}
	}()
	err = f()
	return err
}

func (s *Service) setStatus(status int32) {
	oldStatus := atomic.LoadInt32(&s.status)
	if oldStatus == status || oldStatus >= def.SvcStatusClosed {
		// 退休和关闭状态不允许修改
		return
	}
	atomic.StoreInt32(&s.status, status)
}

func (s *Service) isRunning() bool {
	status := atomic.LoadInt32(&s.status)
	return status == def.SvcStatusRunning || status == def.SvcStatusReady
}

func (s *Service) GetServiceName() string {
	return s.name
}

func (s *Service) GetRpcHandler() inf.IRpcHandler {
	return s.IRpcHandler
}

func (s *Service) EscalateFailure(ctx context.Context, reason interface{}, j inf.IMailboxJob) {
	s.WithContext(ctx).Errorf("job[%d] EscalateFailure: %v", j.GetType(), reason)
}

func (s *Service) OnJobDiscarded(job inf.IMailboxJob, reason error) {
	s.Warnf("job[%d] discarded: %v", job.GetType(), reason)
}

func (s *Service) IsPrivate() bool {
	return s.GetVisibility() != def.ServiceVisibilityCluster
}

func (s *Service) IsRemoteCallable() bool {
	visibility := s.GetVisibility()
	return visibility == def.ServiceVisibilityCluster || visibility == def.ServiceVisibilityNode
}

func (s *Service) GetVisibility() def.ServiceVisibility {
	if s.visibility == 0 {
		return def.ServiceVisibilityNode
	}
	return s.visibility
}

func (s *Service) GetLogger() log.ILoggerX {
	return s.ILoggerX
}

func (s *Service) IsPrimarySecondaryMode() bool {
	return s.isPrimarySecondaryMode
}
