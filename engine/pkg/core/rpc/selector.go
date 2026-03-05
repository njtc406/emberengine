package rpc

import (
	"github.com/njtc406/emberengine/engine/pkg/actor"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

func WithPartition(partition int32) inf.SelectParamBuilder {
	return func(param *inf.SelectParam) {
		param.Partition = &partition
	}
}

func WithSid(serviceId string) inf.SelectParamBuilder {
	return func(param *inf.SelectParam) {
		param.ServiceId = &serviceId
	}
}

func WithName(serviceName string) inf.SelectParamBuilder {
	return func(param *inf.SelectParam) {
		param.ServiceName = &serviceName
	}
}

func WithType(serviceType string) inf.SelectParamBuilder {
	return func(param *inf.SelectParam) {
		param.ServiceType = &serviceType
	}
}

func WithIsSlaver(isSlaver bool) inf.SelectParamBuilder {
	return func(param *inf.SelectParam) {
		param.IsSlaver = isSlaver
	}
}

func (h *Handler) getRouter() inf.INodeRouter {
	if rt := h.GetService().GetRouter(); rt != nil {
		return rt
	}
	if ctx := h.GetService().GetNodeContext(); ctx != nil {
		return ctx.GetRouter()
	}
	return nil
}

// Select 选择相同Partition服务
func (h *Handler) Select(options ...inf.SelectParamBuilder) inf.IBus {
	// h.GetService().GetLogger().Debugf("pid:%s", h.GetPid().String())
	pid := h.GetPid()
	options = append(options, WithPartition(pid.GetPartition()))
	rt := h.getRouter()
	if rt == nil {
		return nil
	}
	return rt.Select(pid, options...)
}

// SelectByOpt 选择服务
func (h *Handler) SelectByOpt(options ...inf.SelectParamBuilder) inf.IBus {
	rt := h.getRouter()
	if rt == nil {
		return nil
	}
	return rt.Select(h.GetPid(), options...)
}

func (h *Handler) SelectByPid(receiver *actor.PID) inf.IBus {
	rt := h.getRouter()
	if rt == nil {
		return nil
	}
	return rt.SelectByPid(h.GetPid(), receiver)
}

// SelectByRule 根据自定义规则选择服务
func (h *Handler) SelectByRule(rule func(pid *actor.PID) bool) inf.IBus {
	rt := h.getRouter()
	if rt == nil {
		return nil
	}
	return rt.SelectByRule(h.GetPid(), rule)
}

func (h *Handler) SelectSlavers(options ...inf.SelectParamBuilder) inf.IBus {
	rt := h.getRouter()
	if rt == nil {
		return nil
	}
	return rt.Select(h.GetPid(), append(options, WithIsSlaver(true))...)
}

func (h *Handler) SelectByServiceUid(receiverServiceUid string) inf.IBus {
	rt := h.getRouter()
	if rt == nil {
		return nil
	}
	return rt.SelectByServiceUid(h.GetPid(), receiverServiceUid)
}
