// Package endpoints
// @Title  title
// @Description  desc
// @Author  yr  2024/12/3
// @Update  yr  2024/12/3
package endpoints

import (
	"github.com/njtc406/emberengine/engine/pkg/actor"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

func (em *EndpointManager) Select(sender *actor.PID, serverId int32, serviceId, serviceName string) inf.IBus {
	return em.repository.Select(sender, serverId, serviceId, serviceName)
}

func (em *EndpointManager) SelectByPid(sender, receiver *actor.PID) inf.IBus {
	return em.repository.SelectByPid(sender, receiver)
}

// SelectByRule 根据自定义规则选择服务
func (em *EndpointManager) SelectByRule(sender *actor.PID, rule func(pid *actor.PID) bool) inf.IBus {
	return em.repository.SelectByRule(sender, rule)
}

func (em *EndpointManager) SelectByServiceType(sender *actor.PID, serverId int32, serviceType, serviceName string) inf.IBus {
	return em.repository.SelectByServiceType(sender, serverId, serviceType, serviceName)
}

func (em *EndpointManager) SelectByFilterAndChoice(sender *actor.PID, filter func(pid *actor.PID) bool, choice func(pids []*actor.PID) []*actor.PID) inf.IBus {
	return em.repository.SelectByFilterAndChoice(sender, filter, choice)
}
