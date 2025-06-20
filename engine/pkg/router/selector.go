// Package router
// @Title  路由选择器
// @Description  desc
// @Author  yr  2025/4/10
// @Update  yr  2025/4/10
package router

import (
	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/cluster/endpoints"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

func Select(sender *actor.PID, options ...inf.SelectParamBuilder) inf.IBus {
	return endpoints.GetEndpointManager().GetRepository().Select(sender, options...)
}

func SelectByPid(sender, receiver *actor.PID) inf.IBus {
	return endpoints.GetEndpointManager().GetRepository().SelectByPid(sender, receiver)
}

func SelectByServiceUid(sender *actor.PID, receiverServiceUid string) inf.IBus {
	return endpoints.GetEndpointManager().GetRepository().SelectBySvcUid(sender, receiverServiceUid)
}

// SelectByRule 根据自定义规则选择服务
func SelectByRule(sender *actor.PID, rule func(pid *actor.PID) bool) inf.IBus {
	return endpoints.GetEndpointManager().GetRepository().SelectByRule(sender, rule)
}

func SelectByServiceType(sender *actor.PID, serverId int32, serviceType, serviceName string) inf.IBus {
	return endpoints.GetEndpointManager().GetRepository().SelectByServiceType(sender, serverId, serviceType, serviceName)
}

func SelectByFilterAndChoice(sender *actor.PID, filter func(pid *actor.PID) bool, choice func(pids []*actor.PID) []*actor.PID) inf.IBus {
	return endpoints.GetEndpointManager().GetRepository().SelectByFilterAndChoice(sender, filter, choice)
}

func SelectSlavers(sender *actor.PID, options ...inf.SelectParamBuilder) inf.IBus {
	return endpoints.GetEndpointManager().GetRepository().SelectSlavers(sender, options...)
}
