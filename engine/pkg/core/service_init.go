// Package core
// @Title  Service Init 子方法
// @Description  将 Service.Init 中的各组件初始化逻辑拆分为独立的子方法，
//
//	每个子方法负责一个职责域，Init 方法仅做编排调度。
//
// @Author  yr  2026/5/17
package core

import (
	"fmt"
	"reflect"
	"runtime/debug"
	"sync/atomic"

	"github.com/njtc406/emberengine/engine/pkg/actor/mailbox"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/core/rpc"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/concurrent"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
)

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
	if serviceInitConf.EventChanSize <= 0 {
		serviceInitConf.EventChanSize = def.DefaultEventChanSize
	}
	return serviceInitConf
}

// Init 编排方法：依次调用子初始化方法完成 Service 运行时装配。
func (s *Service) Init(svc interface{}, serviceInitConf *config.ServiceInitConf, cfg interface{}) (err error) {
	s.initErr = nil
	defer func() {
		if err != nil {
			s.initErr = err
			s.rollbackInitResources()
		}
	}()

	// 前置校验
	var baseLogger log.ILoggerX
	if s.deps.nodeCtx != nil {
		baseLogger = s.deps.nodeCtx.GetLogger()
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
	if serviceInitConf == nil {
		err = fmt.Errorf("service init conf is nil")
		baseLogger.WithField("sName", s.GetName()).Error("service init conf is nil")
		return
	}
	serviceInitConf = fixConf(serviceInitConf)
	s.src = svc.(inf.IService)
	s.cfg = cfg

	// 1. 日志
	if err = s.initLogger(serviceInitConf); err != nil {
		return
	}
	// 2. 停机策略
	s.isPrimarySecondaryMode = serviceInitConf.IsPrimarySecondaryMode
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
	// 3. 定时器
	if err = s.initTimers(serviceInitConf); err != nil {
		return
	}
	// 4. 邮箱
	if err = s.initMailbox(serviceInitConf, drainPolicy); err != nil {
		return
	}
	// 5. 模块层级
	s.initModule(svc)
	// 6. 事件
	s.initEvents()
	// 7. 并发
	s.initConcurrent()
	// 8. Job/SysCtl 注册
	s.initJobHandlers()
	s.initSysCtlRegistry()
	// 9. PID
	if err = s.initPID(serviceInitConf); err != nil {
		return
	}
	// 10. RPC
	if err = s.initRPC(); err != nil {
		return
	}

	// 用户回调
	if s.src != nil {
		if err = s.src.OnInit(); err != nil {
			err = fmt.Errorf("service[%s] onInit error: %w", s.GetName(), err)
			return
		}
	}

	return nil
}

// initLogger 初始化日志系统。
func (s *Service) initLogger(conf *config.ServiceInitConf) error {
	var baseConcreteLogger *log.Logger
	if s.deps.nodeCtx != nil {
		if baseLogger := s.deps.nodeCtx.GetLogger(); baseLogger != nil {
			if l, ok := baseLogger.(*log.Logger); ok {
				baseConcreteLogger = l
			}
		}
	}

	if conf.LogConf.Enable {
		// 配置了独立日志
		s.enableLogging = true
		if conf.LogConf.Config.PrefixName == "" {
			conf.LogConf.Config.PrefixName = s.GetName()
		}
		l, loggerErr := log.NewDefaultLogger(conf.LogConf.Config)
		if loggerErr != nil {
			return fmt.Errorf("service[%s] create logger error: %w", s.GetName(), loggerErr)
		}
		s.logger = l
	} else {
		s.logger = baseConcreteLogger
		if s.logger == nil {
			l, loggerErr := log.NewDefaultLogger(nil)
			if loggerErr != nil {
				return fmt.Errorf("service[%s] create fallback logger error: %w", s.GetName(), loggerErr)
			}
			s.logger = l
		}
	}
	s.ILoggerX = s.logger.WithFields(log.Fields{
		"sName":     s.GetName(),
		"partition": conf.Partition,
	})
	return nil
}

// initTimers 创建定时器调度器。
// 前置条件：s.deps.nodeCtx 已注入、s.ILoggerX 已初始化。
func (s *Service) initTimers(conf *config.ServiceInitConf) error {
	if s.deps.nodeCtx == nil {
		return fmt.Errorf("service[%s] node context is nil", s.GetName())
	}
	tw := s.deps.nodeCtx.GetTimingWheel()
	if tw == nil {
		return fmt.Errorf("service[%s] timing wheel is nil", s.GetName())
	}
	scheduler, err := timingwheel.NewJobScheduler(
		s.GetName(),
		conf.TimerConf.TimerSize,
		conf.TimerConf.TimerBucketSize,
		tw, s.ILoggerX,
	)
	if err != nil {
		return fmt.Errorf("service[%s] create timer scheduler error: %w", s.GetName(), err)
	}
	s.ITimerScheduler = scheduler
	return nil
}

// initMailbox 创建邮箱及中间件链。
// 前置条件：s.ILoggerX 已初始化、s.deps.nodeCtx 已注入。
func (s *Service) initMailbox(conf *config.ServiceInitConf, drainPolicy mailbox.DrainPolicy) error {
	isDebug := false
	if s.deps.nodeCtx != nil {
		if cfg := s.deps.nodeCtx.GetConfig(); cfg != nil {
			isDebug = cfg.IsDebug()
		}
	}
	configMiddlewares := mailbox.CreateMiddlewaresFromConfig(conf.Mailbox, s.GetName(), s.ILoggerX, isDebug)
	allMiddlewares := mailbox.MergeMiddlewares(configMiddlewares, s.mailboxMiddlewares)

	var err error
	s.mailbox, err = mailbox.NewMailbox(conf.Mailbox, s.ILoggerX, s, allMiddlewares, mailbox.WithDrainPolicy(drainPolicy))
	return err
}

// initModule 初始化根模块层级。
// 前置条件：svc 实现了 inf.IModule。
func (s *Service) initModule(svc interface{}) {
	s.self = svc.(inf.IModule)
	s.root = s.self
	s.rootContains = make(map[uint32]inf.IModule)
	s.moduleIdSeed = def.DefaultModuleIdSeed
	s.moduleName = s.name
}

// initEvents 创建事件处理器和事件管理器。
// 前置条件：s.deps.nodeCtx 已注入。
func (s *Service) initEvents() {
	s.eventProcessor = event.NewTrigger()
	s.eventProcessor.Init(s)
	if s.deps.nodeCtx != nil {
		if bus := s.deps.nodeCtx.GetEventBus(); bus != nil {
			if b, ok := bus.(*event.Bus); ok {
				s.eventProcessor.SetEventBus(b)
			}
		}
	}
	s.eventHandler = event.NewTriggerHandler()
	s.eventHandler.Init(s.eventProcessor)
}

// initConcurrent 创建并发任务调度器。
// 前置条件：s.ILoggerX 已初始化。
func (s *Service) initConcurrent() {
	s.IConcurrent = concurrent.NewTaskScheduler(s.ILoggerX)
}

// initPID 创建服务 PID 并追加日志字段。
// 前置条件：EndpointManager 可用。
func (s *Service) initPID(conf *config.ServiceInitConf) error {
	em := s.GetEndpointManager()
	if em == nil {
		return fmt.Errorf("service[%s] endpoint manager is nil", s.GetName())
	}
	s.pid = em.CreatePid(conf.Partition, conf.ServiceId, conf.Type, s.name, conf.Version, conf.RpcType)
	if s.pid == nil {
		return fmt.Errorf("service[%s] create pid error", s.GetName())
	}
	s.ILoggerX = s.ILoggerX.WithFields(log.Fields{
		"sUid":    s.pid.GetServiceUid(),
		"version": s.pid.GetVersion(),
	})
	return nil
}

// initRPC 初始化 RPC 方法管理器和处理器，并注入授权引擎。
// 前置条件：s.self 已设置、s.mailbox 已创建、s.ILoggerX 已初始化。
func (s *Service) initRPC() error {
	var methodIdx inf.INodeMethodIndex
	if s.deps.nodeCtx != nil {
		methodIdx = s.deps.nodeCtx.GetMethodIndex()
	}
	s.methodMgr = rpc.NewMethodMgr(s.ILoggerX, methodIdx)
	// 通过 func() bool 闭包注入 RW 状态查询，避免暴露内部 *atomic.Bool
	if rwMgr, ok := s.methodMgr.(*rpc.MethodMgr); ok {
		rwMgr.SetRWStateProvider(s.mailbox.IsRWEnabled)
	}
	var err error
	s.IRpcHandler, err = rpc.NewHandler(s.self).Init(s.methodMgr)
	if err != nil {
		return fmt.Errorf("service[%s] register rpc methods failed: %w", s.GetName(), err)
	}
	// 注入 RBAC 授权引擎
	if s.authorizer != nil {
		if h, ok := s.IRpcHandler.(*rpc.Handler); ok {
			h.SetAuthorizer(s.authorizer)
		}
	}
	return nil
}
