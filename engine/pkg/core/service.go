// Package core
// @Title  title
// @Description  desc
// @Author  pc  2024/11/5
// @Update  pc  2024/11/5
package core

import (
	"context"
	"fmt"
	"reflect"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/actor/mailbox"
	"github.com/njtc406/emberengine/engine/pkg/actor/mailbox/job"
	"github.com/njtc406/emberengine/engine/pkg/cluster"
	"github.com/njtc406/emberengine/engine/pkg/cluster/endpoints"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/core/rpc"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/profiler"
	"github.com/njtc406/emberengine/engine/pkg/router"
	"github.com/njtc406/emberengine/engine/pkg/utils/concurrent"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
	"github.com/njtc406/emberengine/engine/pkg/utils/xcontext"
)

// TODO 还需要给部分可自定义的组件增加一个设置的入口,不然需要覆写整个init太麻烦

var (
	_ inf.IMessageInvoker = (*Service)(nil)
	_ inf.IService        = (*Service)(nil)
)

type Service struct {
	Module
	inf.IMessageInvoker

	pid  *actor.PID // 服务元数据
	name string     // 服务名称

	src                    inf.IService // 服务实现
	cfg                    interface{}  // 服务配置
	status                 int32        // 服务状态(0初始化 1启动中 2启动  3关闭中 4关闭 5退休)
	isPrimarySecondaryMode bool         // 是否是主从模式

	mailbox *mailbox.Mailbox // 邮箱

	eventProcessor *event.Processor // 事件管理器

	profiler inf.IProfiler // 性能监控
	nodeCtx  inf.INodeContext

	cluster          *cluster.Cluster
	endpointManager  *endpoints.EndpointManager
	profilerRegistry *profiler.Registry
	router           *router.Router

	msgHooks []MsgHookFun // 消息钩子函数(在消息处理之前调用) TODO 这个实际上已经在mailbox中做了,这里暂时废弃

	mailboxMiddlewares []inf.IMailboxMiddleware // 邮箱中间件

	jobRegistry *jobHandlerRegistry // Job 处理器注册表

	txHookMgr TxHookManager // 事务钩子管理器（值类型，Init 注册，写路径 WLock 串行保护）

	stopGraceTimeout time.Duration // 关闭时等待窗口
	stopRequested    atomic.Bool   // 是否已请求停止（防止重复投递 FinalizeEvent）
	initErr          error
}

type profilerRegistryAdapter struct {
	registry *profiler.Registry
}

func (a *profilerRegistryAdapter) RegProfiler(name string, logger log.ILoggerX) inf.IProfiler {
	if a == nil || a.registry == nil {
		return nil
	}
	p := a.registry.RegProfiler(name, logger)
	if p == nil {
		return nil
	}
	return profiler.NewAdapter(p)
}

func (a *profilerRegistryAdapter) UnRegProfiler(name string) {
	if a == nil || a.registry == nil {
		return
	}
	a.registry.UnRegProfiler(name)
}

func (s *Service) SetRuntimeDeps(c *cluster.Cluster, em *endpoints.EndpointManager, pr *profiler.Registry, rt *router.Router) {
	s.cluster = c
	s.endpointManager = em
	s.profilerRegistry = pr
	s.router = rt
}

type loggerReleasable interface {
	Close() error
}

func releaseServiceLogger(logger log.ILoggerX) {
	if releasable, ok := logger.(loggerReleasable); ok {
		_ = releasable.Close()
	}
}

func (s *Service) rollbackInitResources() {
	if s.ITimerScheduler != nil {
		s.ITimerScheduler.Stop()
		s.ITimerScheduler = nil
	}
	if s.IConcurrent != nil {
		s.IConcurrent.Close()
		s.IConcurrent = nil
	}
	s.mailbox = nil
	s.eventProcessor = nil
	s.eventHandler = nil
	s.methodMgr = nil
	s.IRpcHandler = nil
	s.pid = nil
	if s.enableLogging && s.logger != nil {
		releaseServiceLogger(s.logger)
	}
	s.logger = nil
	s.ILoggerX = nil
}

func (s *Service) SetNodeContext(ctx inf.INodeContext) {
	s.nodeCtx = ctx
}

func (s *Service) GetNodeContext() inf.INodeContext {
	return s.nodeCtx
}

func (s *Service) GetEndpointManager() inf.INodeEndpointManager {
	if s.endpointManager != nil {
		return s.endpointManager
	}
	if s.nodeCtx != nil {
		return s.nodeCtx.GetEndpointManager()
	}
	return nil
}

func (s *Service) GetRouter() inf.INodeRouter {
	if s.router != nil {
		return s.router
	}
	if s.nodeCtx != nil {
		return s.nodeCtx.GetRouter()
	}
	return nil
}

