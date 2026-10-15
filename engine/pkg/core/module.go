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
	"sync/atomic"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/core/rpc"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/concurrent"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
)

var (
	_ inf.IModule          = (*Module)(nil)
	_ inf.IModuleLifecycle = (*Module)(nil)
	_ inf.IModuleIdentity  = (*Module)(nil)
	_ inf.IModuleHierarchy = (*Module)(nil)
)

// coreModuleCarrier 是 core 包内部桥接接口。
// 外部仍然使用 IModule，不改变对用户自定义模块类型的暴露。
type coreModuleCarrier interface {
	CoreModule() *Module
}

type Module struct {
	//interfaces.IRpcHandler
	concurrent.IConcurrent
	moduleId     uint32 // 模块ID
	moduleName   string // 模块名称
	moduleIdSeed uint32 // 模块ID种子(如果没有给模块ID，则子模块从该种子开始分配)

	self     inf.IModule            // 自身
	parent   inf.IModule            // 父模块
	children map[uint32]inf.IModule // 子模块列表 map[moduleId]module

	root         inf.IModule            // 根模块
	rootContains map[uint32]inf.IModule // 根模块下所有模块(包括所有的子模块)

	eventHandler *event.Handler // 事件处理器

	timingwheel.ITimerScheduler

	inf.IRpcHandler // rpc处理器(从service移动到这里,主要是为了能直接调用模块的接口,不需要都从service那层转一次)

	methodMgr inf.IMethodMgr // 接口信息管理器

	// 独立日志
	enableLogging bool
	logger        log.ILoggerX
	log.ILoggerX  // 需要在服务init阶段之后才能使用
}

func asCoreModule(module inf.IModule) (*Module, bool) {
	if module == nil {
		return nil, false
	}
	carrier, ok := module.(coreModuleCarrier)
	if !ok {
		return nil, false
	}
	coreModule := carrier.CoreModule()
	if coreModule == nil {
		return nil, false
	}
	return coreModule, true
}

func (m *Module) rootCoreModule() (*Module, bool) {
	if m == nil || m.root == nil {
		return nil, false
	}
	return asCoreModule(m.root)
}

func (m *Module) AddModule(module inf.IModule) (uint32, error) {
	if m.GetEventProcessor() == nil {
		return 0, def.ErrModuleNotInitialized
	}

	pModule, ok := asCoreModule(module)
	if !ok {
		return 0, fmt.Errorf("module %T does not embed core.Module", module)
	}
	rootModule, ok := m.rootCoreModule()
	if !ok {
		return 0, def.ErrModuleNotInitialized
	}

	if pModule.GetModuleID() == 0 {
		pModule.moduleId = m.newModuleID()
		if pModule.moduleId == 0 {
			return 0, def.ErrModuleNotInitialized
		}
	}

	if m.children == nil {
		m.children = make(map[uint32]inf.IModule)
	}

	if _, ok := m.children[pModule.GetModuleID()]; ok {
		return 0, def.ErrModuleHadRegistered
	}

	pModule.self = module
	pModule.parent = m.self
	pModule.ITimerScheduler = rootModule.ITimerScheduler
	pModule.root = m.root
	pModule.logger = m.GetService().GetLogger()
	pModule.ILoggerX = m.GetService().GetLogger()
	pModule.ILoggerX = pModule.ILoggerX.WithFields(log.Fields{
		"mId":   pModule.GetModuleID(),
		"mName": pModule.GetModuleName(),
	})
	pModule.moduleName = reflect.Indirect(reflect.ValueOf(module)).Type().Name()
	pModule.eventHandler = event.NewTriggerHandler()
	pModule.eventHandler.Init(m.eventHandler.GetProcessor().(*event.Processor))
	pModule.IConcurrent = m.IConcurrent
	rpcHandler, err := rpc.NewHandler(pModule.self).Init(m.root.GetMethodMgr())
	if err != nil {
		return 0, err
	}
	pModule.IRpcHandler = rpcHandler
	if err := module.OnInit(); err != nil {
		return 0, err
	}
	m.children[pModule.GetModuleID()] = module
	if rootModule.rootContains == nil {
		rootModule.rootContains = make(map[uint32]inf.IModule)
	}
	rootModule.rootContains[pModule.GetModuleID()] = module

	//m.Debugf("add module [%s] completed", pModule.GetModuleName())

	return pModule.moduleId, nil
}

