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

type Router struct {
	endpoints *endpoints.EndpointManager
}

func NewRouter(em *endpoints.EndpointManager) *Router {
	return &Router{endpoints: em}
}

func (r *Router) endpointManager() *endpoints.EndpointManager {
	if r != nil && r.endpoints != nil {
		return r.endpoints
	}
	return nil
}

func (r *Router) Select(sender *actor.PID, options ...inf.SelectParamBuilder) inf.IBus {
	em := r.endpointManager()
	if em == nil {
		return nil
	}
	return em.GetRepository().Select(sender, options...)
}

func (r *Router) SelectByPid(sender, receiver *actor.PID) inf.IBus {
	em := r.endpointManager()
	if em == nil {
		return nil
	}
	return em.GetRepository().SelectByPid(sender, receiver)
}

func (r *Router) SelectByServiceUid(sender *actor.PID, receiverServiceUid string) inf.IBus {
	em := r.endpointManager()
	if em == nil {
		return nil
	}
	return em.GetRepository().SelectBySvcUid(sender, receiverServiceUid)
}

// SelectByRule 根据自定义规则选择服务
func (r *Router) SelectByRule(sender *actor.PID, rule func(pid *actor.PID) bool) inf.IBus {
	em := r.endpointManager()
	if em == nil {
		return nil
	}
	return em.GetRepository().SelectByRule(sender, rule)
}

func (r *Router) SelectByServiceType(sender *actor.PID, partition int32, serviceType, serviceName string) inf.IBus {
	em := r.endpointManager()
	if em == nil {
		return nil
	}
	return em.GetRepository().SelectByServiceType(sender, partition, serviceType, serviceName)
}

func (r *Router) SelectByFilterAndChoice(sender *actor.PID, filter func(pid *actor.PID) bool, choice func(pids []*actor.PID) []*actor.PID) inf.IBus {
	em := r.endpointManager()
	if em == nil {
		return nil
	}
	return em.GetRepository().SelectByFilterAndChoice(sender, filter, choice)
}
