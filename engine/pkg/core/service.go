// Package core
// @Title  title
// @Description  desc
// @Author  pc  2024/11/5
// @Update  pc  2024/11/5
package core

import (
	"fmt"
	"github.com/njtc406/emberengine/engine/pkg/actor/mailbox"
	"path"
	"reflect"
	"runtime/debug"
	"sync/atomic"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/cluster/endpoints"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/core/rpc"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/profiler"
	"github.com/njtc406/emberengine/engine/pkg/utils/concurrent"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
)

// TODO 还需要给部分可自定义的组件增加一个设置的入口,不然需要覆写整个init太麻烦

// TODO 之后将所有的serverId换个名字,叫做namespace,或者group,用来划分服务组

type Service struct {
	Module
	inf.IMessageInvoker

	pid  *actor.PID // 服务基础信息
	name string     // 服务名称

	src                    inf.IService // 服务源
	cfg                    interface{}  // 服务配置
	status                 int32        // 服务状态(0初始化 1启动中 2启动  3关闭中 4关闭 5退休)
	isPrimarySecondaryMode bool         // 是否是主从模式

	mailbox              inf.IMailbox        // 邮箱
	eventProcessor       inf.IEventProcessor // 事件管理器
	globalEventProcessor inf.IEventProcessor // 全局事件管理器

	profiler *profiler.Profiler // 性能监控

	logger log.ILogger
}

