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
	"github.com/njtc406/emberengine/engine/pkg/cluster"
	"github.com/njtc406/emberengine/engine/pkg/cluster/endpoints"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/core/rpc"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/profiler"
	"github.com/njtc406/emberengine/engine/pkg/utils/concurrent"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
	"github.com/njtc406/emberengine/engine/pkg/utils/xcontext"
)

// TODO 还需要给部分可自定义的组件增加一个设置的入口,不然需要覆写整个init太麻烦

// TODO 之后将所有的serverId换个名字,叫做namespace,或者group,用来划分服务组

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

	mailbox              *mailbox.Mailbox // 邮箱
	eventProcessor       *event.Processor // 事件管理器
	globalEventProcessor *event.Processor // 全局事件管理器

	profiler *profiler.Profiler // 性能监控

	eventHandlers map[int32]EventHandler

	msgHooks []MsgHookFun // 消息钩子函数(在消息处理之前调用)

	mailboxMiddlewares []inf.IMailboxMiddleware // 邮箱中间件

	stopGraceTimeout time.Duration // 关闭时等待窗口
}

func fixConf(serviceInitConf *config.ServiceInitConf) *config.ServiceInitConf {
	if serviceInitConf.Type == "" {
		serviceInitConf.Type = "Normal"
	}
	if serviceInitConf.StopGraceTimeout < 0 {
		serviceInitConf.StopGraceTimeout = 0
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

	return serviceInitConf
}

func (s *Service) Init(svc interface{}, serviceInitConf *config.ServiceInitConf, cfg interface{}) {
	if svc == nil {
		log.SysLogger.Fatalf("service impl is nil, trace: %s", debug.Stack())
		return
	}
	if !atomic.CompareAndSwapInt32(&s.status, def.SvcStatusUnknown, def.SvcStatusInit) {
		return
	}

	if s.name == "" {
		s.name = reflect.Indirect(reflect.ValueOf(svc)).Type().Name()
	}

	// 整理配置参数
	if serviceInitConf == nil {
		log.SysLogger.WithField("sName", s.GetName()).Fatal("service init conf is nil")
		return
	}
	serviceInitConf = fixConf(serviceInitConf)
	//log.SysLogger.Debugf("service[%s] init conf: %+v", s.GetName(), serviceInitConf)
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
		l, err := log.NewDefaultLogger(serviceInitConf.LogConf.Config)
		if err != nil {
			log.SysLogger.Panicf("service[%s] create logger error: %s", s.GetName(), err)
		}
		s.logger = l
	} else {
		s.logger = log.SysLogger
	}
	s.ILoggerX = log.NewLoggerX(s.logger, log.Fields{
		"sName": s.GetName(),
		"sId":   serviceInitConf.ServerId,
	})
	s.isPrimarySecondaryMode = serviceInitConf.IsPrimarySecondaryMode
	s.stopGraceTimeout = serviceInitConf.StopGraceTimeout

	// 创建定时器调度器
	s.ITimerScheduler = timingwheel.NewJobScheduler(s.GetName(), serviceInitConf.TimerConf.TimerSize, serviceInitConf.TimerConf.TimerBucketSize,
		timingwheel.GetTimingWheel(), s.ILoggerX, config.IsDebug())
	// Debug-only mailbox diagnostics
	if config.IsDebug() {
		s.mailboxMiddlewares = append(s.mailboxMiddlewares, mailbox.NewDispatchKeyStatsMiddleware(s.ILoggerX, 10*time.Second, 10))
	}
	// 创建邮箱
	s.mailbox = mailbox.NewMailbox(serviceInitConf.Mailbox, s.ILoggerX, s, s.mailboxMiddlewares...)

	// 初始化根模块
	s.self = svc.(inf.IModule)
	s.root = s.self
	s.rootContains = make(map[uint32]inf.IModule)
	s.moduleIdSeed = def.DefaultModuleIdSeed
	s.moduleName = s.name

	// 创建事件处理器
	s.eventProcessor = event.NewProcessor()
	s.eventProcessor.Init(s)
	// 注册事件管理器
	s.eventHandler = event.NewHandler()
	s.eventHandler.Init(s.eventProcessor)

	s.globalEventProcessor = event.NewProcessor()
	s.globalEventProcessor.Init(s)

	s.IConcurrent = concurrent.NewTaskScheduler(s.ILoggerX)

	// 注册事件处理函数
	s.initEventHandlers()

	s.pid = endpoints.GetEndpointManager().CreatePid(serviceInitConf.ServerId, serviceInitConf.ServiceId, serviceInitConf.Type, s.name, serviceInitConf.Version, serviceInitConf.RpcType)
	if s.pid == nil {
		s.logger.Panicf("service[%s] create pid error", s.GetName())
		return
	}
	s.ILoggerX = s.ILoggerX.WithFields(log.Fields{
		"sUid":    s.pid.GetServiceUid(),
		"version": s.pid.GetVersion(),
	})

	// 初始化根节点rpc处理器
	s.methodMgr = rpc.NewMethodMgr(s.ILoggerX)
	s.IRpcHandler = rpc.NewHandler(s.self).Init(s.methodMgr)

	if s.src != nil {
		if err := s.src.OnInit(); err != nil {
			s.Panicf("service[%s] onInit error: %s", s.GetName(), err)
		}
	}
}

