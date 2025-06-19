package rpc

import (
	"github.com/njtc406/emberengine/engine/internal/message/msgbus"
	"github.com/njtc406/emberengine/engine/pkg/actor"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/router"
)

func WithServerId(serverId int32) inf.SelectParamBuilder {
	return func(param *inf.SelectParam) {
		param.ServerId = &serverId
	}
}

func WithServiceId(serviceId string) inf.SelectParamBuilder {
	return func(param *inf.SelectParam) {
		param.ServiceId = &serviceId
	}
}

func WithServiceName(serviceName string) inf.SelectParamBuilder {
	return func(param *inf.SelectParam) {
		param.ServiceName = &serviceName
	}
}

func WithServiceType(serviceType string) inf.SelectParamBuilder {
	return func(param *inf.SelectParam) {
		param.ServiceType = &serviceType
	}
}

// Select 选择服务
func (h *Handler) Select(options ...inf.SelectParamBuilder) inf.IBus {
	//log.SysLogger.Debugf("pid:%s", h.GetPid().String())
	pid := h.GetPid()
	options = append(options, WithServerId(pid.GetServerId()))
	return router.Select(pid, options...)
}

// SelectSameServer 选择相同服务器标识的服务
func (h *Handler) SelectByOpt(options ...inf.SelectParamBuilder) inf.IBus {
	return router.Select(h.GetPid(), options...)
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

func (h *Handler) SelectSlavers(serverId int32, serviceName, serviceId string) inf.IBus {
	return router.SelectSlavers(h.GetPid(), serverId, serviceName, serviceId)
}
