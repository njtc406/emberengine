// Package inf
// @Title  title
// @Description  desc
// @Author  pc  2024/11/5
// @Update  pc  2024/11/5
package inf

import (
	"github.com/njtc406/emberengine/engine/actor"
	"github.com/njtc406/emberengine/engine/def"
	"github.com/njtc406/emberengine/engine/utils/concurrent"
	"github.com/njtc406/emberengine/engine/utils/timer"
	"time"
)

type IModule interface {
	IModuleLifecycle
	IModuleIdentity
	IModuleHierarchy
	IModuleServiceEvent
	concurrent.IConcurrent
}

type IModuleLifecycle interface {
	OnInit() error
	OnRelease()
}

type IModuleIdentity interface {
	SetModuleID(uint32) bool // 设置模块ID
	GetModuleID() uint32     // 获取模块ID
	GetModuleName() string   // 获取模块名称
	NewModuleID() uint32     // 生成模块ID
	GetPid() *actor.PID
}

type IModuleHierarchy interface {
	AddModule(IModule) (uint32, error) // 添加子模块
	ReleaseAllChildModule()            // 释放所有子模块
	ReleaseModule(moduleId uint32)     // 释放指定模块
	GetModule(uint32) IModule          // 获取指定模块
	GetRoot() IModule                  // 获取根模块
	GetBaseModule() IModule            // 获取基础模块
	GetParent() IModule                // 获取父模块
	GetMethodMgr() IMethodMgr          // 获取方法管理器
}

type IModuleServiceEvent interface {
	GetService() IService               // 获取服务
	GetEventHandler() IEventHandler     // 获取事件处理器
	GetEventProcessor() IEventProcessor // 获取事件管理器
	NotifyEvent(IEvent)                 // 通知事件
}

type IModuleTimer interface {
	AfterFunc(d time.Duration, cb func(iTimer timer.ITimer)) timer.ITimer
	CronFunc(cronExpr *timer.CronExpr, cb func(timer.ITimer)) timer.ITimer
	NewTicker(d time.Duration, cb func(timer.ITimer)) timer.ITimer
}

type IMethodMgr interface {
	AddMethod(name string, info *def.MethodInfo)
	GetMethod(name string) (*def.MethodInfo, bool)
	RemoveMethods(names []string)
	IsPrivate() bool
}
