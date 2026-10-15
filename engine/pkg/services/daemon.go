// Package services
// @Title  守护线程
// @Description  主要用于停止、重启服务
// @Author  yr  2024/12/3
// @Update  yr  2024/12/3
package services

import (
	"github.com/njtc406/emberengine/engine/pkg/core"
	"github.com/njtc406/emberengine/engine/pkg/event"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

// TODO 节点守护线程
// 如果之后使用plugin模式,这里就可以做热更

type daemon struct {
	core.Service
}

func (d *daemon) OnInit() error {
	var bus inf.INodeEventBus
	if ctx := d.GetNodeContext(); ctx != nil {
		bus = ctx.GetEventBus()
	}
	if bus == nil {
		d.Warn("daemon init: event bus is nil, skip global subscriptions")
		return nil
	}

	// TODO 注册服务事件
	bus.SubscribeGlobal(event.ServiceNew, d)
	bus.SubscribeGlobal(event.ServiceClose, d)

	//d.GetEventProcessor().RegEventReceiver(event.SysEventServiceUp, d.GetEventHandler(), d.serviceUp)
	//d.GetEventProcessor().RegEventReceiver(event.SysEventServiceDown, d.GetEventHandler(), d.serviceDown)
	//d.GetEventProcessor().RegEventReceiver(event.SysEventServiceReload, d.GetEventHandler(), d.serviceReload)
	return nil
}

func (d *daemon) OnStart() error {
	return nil
}

func (d *daemon) OnRelease() {

}

func (d *daemon) serviceUp(e inf.IEvent) {
	// TODO 启动服务
}

func (d *daemon) serviceDown(e inf.IEvent) {
	// TODO 关闭服务
}

func (d *daemon) serviceReload(e inf.IEvent) {
	// TODO 重启服务
	// 关闭服务流量
	// 获取服务内存数据
	// 启动新服务
	// 使用数据恢复服务
	// 关闭老服务
	// 启动新服务
	// 打开新服务流量
}