func (m *Module) ReleaseModule(moduleId uint32) {
	module := m.GetModule(moduleId)
	if module == nil {
		m.Errorf("module %d not found", moduleId)
		return
	}
	pModule, ok := asCoreModule(module)
	if !ok {
		m.Errorf("module %d base type is not *core.Module", moduleId)
		return
	}

	//m.logger.Debugf("release module %s ,id: %d name:%s", m.GetModuleName(), moduleId, pModule.GetModuleName())

	//释放子孙
	for id := range pModule.children {
		m.ReleaseModule(id)
	}

	pModule.self.OnRelease()
	pModule.GetEventHandler().Destroy()
	//m.Debugf("Release module %s", pModule.GetModuleName())
	delete(m.children, moduleId)
	if rootModule, ok := m.rootCoreModule(); ok && rootModule.rootContains != nil {
		delete(rootModule.rootContains, moduleId)
	}
	// 从methodmgr中移除模块api(service那层的api是不会移除的)
	m.root.GetMethodMgr().RemoveMethods(m.GetMethods())

	//清理被删除的Module
	pModule.reset()
}

func (m *Module) OnInit() error {
	return nil
}

func (m *Module) OnRelease() {}

func (m *Module) newModuleID() uint32 {
	rootModule, ok := m.rootCoreModule()
	if !ok {
		return 0
	}
	return atomic.AddUint32(&rootModule.moduleIdSeed, 1)
}

func (m *Module) NewModuleID() uint32 {
	return m.newModuleID()
}

func (m *Module) SetPid(pid *actor.PID) {
	m.root.(inf.IService).SetPid(pid)
}

func (m *Module) GetPid() *actor.PID {
	return m.root.(inf.IService).GetPid()
}

func (m *Module) SetModuleID(id uint32) bool {
	if m.moduleId != 0 {
		return false
	}
	m.moduleId = id
	return true
}

func (m *Module) GetModuleID() uint32 {
	return m.moduleId
}

func (m *Module) GetModuleName() string {
	return m.moduleName
}

func (m *Module) GetModule(moduleId uint32) inf.IModule {
	rootModule, ok := m.rootCoreModule()
	if !ok || rootModule.rootContains == nil {
		return nil
	}
	iModule, ok := rootModule.rootContains[moduleId]
	if !ok {
		return nil
	}
	return iModule
}

func (m *Module) GetRoot() inf.IModule {
	return m.root
}

func (m *Module) GetParent() inf.IModule {
	return m.parent
}

func (m *Module) GetBaseModule() inf.IModule {
	return m
}

func (m *Module) CoreModule() *Module {
	return m
}

func (m *Module) GetService() inf.IService {
	return m.GetRoot().(inf.IService)
}

func (m *Module) GetEventProcessor() inf.IEventProcessor {
	return m.eventHandler.GetProcessor()
}

func (m *Module) GetEventHandler() inf.IEventHandler {
	return m.eventHandler
}

func (m *Module) GetEventHandlerRegistry() inf.IEventHandlerRegistrar {
	return m.eventHandler
}

func (m *Module) GetMethodMgr() inf.IMethodMgr {
	return m.methodMgr
}

func (m *Module) ReleaseAllChildModule() {
	// 释放所有子模块
	for id := range m.children {
		m.ReleaseModule(id)
	}
}

func (m *Module) reset() {
	m.moduleId = 0
	m.moduleName = ""
	m.moduleIdSeed = 0
	m.self = nil
	m.parent = nil
	m.children = nil
	m.ITimerScheduler = nil
	m.root = nil
	m.rootContains = nil
	m.eventHandler = nil
	m.IConcurrent = nil
	m.IRpcHandler = nil
	m.methodMgr = nil
	m.logger = nil
	m.ILoggerX = nil
}

func (m *Module) TriggerEvent(ctx context.Context, tp def.EventType, ev inf.IEvent) {
	m.eventHandler.Trigger(ctx, tp, ev)
}

func (m *Module) GetLogger() log.ILoggerX {
	return m.logger
}
