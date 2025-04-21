package rpc

import (
	"github.com/njtc406/emberengine/engine/internal/message/msgbus"
	"github.com/njtc406/emberengine/engine/pkg/actor"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/router"
)

// Select 选择服务
func (h *Handler) Select(serverId int32, serviceId, serviceName string) inf.IBus {
	//log.SysLogger.Debugf("pid:%s", h.GetPid().String())
	return router.Select(h.GetPid(), serverId, serviceId, serviceName)
}

// SelectSameServer 选择相同服务器标识的服务
func (h *Handler) SelectSameServer(serviceId, serviceName string) inf.IBus {
	pid := h.GetPid()
	return router.Select(pid, pid.GetServerId(), serviceId, serviceName)
}

func (h *Handler) SelectByPid(receiver *actor.PID) inf.IBus {
	return router.SelectByPid(h.GetPid(), receiver)
}

// SelectByRule 根据自定义规则选择服务
func (h *Handler) SelectByRule(rule func(pid *actor.PID) bool) inf.IBus {
	return router.SelectByRule(h.GetPid(), rule)
}

// SelectByServiceType 根据类型选择服务
func (h *Handler) SelectByServiceType(serverId int32, serviceType, serviceName string, filters ...func(pid *actor.PID) bool) inf.IBus {
	list := router.SelectByServiceType(h.GetPid(), serverId, serviceType, serviceName)

	var returnList msgbus.MultiBus
	if len(filters) == 0 {
		return list
	}
	for _, filter := range filters {
		for _, bus := range list.(msgbus.MultiBus) {
			if filter(bus.(inf.IRpcDispatcher).GetPid()) {
				returnList = append(returnList, bus)
			}
		}
	}
	return returnList
}

func (h *Handler) SelectByFilterAndChoice(filter func(pid *actor.PID) bool, choice func(pids []*actor.PID) []*actor.PID) inf.IBus {
	return router.SelectByFilterAndChoice(h.GetPid(), filter, choice)
}

func (h *Handler) SelectSameServerByServiceType(serviceType, serviceName string, filters ...func(pid *actor.PID) bool) inf.IBus {
	list := router.SelectByServiceType(h.GetPid(), h.GetPid().GetServerId(), serviceType, serviceName)
	//log.SysLogger.Debugf("list len: %+v", list)
	var returnList msgbus.MultiBus
	if len(filters) == 0 {
		return list
	}
	for _, filter := range filters {
		for _, bus := range list.(msgbus.MultiBus) {
			if filter(bus.(inf.IRpcDispatcher).GetPid()) {
				returnList = append(returnList, bus)
			}
		}
	}
	return returnList
}
