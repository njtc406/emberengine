// Package core
// @Title  title
// @Description  desc
// @Author  pc  2024/11/5
// @Update  pc  2024/11/5
package core

import (
	"fmt"
	"github.com/njtc406/emberengine/engine/actor"
	"github.com/njtc406/emberengine/engine/cluster/endpoints"
	"github.com/njtc406/emberengine/engine/config"
	"github.com/njtc406/emberengine/engine/def"
	"github.com/njtc406/emberengine/engine/errdef"
	"github.com/njtc406/emberengine/engine/event"
	"github.com/njtc406/emberengine/engine/inf"
	"github.com/njtc406/emberengine/engine/profiler"
	"github.com/njtc406/emberengine/engine/rpc"
	"github.com/njtc406/emberengine/engine/utils/asynclib"
	"github.com/njtc406/emberengine/engine/utils/concurrent"
	"github.com/njtc406/emberengine/engine/utils/log"
	"github.com/njtc406/emberengine/engine/utils/timingwheel"
	"reflect"
	"runtime/debug"
	"sync"
	"sync/atomic"
)

type Service struct {
	Module

	pid *actor.PID // 服务基础信息

	id          string // 服务唯一id(针对本地节点的相同服务名称中唯一)
	name        string // 服务名称
	serviceType string // 服务类型
	rpcType     string // 远程调用类型(默认rpcx)
	serverId    int32  // 服务id
	version     int64  // 服务版本

	src          inf.IService       // 服务源
	cfg          interface{}        // 服务配置
	status       int32              // 服务状态(0初始化 1启动中 2启动  3关闭中 4关闭 5退休)
	goroutineNum int32              // 协程数量
	wg           sync.WaitGroup     // 协程等待
	closeSignal  chan struct{}      // 关闭信号
	mailBox      chan inf.IEvent    // 事件队列
	profiler     *profiler.Profiler // 性能分析

	rpcHandler     rpc.Handler         // rpc处理器
	eventProcessor inf.IEventProcessor // 事件管理器
}