func (s *Service) Start() error {
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

	if !s.isPrimarySecondaryMode || s.IsPrivate() || !cluster.GetCluster().IsClusterMode() {
		// 没有开启主从模式或者私有服务或者没有开启集群,那么直接是主服务
		s.pid.SetMaster(true)
	}

	// 所有服务都注册到服务列表
	endpoints.GetEndpointManager().AddService(s)
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

func (s *Service) Stop() {
	if s.IsClosed() {
		// 防止多次关闭
		return
	}
	//s.Debugf("service[%s] begin stop", s.GetName())
	atomic.StoreInt32(&s.status, def.SvcStatusClosing)

	// 挂起邮箱(防止有新的请求消息进来)
	s.mailbox.Suspend()

	// 关闭定时器
	s.ITimerScheduler.Stop()

	// 关闭并发
	s.IConcurrent.Close()

	// 释放资源(这里面可能还会有call类型的调用,所以先执行)
	s.release()

	// 等待窗口：允许短延迟回调/回复在关闭前最后进入并处理。
	// 超过窗口不再等待，直接停止 mailbox。
	if s.stopGraceTimeout > 0 {
		time.Sleep(s.stopGraceTimeout)
	}

	// 关闭邮箱(完全关闭所有的工作线程,不再接收任何消息)
	s.mailbox.Stop()

	if s.enableLogging && s.logger != nil {
		// 如果开启了独立日志,则关闭日志
		log.Release(s.logger)
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
	endpoints.GetEndpointManager().RemoveService(s)

}

func (s *Service) PushEvent(ctx context.Context, evt inf.IEvent) error {
	//if !s.isRunning() {
	//	return def.ErrServiceIsUnavailable
	//}
	// 所有权转移：调用方在 PushEvent 后不得再使用 evt。
	// mailbox/service 会在处理完毕后 Release；若入队失败，这里负责释放避免泄露。
	err := s.mailbox.PostMessage(ctx, evt)
	if err != nil {
		evt.Release()
	}
	return err
}

func (s *Service) pushConcurrentCallback(ctx context.Context, evt concurrent.IConcurrentCallback) error {
	ev := event.NewEvent()
	ev.Type = event.ServiceConcurrentCallback
	ev.Data = evt
	return s.mailbox.PostMessage(ctx, ev)
}

func (s *Service) pushTimerCallback(ctx context.Context, t timingwheel.ITimer) error {
	ev := event.NewEvent()
	ev.Type = event.ServiceTimerCallback
	ev.DispatcherKey = t.GetName() // 保证相同的回调在同一个worker处理
	ev.Data = t
	return s.mailbox.PostMessage(ctx, ev)
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

func (s *Service) GetServerId() int32 {
	return s.pid.GetServerId()
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
	s.profiler = profiler.RegProfiler(s.pid.GetServiceUid(), s.ILoggerX)
	if s.profiler == nil {
		s.Fatal("profiler reg fail")
	}
}

func (s *Service) GetProfiler() *profiler.Profiler {
	return nil //s.profiler
}

func (s *Service) closeProfiler() {
	if s.profiler != nil {
		profiler.UnRegProfiler(s.pid.GetServiceUid())
		s.profiler = nil
	}
}

func (s *Service) GetServiceCfg() interface{} {
	return s.cfg
}

func (s *Service) safeExec(f func()) {
	defer func() {
		if err := recover(); err != nil {
			s.Errorf("safe exec error: %v\ntrace:%s", err, debug.Stack())
		}
	}()
	f()
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

func (s *Service) EscalateFailure(ctx context.Context, reason interface{}, evt inf.IEvent) {
	s.WithContext(ctx).Errorf("event[%d] EscalateFailure: %v", evt.GetType(), reason)
}

func (s *Service) IsPrivate() bool {
	return s.methodMgr.IsPrivate()
}

func (s *Service) GetLogger() *log.Logger {
	return s.logger
}
func (s *Service) GetLoggerX() log.ILoggerX {
	return s.ILoggerX
}

func (s *Service) IsPrimarySecondaryMode() bool {
	return s.isPrimarySecondaryMode
}
