// Package core
// @Title  title
// @Description  desc
// @Author  pc  2024/11/5
// @Update  pc  2024/11/5
package core

import (
	"fmt"
	"reflect"
	"runtime/debug"
	"sync/atomic"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/cluster/endpoints"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/core/mailbox"
	"github.com/njtc406/emberengine/engine/pkg/core/rpc"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/profiler"
	"github.com/njtc406/emberengine/engine/pkg/utils/concurrent"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
)

type Service struct {
	Module
	inf.IMessageInvoker

	pid  *actor.PID // 服务基础信息
	name string     // 服务名称

	src    inf.IService // 服务源
	cfg    interface{}  // 服务配置
	status int32        // 服务状态(0初始化 1启动中 2启动  3关闭中 4关闭 5退休)

	mailbox        inf.IMailbox        // 邮箱
	eventProcessor inf.IEventProcessor // 事件管理器
	//profiler       *profiler.Profiler  // 性能分析
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
		}
		return serviceInitConf
	}

	if serviceInitConf.Type == "" {
		serviceInitConf.Type = "Normal"
	}
	if serviceInitConf.RpcType == "" {
		serviceInitConf.RpcType = def.RpcTypeRpcx
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
	//log.SysLogger.Debugf("service[%s] init conf: %+v", s.GetName(), serviceInitConf)
	// 初始化服务数据
	s.src = svc.(inf.IService)
	s.cfg = cfg

	// 创建定时器调度器
	s.timerDispatcher = timingwheel.NewTaskScheduler(serviceInitConf.TimerConf.TimerSize, serviceInitConf.TimerConf.TimerBucketSize)
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

	s.IConcurrent = concurrent.NewTaskScheduler()
	s.pid = endpoints.GetEndpointManager().CreatePid(serviceInitConf.ServerId, serviceInitConf.ServiceId, serviceInitConf.Type, s.name, serviceInitConf.Version, serviceInitConf.RpcType)
	if s.pid == nil {
		log.SysLogger.Panicf("service[%s] create pid error", s.GetName())
		return
	}

	// 初始化根节点rpc处理器
	s.methodMgr = rpc.NewMethodMgr()
	s.IRpcHandler = rpc.NewHandler(s.self).Init(s.methodMgr)

	if err := s.src.OnInit(); err != nil {
		log.SysLogger.Panicf("service[%s] onInit error: %s", s.GetName(), err)
	}
}

func (s *Service) Start() error {
	// 按理说服务都应该是单线程的被初始化,所以应该不需要这样变更状态的
	if !atomic.CompareAndSwapInt32(&s.status, def.SvcStatusInit, def.SvcStatusStarting) {
		return fmt.Errorf("service[%s] status[%d] has inited", s.GetName(), s.status)
	}

	// 启动邮箱
	s.mailbox.Start()

	go s.startListenCallback()

	if err := s.src.OnStart(); err != nil {
		return err
	}

	// 所有服务都注册到服务列表
	endpoints.GetEndpointManager().AddService(s)
	log.SysLogger.Infof("register service[%s] pid: %s", s.GetName(), s.pid.String())

	s.setStatus(def.SvcStatusRunning)

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
				log.SysLogger.Errorf("service [%s] submit concurrent callback error: %v", s.GetName(), err)
			}
		case t, ok := <-s.timerDispatcher.C:
			if !ok {
				return
			}
			if err := s.pushTimerCallback(t); err != nil {
				log.SysLogger.Errorf("service [%s] submit timer callback error: %v", s.GetName(), err)
			}
		}
	}
}

func (s *Service) Stop() {
	if s.IsClosed() {
		// 防止多次关闭
		return
	}
	//log.SysLogger.Debugf("service[%s] begin stop", s.GetName())
	atomic.StoreInt32(&s.status, def.SvcStatusClosing)

	// 关闭定时器
	s.timerDispatcher.Stop()
	//log.SysLogger.Debugf("service[%s] stop timer", s.GetName())
	// 先关闭邮箱
	s.mailbox.Stop()
	//log.SysLogger.Debugf("service[%s] stop mailbox", s.GetName())

	// 关闭并发
	s.IConcurrent.Close()

	// 释放资源
	s.release()
	//log.SysLogger.Debugf("service[%s] stop service", s.GetName())

	atomic.StoreInt32(&s.status, def.SvcStatusClosed)
}