func (s *Service) fixConf(serviceInitConf *config.ServiceInitConf) *config.ServiceInitConf {
	if serviceInitConf == nil {
		serviceInitConf = &config.ServiceInitConf{
			ServiceId:   "",
			ServiceName: "",
			Type:        "Normal",
			TimerConf: &config.TimerConf{
				TimerSize:       def.DefaultTimerSize,
				TimerBucketSize: def.DefaultTimerBucketSize,
			},
			RpcType: def.RpcTypeGrpc,
			WorkerConf: &config.WorkerConf{
				UserMailboxSize:      def.DefaultUserMailboxSize,
				SystemMailboxSize:    def.DefaultSysMailboxSize,
				WorkerNum:            def.DefaultWorkerNum,
				DynamicWorkerScaling: false,
				VirtualWorkerRate:    def.DefaultVirtualWorkerRate,
			},
			LogConf: &config.ServiceLogConf{
				Enable: false,
				Config: nil,
			},
		}
		return serviceInitConf
	}

	if serviceInitConf.Type == "" {
		serviceInitConf.Type = "Normal"
	}
	if serviceInitConf.RpcType == "" {
		serviceInitConf.RpcType = def.RpcTypeRpcx
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

	if serviceInitConf.WorkerConf == nil {
		serviceInitConf.WorkerConf = &config.WorkerConf{
			UserMailboxSize:      def.DefaultUserMailboxSize,
			SystemMailboxSize:    def.DefaultSysMailboxSize,
			WorkerNum:            def.DefaultWorkerNum,
			DynamicWorkerScaling: false,
			VirtualWorkerRate:    def.DefaultVirtualWorkerRate,
		}
	} else {
		if serviceInitConf.WorkerConf.UserMailboxSize == 0 {
			serviceInitConf.WorkerConf.UserMailboxSize = def.DefaultUserMailboxSize
		}
		if serviceInitConf.WorkerConf.SystemMailboxSize == 0 {
			serviceInitConf.WorkerConf.SystemMailboxSize = def.DefaultSysMailboxSize
		}

		if serviceInitConf.WorkerConf.WorkerNum <= 0 {
			serviceInitConf.WorkerConf.WorkerNum = def.DefaultWorkerNum
		}

		if serviceInitConf.WorkerConf.VirtualWorkerRate <= 0 {
			serviceInitConf.WorkerConf.VirtualWorkerRate = def.DefaultVirtualWorkerRate
		}
	}

	return serviceInitConf
}

func (s *Service) Init(svc interface{}, serviceInitConf *config.ServiceInitConf, cfg interface{}) {
	if !atomic.CompareAndSwapInt32(&s.status, def.SvcStatusUnknown, def.SvcStatusInit) {
		return
	}
	// 整理配置参数
	serviceInitConf = s.fixConf(serviceInitConf)
	//s.logger.Debugf("service[%s] init conf: %+v", s.GetName(), serviceInitConf)
	// 初始化服务数据
	s.src = svc.(inf.IService)
	s.cfg = cfg

	// 初始化日志
	if serviceInitConf.LogConf.Enable {
		logger, err := log.NewDefaultLogger(path.Join(serviceInitConf.LogConf.Config.Path, serviceInitConf.LogConf.Config.Name), serviceInitConf.LogConf.Config, config.IsDebug())
		if err != nil {
			log.SysLogger.Panicf("service[%s] init logger error: %s", s.GetName(), err)
		} else {
			s.logger = logger
		}
	} else {
		// 使用系统日志
		s.logger = log.SysLogger
	}
	s.isPrimarySecondaryMode = serviceInitConf.IsPrimarySecondaryMode

	// 创建定时器调度器
	s.ITimerScheduler = timingwheel.NewTaskScheduler(serviceInitConf.TimerConf.TimerSize, serviceInitConf.TimerConf.TimerBucketSize)
	// 创建邮箱
	s.mailbox = mailbox.NewDefaultMailbox(serviceInitConf.WorkerConf, s)

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

	s.IConcurrent = concurrent.NewTaskScheduler()

	s.pid = endpoints.GetEndpointManager().CreatePid(serviceInitConf.ServerId, serviceInitConf.ServiceId, serviceInitConf.Type, s.name, serviceInitConf.Version, serviceInitConf.RpcType)
	if s.pid == nil {
		s.logger.Panicf("service[%s] create pid error", s.GetName())
		return
	}

	// 初始化根节点rpc处理器
	s.methodMgr = rpc.NewMethodMgr()
	s.IRpcHandler = rpc.NewHandler(s.self).Init(s.methodMgr)

	if err := s.src.OnInit(); err != nil {
		s.logger.Panicf("service[%s] onInit error: %s", s.GetName(), err)
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
	if err := s.src.OnStart(); err != nil {
		return err
	}

	s.setStatus(def.SvcStatusRunning) // 到这里算是服务已经准备好所有东西,准备工作都在OnStart中完成

	if !s.isPrimarySecondaryMode || s.IsPrivate() {
		// 没有开启主从模式或者私有服务,那么直接是主服务
		s.pid.SetMaster(true)
	}

	// 所有服务都注册到服务列表
	endpoints.GetEndpointManager().AddService(s)
	//s.logger.Infof("register service[%s] pid: %s", s.GetName(), s.pid.String())

	if err := s.src.OnStarted(); err != nil { // 这个阶段服务已经加入集群,需要集群操作的可以放这里完成
		return err
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
			if err := s.pushConcurrentCallback(t); err != nil {
				s.logger.Errorf("service [%s] submit concurrent callback error: %v", s.GetName(), err)
			}
		case t, ok := <-s.ITimerScheduler.GetTimerCbChannel():
			if !ok {
				return
			}
			if err := s.pushTimerCallback(t); err != nil {
				s.logger.Errorf("service [%s] submit timer callback error: %v", s.GetName(), err)
			}
		}
	}
}

func (s *Service) Stop() {
	if s.IsClosed() {
		// 防止多次关闭
		return
	}
	//s.logger.Debugf("service[%s] begin stop", s.GetName())
	atomic.StoreInt32(&s.status, def.SvcStatusClosing)

	// 关闭定时器
	s.ITimerScheduler.Stop()

	// 关闭并发
	s.IConcurrent.Close()

	// 释放资源(这里面可能还会有call类型的调用,所以先执行)
	s.release()

	// 关闭邮箱(完全关闭所有的工作线程,不再接收新的消息)
	s.mailbox.Stop()

	atomic.StoreInt32(&s.status, def.SvcStatusClosed)
}

func (s *Service) release() {
	defer func() {
		if err := recover(); err != nil {
			s.logger.Errorf("service [%s] release error: %v", s.GetName(), err)
		}
	}()

	s.self.OnRelease()
	s.closeProfiler()

	// 服务关闭,从服务移除(等待其他释放完再移除,防止在释放的时候有同步调用,例如db等,会导致调用失败)
	endpoints.GetEndpointManager().RemoveService(s)
}

func (s *Service) PushEvent(evt inf.IEvent) error {
	//if !s.isRunning() {
	//	return def.ErrServiceIsUnavailable
	//}
	evt.IncRef()
	return s.mailbox.PostMessage(evt)
}

func (s *Service) PushRequest(c inf.IEnvelope) error {
	return s.mailbox.PostMessage(c)
}

func (s *Service) pushConcurrentCallback(evt concurrent.IConcurrentCallback) error {
	ev := event.NewEvent()
	ev.Type = event.ServiceConcurrentCallback
	ev.Data = evt
	return s.mailbox.PostMessage(ev)
}

func (s *Service) pushTimerCallback(t timingwheel.ITimer) error {
	ev := event.NewEvent()
	ev.Type = event.ServiceTimerCallback
	ev.SetHeader(def.DefaultDispatcherKey, t.GetName()) // 保证相同的回调在同一个worker处理
	ev.Data = t
	return s.mailbox.PostMessage(ev)
}

func (s *Service) SetName(name string) {
	s.name = name
}

func (s *Service) GetName() string {
	return s.name
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

func (s *Service) OnSetup(svc inf.IService) {
	if svc.GetName() == "" {
		s.name = reflect.Indirect(reflect.ValueOf(svc)).Type().Name()
	}
}

func (s *Service) IsClosed() bool {
	return atomic.LoadInt32(&s.status) > def.SvcStatusRunning
}

func (s *Service) OpenProfiler() {
	s.profiler = profiler.RegProfiler(s.pid.GetServiceUid())
	if s.profiler == nil {
		s.logger.Fatalf("profiler %s reg fail", s.GetName())
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
			s.logger.Errorf("service [%s] exec error: %v\ntrace:%s", s.GetName(), err, debug.Stack())
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

// InvokeSystemMessage 处理系统事件(这个函数是在mailbox的线程中被调用的)
func (s *Service) InvokeSystemMessage(ev inf.IEvent) {
	if !ev.IsRef() {
		return
	}
	tp := ev.GetType()

	var analyzer *profiler.Analyzer
	var open bool
	if s.profiler != nil {
		open = true
	}

	// TODO 这里需要改造一下,这么平铺写很麻烦,每次加了都要改,做成注册,性能分析那里可以在外层包一个函数,在这个函数里面执行注册的函数

	s.logger.Debugf("service[%s] receive system event[%d]", s.GetName(), tp)
	switch tp {
	case event.ServiceSuspended:
		ev.Release()
		if open {
			analyzer = s.profiler.Push(fmt.Sprintf("[SYS_MSG]%d", event.ServiceSuspended))
		}
		// 服务挂起
		s.mailbox.Suspend()
	case event.ServiceResumed:
		ev.Release()
		if open {
			analyzer = s.profiler.Push(fmt.Sprintf("[SYS_MSG] type:%d", event.ServiceSuspended))
		}
		// 服务恢复
		s.mailbox.Resume()
	case event.SysEventServiceClose:
		ev.Release()
		if open {
			analyzer = s.profiler.Push(fmt.Sprintf("[SYS_MSG] type:%d", event.ServiceSuspended))
		}
		// 服务关闭
		s.Stop()
	case event.ServiceHeartbeat:
		ev.Release()
		if open {
			analyzer = s.profiler.Push(fmt.Sprintf("[SYS_MSG] type:%d", event.ServiceSuspended))
		}
		// TODO 服务健康检查,需要回复服务负载等等信息
	case event.ServiceGlobalEventTrigger:
		s.safeExec(func() {
			evt := ev.(*event.Event)
			t := evt.Data.(*actor.Event)
			if open {
				analyzer = s.profiler.Push(fmt.Sprintf("[SYS_GLB_EVENT] type:%d", t.GetType()))
			}
			s.globalEventProcessor.EventHandler(t)
			ev.Release()
		})
	case event.RpcMsg:
		s.safeExec(func() {
			// rpc调用
			c := ev.(inf.IEnvelope)
			meta := c.GetMeta()
			data := c.GetData()
			if meta == nil || data == nil {
				c.Release()
				s.logger.Errorf("service[%s] receive call error, meta or data is nil", s.GetName())
				s.logger.Errorf("meta: %v", meta)
				s.logger.Errorf("data: %v", data)
				return
			}
			//s.logger.WithContext(c.GetContext()).Debugf("service[%s] receive call method[%s]", s.GetName(), c.GetMethod())
			if data.IsReply() {
				if open {
					analyzer = s.profiler.Push(fmt.Sprintf("[SYS_RPC_RESP] service:%s method:%s", meta.GetReceiverPid().GetServiceUid(), data.GetMethod()))
				}
				// 回复
				s.HandleResponse(c)
			} else {
				if open {
					analyzer = s.profiler.Push(fmt.Sprintf("[SYS_RPC_REQ] service:%s method:%s", meta.GetReceiverPid().GetServiceUid(), data.GetMethod()))
				}
				// rpc调用
				s.HandleRequest(c)
			}
		})
	default:
		if open {
			analyzer = s.profiler.Push(fmt.Sprintf("[SYS_OTHER_EVENT] type:%d", ev.GetType()))
		}
		// 其他系统事件
		s.eventProcessor.EventHandler(ev)
		ev.Release()
	}
	if analyzer != nil {
		analyzer.Pop()
		analyzer = nil
	}
}

func (s *Service) InvokeUserMessage(ev inf.IEvent) {
	if !ev.IsRef() {
		// 前面的超时之后导致后面的已经被丢弃
		return
	}
	tp := ev.GetType()

	var analyzer *profiler.Analyzer
	var open bool
	if s.profiler != nil {
		open = true
	}
	// TODO 这里需要改造一下,这么平铺写很麻烦,每次加了都要改,做成注册,性能分析那里可以在外层包一个函数,在这个函数里面执行注册的函数
	switch tp {
	case event.RpcMsg:
		s.safeExec(func() {
			// rpc调用
			c := ev.(inf.IEnvelope)
			//s.logger.WithContext(c.GetContext()).Debugf("service[%s] receive call method[%s]", s.GetName(), c.GetData().GetMethod())
			meta := c.GetMeta()
			data := c.GetData()
			if meta == nil || data == nil {
				c.Release()
				s.logger.Errorf("service[%s] receive rpc msg call error, meta or data is nil", s.GetName())
				s.logger.Errorf("meta: %v", meta)
				s.logger.Errorf("data: %v", data)
				return
			}
			if data.IsReply() {
				if open {
					analyzer = s.profiler.Push(fmt.Sprintf("[USER_RPC_RESP] service:%s method:%s", meta.GetReceiverPid().GetServiceUid(), data.GetMethod()))
				}
				// 回复
				s.HandleResponse(c)
			} else {
				if open {
					analyzer = s.profiler.Push(fmt.Sprintf("[USER_RPC_REQ] service:%s method:%s", meta.GetReceiverPid().GetServiceUid(), data.GetMethod()))
				}
				// rpc调用
				s.HandleRequest(c)
			}
		})
	case event.ServiceTimerCallback:
		// TODO 定时器要不要支持系统级(执行优先级更高)回调?
		s.safeExec(func() {
			defer ev.Release()
			evt := ev.(*event.Event)
			t := evt.Data.(timingwheel.ITimer)
			if open {
				analyzer = s.profiler.Push(fmt.Sprintf("[USER_TIME_CB] name:%s", t.GetName()))
			}
			t.Do()
		})
	case event.ServiceConcurrentCallback:
		// TODO 并发回调要不要支持系统级(执行优先级更高)回调?
		s.safeExec(func() {
			defer ev.Release()
			evt := ev.(*event.Event)
			t := evt.Data.(concurrent.IConcurrentCallback)
			if open {
				analyzer = s.profiler.Push(fmt.Sprintf("[USER_ASYNC_CB] name:%s", t.GetName()))
			}
			t.DoCallback()
		})
	case event.ServiceGlobalEventTrigger:
		s.safeExec(func() {
			defer ev.Release()
			evt := ev.(*event.Event)
			t := evt.Data.(*actor.Event)
			if open {
				analyzer = s.profiler.Push(fmt.Sprintf("[USER_GLB_EVENT] type:%d", t.GetType()))
			}
			s.globalEventProcessor.EventHandler(t)
		})
	default:
		if open {
			analyzer = s.profiler.Push(fmt.Sprintf("[USER_OTHER_EVENT] type:%d", ev.GetType()))
		}
		s.safeExec(func() {
			defer ev.Release()
			s.eventProcessor.EventHandler(ev)
		})
	}

	if analyzer != nil {
		analyzer.Pop()
		analyzer = nil
	}
}

func (s *Service) EscalateFailure(reason interface{}, evt inf.IEvent) {
	s.logger.Errorf("service [%s] event[%d] EscalateFailure: %v", s.GetName(), evt.GetType(), reason)
}

func (s *Service) IsPrivate() bool {
	return s.methodMgr.IsPrivate()
}

func (s *Service) GetLogger() log.ILogger {
	return s.logger
}

func (s *Service) IsPrimarySecondaryMode() bool {
	return s.isPrimarySecondaryMode
}