func fixConf(serviceInitConf *config.ServiceInitConf) *config.ServiceInitConf {
	if serviceInitConf.Type == "" {
		serviceInitConf.Type = "Normal"
	}
	if serviceInitConf.StopGraceTimeout < 0 {
		serviceInitConf.StopGraceTimeout = 0
	}
	if serviceInitConf.StopPolicy != nil {
		if serviceInitConf.StopPolicy.GraceTimeout < 0 {
			serviceInitConf.StopPolicy.GraceTimeout = 0
		}
	}
	if serviceInitConf.RpcType == "" {
		// 优先推荐使用nats(如果业务需要明确知道对方是否有收到消息,推荐使用rpcx,如果被调用方是非go语言服务,且不支持nats,可以选择grpc)
		serviceInitConf.RpcType = def.RpcTypeNats
	}
	if serviceInitConf.LogConf == nil {
		serviceInitConf.LogConf = &config.ServiceLogConf{
			Enable: false,
			Config: nil,
		}
	}

	if serviceInitConf.TimerConf == nil {
		serviceInitConf.TimerConf = &config.TimerConf{
			TimerSize:       def.DefaultTimerSize,
			TimerBucketSize: def.DefaultTimerBucketSize,
		}
	} else {
		if serviceInitConf.TimerConf.TimerSize <= 0 {
			serviceInitConf.TimerConf.TimerSize = def.DefaultTimerSize
		}
		if serviceInitConf.TimerConf.TimerBucketSize <= 0 {
			serviceInitConf.TimerConf.TimerBucketSize = def.DefaultTimerBucketSize
		}
	}
	// 事件通道大小
	if serviceInitConf.EventChanSize <= 0 {
		serviceInitConf.EventChanSize = def.DefaultEventChanSize
	}

	return serviceInitConf
}