func (s *Service) release() {
	defer func() {
		if err := recover(); err != nil {
			log.SysLogger.Errorf("service [%s] release error: %v", s.GetName(), err)
		}
	}()

	s.self.OnRelease()
	s.closeProfiler()

	// 服务关闭,从服务移除(等待其他释放完再移除,防止在释放的时候有同步调用,例如db等,会导致调用失败)
	endpoints.GetEndpointManager().RemoveService(s)
}

func (s *Service) PushEvent(evt inf.IEvent) error {
	if !s.isRunning() {
		return def.ServiceIsUnavailable
	}
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
	ev.Key = t.GetName() // 保证相同的回调在同一个worker处理
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

func (s *Service) OnRelease() {}

func (s *Service) OnSetup(svc inf.IService) {
	if svc.GetName() == "" {
		s.name = reflect.Indirect(reflect.ValueOf(svc)).Type().Name()
	}
}

func (s *Service) IsClosed() bool {
	return atomic.LoadInt32(&s.status) > def.SvcStatusRunning
}

func (s *Service) getUniqueKey() string {
	return fmt.Sprintf("%s-%s", s.pid.GetName(), s.pid.GetServiceUid())
}

// TODO 这里需要修改,Profiler被移动到workerPool中了
func (s *Service) OpenProfiler() {
	//s.profiler = profiler.RegProfiler(s.getUniqueKey())
	//if s.profiler == nil {
	//	log.SysLogger.Fatalf("profiler %s reg fail", s.GetName())
	//}
}

func (s *Service) GetProfiler() *profiler.Profiler {
	return nil //s.profiler
}

func (s *Service) closeProfiler() {
	//if s.profiler != nil {
	//	profiler.UnRegProfiler(s.getUniqueKey())
	//}
}

func (s *Service) GetServiceCfg() interface{} {
	return s.cfg
}

func (s *Service) safeExec(f func()) {
	defer func() {
		if err := recover(); err != nil {
			log.SysLogger.Errorf("service [%s] exec error: %v\ntrace:%s", s.GetName(), err, debug.Stack())
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
func (s *Service) InvokeSystemMessage(evt inf.IEvent) {
	tp := evt.GetType()
	log.SysLogger.Debugf("service[%s] receive system event[%d]", s.GetName(), tp)
	switch tp {
	case event.ServiceSuspended:
		// 服务挂起
		s.mailbox.Suspend()
	case event.ServiceResumed:
		// 服务恢复
		s.mailbox.Resume()
	case event.SysEventServiceClose:
		// 服务关闭
		s.Stop()
	default:
		// 其他系统事件
		s.eventProcessor.EventHandler(evt)
	}
}

func (s *Service) InvokeUserMessage(ev inf.IEvent) {
	tp := ev.GetType()

	switch tp {
	case event.RpcMsg:
		s.safeExec(func() {
			// rpc调用
			c := ev.(inf.IEnvelope)
			log.SysLogger.Debugf("service[%s] receive call method[%s]", s.GetName(), c.GetMethod())
			if c.IsReply() {
				// 回复
				s.HandleResponse(c)
			} else {
				// rpc调用
				s.HandleRequest(c)
			}
		})
	case event.ServiceTimerCallback:
		evt := ev.(*event.Event)
		t := evt.Data.(timingwheel.ITimer)
		t.Do()
	case event.ServiceConcurrentCallback:
		evt := ev.(*event.Event)
		t := evt.Data.(concurrent.IConcurrentCallback)
		t.DoCallback()
	default:
		s.safeExec(func() {
			s.eventProcessor.EventHandler(ev)
		})
	}

}

func (s *Service) EscalateFailure(reason interface{}, evt inf.IEvent) {
	log.SysLogger.Errorf("service [%s] event[%d] EscalateFailure: %v", s.GetName(), evt.GetType(), reason)
}

func (s *Service) IsPrivate() bool {
	return s.methodMgr.IsPrivate()
}