func (s *Service) fixConf(serviceInitConf *config.ServiceInitConf) *config.ServiceInitConf {
	if serviceInitConf == nil {
		serviceInitConf = &config.ServiceInitConf{
			ServiceId:    "",
			ServiceName:  "",
			Type:         "Normal",
			ServerId:     0,
			TimerSize:    def.DefaultTimerSize,
			MailBoxSize:  def.DefaultMailBoxSize,
			GoroutineNum: def.DefaultGoroutineNum,
			RpcType:      def.RpcTypeRpcx,
		}
		return serviceInitConf
	}

	if serviceInitConf.Type == "" {
		serviceInitConf.Type = "Normal"
	}
	if serviceInitConf.RpcType == "" {
		serviceInitConf.RpcType = def.RpcTypeRpcx
	}
	if serviceInitConf.TimerSize == 0 {
		serviceInitConf.TimerSize = def.DefaultTimerSize
	}
	if serviceInitConf.MailBoxSize == 0 {
		serviceInitConf.MailBoxSize = def.DefaultMailBoxSize
	}
	if serviceInitConf.GoroutineNum == 0 {
		serviceInitConf.GoroutineNum = def.DefaultGoroutineNum
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
	s.id = serviceInitConf.ServiceId
	s.serviceType = serviceInitConf.Type
	s.rpcType = serviceInitConf.RpcType
	s.serverId = serviceInitConf.ServerId
	s.src = svc.(inf.IService)
	s.cfg = cfg
	if s.closeSignal == nil {
		s.closeSignal = make(chan struct{})
	}
	if s.timerDispatcher == nil {
		s.timerDispatcher = timingwheel.NewTaskScheduler(serviceInitConf.TimerSize, serviceInitConf.TimerBucketSize)
	}
	if s.mailBox == nil {
		s.mailBox = make(chan inf.IEvent, serviceInitConf.MailBoxSize)
	}
	s.goroutineNum = serviceInitConf.GoroutineNum

	// rpc处理器
	s.rpcHandler.Init(svc.(inf.IRpcHandler))
	s.IRpcHandler = &s.rpcHandler

	// 初始化根模块
	s.self = svc.(inf.IModule)
	s.root = s.self
	s.rootContains = make(map[uint32]inf.IModule)
	s.moduleIdSeed = def.DefaultModuleIdSeed
	s.eventProcessor = event.NewProcessor()
	s.eventProcessor.Init(s)
	s.eventHandler = event.NewHandler()
	s.eventHandler.Init(s.eventProcessor)
	s.IConcurrent = concurrent.NewConcurrent()
	s.pid = endpoints.GetEndpointManager().CreatePid(s.serverId, s.id, s.serviceType, s.name, s.version, s.rpcType)
	if s.pid == nil {
		log.SysLogger.Panicf("service[%s] create pid error", s.GetName())
		return
	}

	if err := s.src.OnInit(); err != nil {
		log.SysLogger.Panicf("service[%s] onInit error: %s", s.GetName(), err)
	}
}

func (s *Service) Start() error {
	// 按理说服务都应该是单线程的被初始化,所以应该不需要这样变更状态的
	if !atomic.CompareAndSwapInt32(&s.status, def.SvcStatusInit, def.SvcStatusStarting) {
		return fmt.Errorf("service[%s] status[%d] has inited", s.GetName(), s.status)
	}

	if err := s.src.OnStart(); err != nil {
		return err
	}

	var waitRun sync.WaitGroup
	for i := int32(0); i < s.goroutineNum; i++ {
		s.wg.Add(1)
		waitRun.Add(1)
		asynclib.Go(func() {
			waitRun.Done()
			s.run()
		})
	}
	waitRun.Wait()

	// 所有服务都注册到服务列表
	endpoints.GetEndpointManager().AddService(s.pid, s.GetRpcHandler())
	log.SysLogger.Infof(" service[%s] pid: %s", s.GetName(), s.pid.String())
	return nil
}

func (s *Service) run() {
	// TODO 这里之后增加一个mpse模式的消息处理
	defer s.wg.Done()

	var bStop bool

	concurrent := s.IConcurrent.(*concurrent.Concurrent)
	concurrentCBChannel := concurrent.GetCallBackChannel()

	for {
		var analyzer *profiler.Analyzer
		select {
		case <-s.closeSignal:
			if !bStop {
				bStop = true // 关闭信号
				concurrent.Close()
				s.timerDispatcher.Stop()
			}
		case cb := <-concurrentCBChannel:
			s.safeExec(func() {
				if s.profiler != nil {
					analyzer = s.profiler.Push(fmt.Sprintf("[Concurrent]%s", reflect.TypeOf(cb).String()))
				}
				concurrent.DoCallback(cb) // 异步执行
				if analyzer != nil {
					analyzer.Pop()
					analyzer = nil
				}
			})
		case ev := <-s.mailBox:
			// 事件处理
			switch ev.GetType() {
			case event.SysEventRpc:
				s.safeExec(func() {
					// rpc调用
					cEvent, ok := ev.(*event.Event)
					if !ok {
						log.SysLogger.Error("event type error")
						return
					}
					c := cEvent.Data.(inf.IEnvelope)
					if c.IsReply() {
						if s.profiler != nil {
							analyzer = s.profiler.Push(fmt.Sprintf("[RPCResponse]%s", c.GetMethod()))
						}
						// 回复
						s.rpcHandler.HandleResponse(c)
					} else {
						if s.profiler != nil {
							analyzer = s.profiler.Push(fmt.Sprintf("[RPCRequest]%s", c.GetMethod()))
						}
						// rpc调用
						s.rpcHandler.HandleRequest(c)
					}

					event.ReleaseEvent(cEvent)

					if analyzer != nil {
						analyzer.Pop()
						analyzer = nil
					}
				})
			default:
				s.safeExec(func() {
					if s.profiler != nil {
						analyzer = s.profiler.Push(fmt.Sprintf("[SvcEvent][%d]", ev.GetType()))
					}
					s.eventProcessor.EventHandler(ev)
					if analyzer != nil {
						analyzer.Pop()
						analyzer = nil
					}
				})
			}
		case t := <-s.timerDispatcher.C:
			s.safeExec(func() {
				// 定时器处理
				if s.profiler != nil {
					analyzer = s.profiler.Push("[timer]" + s.GetName() + "." + t.GetName())
				}
				log.SysLogger.Debugf("service[%s] timer[%s]", s.GetName(), t.GetName())
				t.Do() // Tips:所有定时器的执行时如果有时间判断请注意,定时器的触发受到前置逻辑的影响可能在执行的那一刻已经超过时间,所以尽量不要用==去判断时间
				if analyzer != nil {
					analyzer.Pop()
					analyzer = nil
				}
			})
		}

		if bStop {
			// 等待所有channel处理完成后关闭, timerDispatcher中可能有些timer是没有保存的,所以这里可能会有点点小问题,可能需要再加个超时,防止一直有timer回调
			if len(s.mailBox) > 0 || len(s.timerDispatcher.C) > 0 {
				continue
			}

			if atomic.AddInt32(&s.goroutineNum, -1) <= 0 {
				s.release() // 关闭最后一个协程的时候才调用
			}
			break
		}
	}
}

func (s *Service) Stop() {
	close(s.closeSignal)
	s.status = def.SvcStatusClosing
	s.wg.Wait()
	s.status = def.SvcStatusClosed
}

func (s *Service) release() {
	defer func() {
		if err := recover(); err != nil {
			log.SysLogger.Errorf("service [%s] release error: %v\ntrace:%s", s.GetName(), err, debug.Stack())
		}
	}()

	s.self.OnRelease()
	s.closeProfiler()

	// 服务关闭,从服务移除(等待其他释放完再移除,防止在释放的时候有同步调用,例如db等,会导致调用失败)
	endpoints.GetEndpointManager().RemoveService(s.GetPid())
}

func (s *Service) pushEvent(e inf.IEvent) error {
	select {
	case s.mailBox <- e:
	default:
		log.SysLogger.Errorf("service[%s] event channel full", s.GetName())
		return errdef.EventChannelIsFull
	}

	return nil
}

func (s *Service) PushEvent(e inf.IEvent) error {
	return s.pushEvent(e)
}

func (s *Service) PushRequest(c inf.IEnvelope) error {
	ev := event.NewEvent()
	ev.Type = event.SysEventRpc
	ev.Data = c
	return s.pushEvent(ev)
}

func (s *Service) PushHttpEvent(e interface{}) error {
	ev := event.NewEvent()
	ev.Type = event.SysEventHttpMsg
	ev.Data = e
	return s.pushEvent(ev)
}

func (s *Service) SetName(name string) {
	s.name = name
}

func (s *Service) GetName() string {
	return s.name
}

func (s *Service) SetServiceId(id string) {
	s.id = id
}

func (s *Service) GetServiceId() string {
	return s.id
}

func (s *Service) GetServerId() int32 {
	return s.serverId
}

func (s *Service) GetPid() *actor.PID {
	return s.pid
}

func (s *Service) GetRpcHandler() inf.IRpcHandler {
	return &s.rpcHandler
}

func (s *Service) GetServiceEventChannelNum() int {
	return len(s.mailBox)
}

func (s *Service) GetServiceTimerChannelNum() int {
	return len(s.timerDispatcher.C)
}

func (s *Service) OnInit() error {
	return nil
}

func (s *Service) OnStart() error {
	return nil
}

func (s *Service) OnRelease() {}

func (s *Service) SetGoRoutineNum(goroutineNum int32) {
	if atomic.LoadInt32(&s.status) != def.SvcStatusInit { // 已经启动的不允许修改
		return
	}

	s.goroutineNum = goroutineNum
}

func (s *Service) OnSetup(svc inf.IService) {
	if svc.GetName() == "" {
		s.name = reflect.Indirect(reflect.ValueOf(svc)).Type().Name()
	}
}

func (s *Service) IsClosed() bool {
	return atomic.LoadInt32(&s.status) > def.SvcStatusRunning
}

func (s *Service) getUniqueKey() string {
	var name = s.GetName()
	if s.id != "" {
		name = fmt.Sprintf("%s-%s", name, s.id)
	}
	return name
}

func (s *Service) OpenProfiler() {
	s.profiler = profiler.RegProfiler(s.getUniqueKey())
	if s.profiler == nil {
		log.SysLogger.Fatalf("profiler %s reg fail", s.GetName())
	}
}

func (s *Service) GetProfiler() *profiler.Profiler {
	return s.profiler
}

func (s *Service) closeProfiler() {
	if s.profiler != nil {
		profiler.UnRegProfiler(s.getUniqueKey())
	}
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