func (s *Service) Init(svc interface{}, serviceInitConf *config.ServiceInitConf, cfg interface{}) (err error) {
	s.initErr = nil
	defer func() {
		if err != nil {
			s.initErr = err
			s.rollbackInitResources()
		}
	}()
	var baseLogger log.ILoggerX
	var baseConcreteLogger *log.Logger
	if s.nodeCtx != nil {
		baseLogger = s.nodeCtx.GetLogger()
		if l, ok := baseLogger.(*log.Logger); ok {
			baseConcreteLogger = l
		}
	}
	if baseLogger == nil {
		return fmt.Errorf("service init requires node context logger")
	}

	if svc == nil {
		err = fmt.Errorf("service impl is nil")
		baseLogger.Errorf("service impl is nil, trace: %s", debug.Stack())
		return
	}
	if !atomic.CompareAndSwapInt32(&s.status, def.SvcStatusUnknown, def.SvcStatusInit) {
		return nil
	}

	if s.name == "" {
		s.name = reflect.Indirect(reflect.ValueOf(svc)).Type().Name()
	}

	// 整理配置参数
	if serviceInitConf == nil {
		err = fmt.Errorf("service init conf is nil")
		baseLogger.WithField("sName", s.GetName()).Error("service init conf is nil")
		return
	}
	serviceInitConf = fixConf(serviceInitConf)
	//baseLogger.Debugf("service[%s] init conf: %+v", s.GetName(), serviceInitConf)
	// 初始化服务数据
	s.src = svc.(inf.IService)
	s.cfg = cfg

	// 初始化日志
	if serviceInitConf.LogConf.Enable {
		// 配置了独立日志
		s.enableLogging = true
		// 更新日志文件的前缀名称为服务名称
		if serviceInitConf.LogConf.Config.PrefixName == "" {
			serviceInitConf.LogConf.Config.PrefixName = s.GetName()
		}
		l, loggerErr := log.NewDefaultLogger(serviceInitConf.LogConf.Config)
		if loggerErr != nil {
			err = fmt.Errorf("service[%s] create logger error: %w", s.GetName(), loggerErr)
			baseLogger.Errorf("service[%s] create logger error: %v", s.GetName(), loggerErr)
			return
		}
		s.logger = l
	} else {
		s.logger = baseConcreteLogger
		if s.logger == nil {
			l, loggerErr := log.NewDefaultLogger(nil)
			if loggerErr != nil {
				err = fmt.Errorf("service[%s] create fallback logger error: %w", s.GetName(), loggerErr)
				baseLogger.Errorf("service[%s] create fallback logger error: %v", s.GetName(), loggerErr)
				return
			}
			s.logger = l
		}
	}
	s.ILoggerX = s.logger.WithFields(log.Fields{
		"sName":     s.GetName(),
		"partition": serviceInitConf.Partition,
	})
	s.isPrimarySecondaryMode = serviceInitConf.IsPrimarySecondaryMode
	// StopPolicy 优先，其次兼容旧的 StopGraceTimeout
	stopGraceTimeout := serviceInitConf.StopGraceTimeout
	drainPolicy := mailbox.DrainExecute
	if serviceInitConf.StopPolicy != nil {
		stopGraceTimeout = serviceInitConf.StopPolicy.GraceTimeout
		drainPolicy = mailbox.ParseDrainPolicy(serviceInitConf.StopPolicy.DrainPolicy)
	}
	if stopGraceTimeout < 0 {
		stopGraceTimeout = 0
	}
	s.stopGraceTimeout = stopGraceTimeout

	// 创建定时器调度器
	tw := s.nodeCtx.GetTimingWheel()
	if tw == nil {
		err = fmt.Errorf("service[%s] timing wheel is nil", s.GetName())
		s.Errorf("service[%s] timing wheel is nil", s.GetName())
		return
	}
	var schedulerErr error
	s.ITimerScheduler, schedulerErr = timingwheel.NewJobScheduler(s.GetName(), serviceInitConf.TimerConf.TimerSize, serviceInitConf.TimerConf.TimerBucketSize,
		tw, s.ILoggerX)
	if schedulerErr != nil {
		err = fmt.Errorf("service[%s] create timer scheduler error: %w", s.GetName(), schedulerErr)
		s.Errorf("service[%s] create timer scheduler error: %v", s.GetName(), schedulerErr)
		return
	}

	// 根据配置创建中间件，并与用户自定义中间件合并
	isDebug := false
	if s.nodeCtx != nil {
		if cfg := s.nodeCtx.GetConfig(); cfg != nil {
			isDebug = cfg.IsDebug()
		}
	}
	configMiddlewares := mailbox.CreateMiddlewaresFromConfig(serviceInitConf.Mailbox, s.ILoggerX, isDebug)
	allMiddlewares := mailbox.MergeMiddlewares(configMiddlewares, s.mailboxMiddlewares)

	// 创建邮箱（将停机 drain 策略下发给 mailbox/workerPool）
	s.mailbox = mailbox.NewMailbox(serviceInitConf.Mailbox, s.ILoggerX, s, allMiddlewares, mailbox.WithDrainPolicy(drainPolicy))

	// 初始化根模块
	s.self = svc.(inf.IModule)
	s.root = s.self
	s.rootContains = make(map[uint32]inf.IModule)
	s.moduleIdSeed = def.DefaultModuleIdSeed
	s.moduleName = s.name

	// 创建事件处理器
	s.eventProcessor = event.NewTrigger()
	s.eventProcessor.Init(s)
	if s.nodeCtx != nil {
		if bus := s.nodeCtx.GetEventBus(); bus != nil {
			if b, ok := bus.(*event.Bus); ok {
				s.eventProcessor.SetEventBus(b)
			}
		}
	}
	// 注册事件管理器
	s.eventHandler = event.NewTriggerHandler()
	s.eventHandler.Init(s.eventProcessor)

	s.IConcurrent = concurrent.NewTaskScheduler(s.ILoggerX)

	// 注册 Job 处理函数
	s.initJobHandlers()

	em := s.GetEndpointManager()
	if em == nil {
		err = fmt.Errorf("service[%s] endpoint manager is nil", s.GetName())
		s.Errorf("service[%s] endpoint manager is nil", s.GetName())
		return
	}
	s.pid = em.CreatePid(serviceInitConf.Partition, serviceInitConf.ServiceId, serviceInitConf.Type, s.name, serviceInitConf.Version, serviceInitConf.RpcType)
	if s.pid == nil {
		err = fmt.Errorf("service[%s] create pid error", s.GetName())
		s.logger.Errorf("service[%s] create pid error", s.GetName())
		return
	}
	s.ILoggerX = s.ILoggerX.WithFields(log.Fields{
		"sUid":    s.pid.GetServiceUid(),
		"version": s.pid.GetVersion(),
	})

	// 初始化根节点rpc处理器
	var methodIdx inf.INodeMethodIndex
	if s.nodeCtx != nil {
		methodIdx = s.nodeCtx.GetMethodIndex()
	}
	s.methodMgr = rpc.NewMethodMgr(s.ILoggerX, methodIdx)
	// 将 WorkerPool 的 enableRW 引用传递给 MethodMgr，用于 RemoveMethods 防御性校验
	if rwMgr, ok := s.methodMgr.(*rpc.MethodMgr); ok {
		rwMgr.SetEnableRW(s.mailbox.GetEnableRWPtr())
	}
	s.IRpcHandler, err = rpc.NewHandler(s.self).Init(s.methodMgr)
	if err != nil {
		initErr := fmt.Errorf("service[%s] register rpc methods failed: %w", s.GetName(), err)
		s.Errorf("service[%s] register rpc methods failed: %v", s.GetName(), err)
		return initErr
	}

	if s.src != nil {
		if err := s.src.OnInit(); err != nil {
			initErr := fmt.Errorf("service[%s] onInit error: %w", s.GetName(), err)
			s.Errorf("service[%s] onInit error: %v", s.GetName(), err)
			return initErr
		}
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
			return err
		}
	}

	isCluster := s.cluster != nil && s.cluster.IsClusterMode()
	if !isCluster && s.nodeCtx != nil {
		isCluster = s.nodeCtx.IsClusterMode()
	}
	if !s.isPrimarySecondaryMode || s.IsPrivate() || !isCluster {
		// 没有开启主从模式或者私有服务或者没有开启集群,那么直接是主服务
		s.pid.SetMaster(true)
	}

	// 所有服务都注册到服务列表
	em := s.GetEndpointManager()
	if em == nil {
		return fmt.Errorf("service[%s] endpoint manager is nil", s.GetName())
	}
	em.AddService(s)
	//s.Infof("register service[%s] pid: %s", s.GetName(), s.pid.String())

	s.setStatus(def.SvcStatusRunning) // 到这里服务已经准备启动完成,可以正常处理请求了

	if s.src != nil {
		if err := s.src.OnStarted(); err != nil { // 这个阶段服务已经加入集群,需要集群操作的可以放这里完成
			return err
		}
	}

	return nil
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
	if em := s.GetEndpointManager(); em != nil {
		em.RemoveService(s)
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
	// 通过 RWSourceServiceKey 区分自投递和跨服务调用：
	// 仅当源 Service 与当前 Service 相同时才拦截，允许跨服务 RPC。
	if s.mailbox.IsRWEnabled() {
		if ctx := j.GetContext(); ctx != nil {
			if mode, ok := ctx.Value(def.RWModeContextKey).(def.RWMode); ok && mode == def.RWModeRead {
				if srcSvc, ok := ctx.Value(def.RWSourceServiceKey).(string); ok && srcSvc == s.GetServiceName() {
					s.Warnf("ReadOnly handler attempted to PostJob (self-posting detected). "+
						"This method should NOT be marked as ReadOnly. job_type=%v", j.GetType())
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
	// 1. ctx 是全新的（无 RWModeContextKey），不会触发 ReadOnly 自投递检测；
	// 2. 已显式设置 RWMode，无需经过 setJobRWMode 推断；
	// 3. 避免框架内部投递承担 PostJob 中面向用户的检查开销。
	if err := s.mailbox.PostJob(j); err != nil {
		s.Errorf("post job error: %v", err)
		j.Release()
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
	if err := s.mailbox.PostJob(j); err != nil {
		s.Errorf("post job error: %v", err)
		j.Release()
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
	return atomic.LoadInt32(&s.status) > def.SvcStatusRunning
}

func (s *Service) OpenProfiler() {
	var reg inf.INodeProfilerRegistry
	if s.profilerRegistry != nil {
		reg = &profilerRegistryAdapter{registry: s.profilerRegistry}
	} else if s.nodeCtx != nil {
		reg = s.nodeCtx.GetProfilerRegistry()
	}
	if reg == nil {
		s.Error("profiler registry is nil")
		return
	}
	s.profiler = reg.RegProfiler(s.pid.GetServiceUid(), s.ILoggerX)
	if s.profiler == nil {
		s.Error("profiler reg fail")
		return
	}
}

func (s *Service) GetProfiler() inf.IProfiler {
	return s.profiler
}

func (s *Service) closeProfiler() {
	if s.profiler != nil {
		var reg inf.INodeProfilerRegistry
		if s.profilerRegistry != nil {
			reg = &profilerRegistryAdapter{registry: s.profilerRegistry}
		} else if s.nodeCtx != nil {
			reg = s.nodeCtx.GetProfilerRegistry()
		}
		if reg != nil {
			reg.UnRegProfiler(s.pid.GetServiceUid())
		}
		s.profiler = nil
	}
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
	return atomic.LoadInt32(&s.status) == def.SvcStatusRunning
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
	return s.methodMgr.IsPrivate()
}

func (s *Service) GetLogger() log.ILoggerX {
	return s.ILoggerX
}

func (s *Service) IsPrimarySecondaryMode() bool {
	return s.isPrimarySecondaryMode
}
