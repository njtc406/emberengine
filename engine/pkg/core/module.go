// Package core
// @Title  title
// @Description  desc
// @Author  pc  2024/11/5
// @Update  pc  2024/11/5
package core

import (
	"github.com/njtc406/emberengine/engine/pkg/def"
	"github.com/njtc406/emberengine/engine/pkg/utils/concurrent"
	"reflect"
	"sync/atomic"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/core/rpc"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/utils/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
)

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

	eventHandler    inf.IEventHandler          // 事件处理器
	timerDispatcher *timingwheel.TaskScheduler // 定时器调度器
	inf.IRpcHandler                            // rpc处理器(从service移动到这里,主要是为了能直接调用模块的接口,不需要都从service那层转一次)
	methodMgr       inf.IMethodMgr             // 接口信息管理器
}

func (m *Module) AddModule(module inf.IModule) (uint32, error) {
	if m.GetEventProcessor() == nil {
		return 0, def.ModuleNotInitialized
	}

	pModule := module.GetBaseModule().(*Module)

	if pModule.GetModuleID() == 0 {
		pModule.moduleId = m.newModuleID()
	}

	if m.children == nil {
		m.children = make(map[uint32]inf.IModule)
	}

	if _, ok := m.children[pModule.GetModuleID()]; ok {
		return 0, def.ModuleHadRegistered
	}

	pModule.self = module
	pModule.parent = m.self
	pModule.timerDispatcher = m.GetRoot().GetBaseModule().(*Module).timerDispatcher
	pModule.root = m.root
	pModule.moduleName = reflect.Indirect(reflect.ValueOf(module)).Type().Name()
	pModule.eventHandler = event.NewHandler()
	pModule.eventHandler.Init(m.eventHandler.GetEventProcessor())
	pModule.IConcurrent = m.IConcurrent
	pModule.IRpcHandler = rpc.NewHandler(pModule.self).Init(m.root.GetMethodMgr())
	if err := module.OnInit(); err != nil {
		return 0, err
	}
	m.children[pModule.GetModuleID()] = module
	m.GetRoot().GetBaseModule().(*Module).rootContains[pModule.GetModuleID()] = module

	//log.SysLogger.Debugf("add module [%s] completed", pModule.GetModuleName())

	return pModule.moduleId, nil
}

func (m *Module) OnInit() error {
	return nil
}

func (m *Module) OnRelease() {}

func (m *Module) newModuleID() uint32 {
	return atomic.AddUint32(&m.root.GetBaseModule().(*Module).moduleIdSeed, 1)
}

func (m *Module) NewModuleID() uint32 {
	return m.newModuleID()
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
	iModule, ok := m.GetRoot().GetBaseModule().(*Module).rootContains[moduleId]
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

func (m *Module) GetService() inf.IService {
	return m.GetRoot().(inf.IService)
}

func (m *Module) GetEventProcessor() inf.IEventProcessor {
	return m.eventHandler.GetEventProcessor()
}

func (m *Module) GetEventHandler() inf.IEventHandler {
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
	m.timerDispatcher = nil
	m.root = nil
	m.rootContains = nil
	m.eventHandler = nil
	m.IConcurrent = nil
}

func (m *Module) ReleaseModule(moduleId uint32) {
	pModule := m.GetModule(moduleId).GetBaseModule().(*Module)
	if pModule == nil {
		log.SysLogger.Errorf("module %d not found", moduleId)
		return
	}

	//log.SysLogger.Debugf("release module %s ,id: %d name:%s", m.GetModuleName(), moduleId, pModule.GetModuleName())

	//释放子孙
	for id := range pModule.children {
		m.ReleaseModule(id)
	}

	pModule.self.OnRelease()
	pModule.GetEventHandler().Destroy()
	//log.SysLogger.Debugf("Release module %s", pModule.GetModuleName())
	delete(m.children, moduleId)
	delete(m.GetRoot().GetBaseModule().(*Module).rootContains, moduleId)
	// 从methodmgr中移除模块api(service那层的api是不会移除的)
	m.root.GetMethodMgr().RemoveMethods(m.GetMethods())

	//清理被删除的Module
	pModule.reset()
}

func (m *Module) NotifyEvent(e inf.IEvent) {
	m.eventHandler.NotifyEvent(e)
}

func (m *Module) AfterFunc(d time.Duration, name string, f timingwheel.TimerCallback, args ...interface{}) *timingwheel.Timer {
	return m.timerDispatcher.AfterFunc(d, name, f, args...)
}

func (m *Module) AfterFuncWithStorage(d time.Duration, name string, f timingwheel.TimerCallback, args ...interface{}) (uint64, error) {
	return m.timerDispatcher.AfterFuncWithStorage(d, name, f, args...)
}

func (m *Module) AfterAsyncFunc(d time.Duration, name string, f func(...interface{}), args ...interface{}) *timingwheel.Timer {
	return m.timerDispatcher.AfterAsyncFunc(d, name, f, args...)
}

func (m *Module) TickerFunc(d time.Duration, name string, f timingwheel.TimerCallback, args ...interface{}) *timingwheel.Timer {
	return m.timerDispatcher.TickerFunc(d, name, f, args...)
}

func (m *Module) TickerFuncWithStorage(d time.Duration, name string, f timingwheel.TimerCallback, args ...interface{}) (uint64, error) {
	return m.timerDispatcher.TickerFuncWithStorage(d, name, f, args...)
}

func (m *Module) TickerAsyncFunc(d time.Duration, name string, f func(...interface{}), args ...interface{}) *timingwheel.Timer {
	return m.timerDispatcher.TickerAsyncFunc(d, name, f, args...)
}

func (m *Module) CronFunc(spec string, name string, f timingwheel.TimerCallback, args ...interface{}) *timingwheel.Timer {
	return m.timerDispatcher.CronFunc(spec, name, f, args...)
}

func (m *Module) CronAsyncFunc(spec string, name string, f func(...interface{}), args ...interface{}) *timingwheel.Timer {
	return m.timerDispatcher.CronAsyncFunc(spec, name, f, args...)
}

func (m *Module) CronFuncWithStorage(spec string, name string, f timingwheel.TimerCallback, args ...interface{}) (uint64, error) {
	return m.timerDispatcher.CronFuncWithStorage(spec, name, f, args...)
}
