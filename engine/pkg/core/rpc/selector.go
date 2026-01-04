package rpc

import (
	"github.com/njtc406/emberengine/engine/pkg/actor"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"github.com/njtc406/emberengine/engine/pkg/router"
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

// Select 选择相同Partition服务
func (h *Handler) Select(options ...inf.SelectParamBuilder) inf.IBus {
	//log.SysLogger.Debugf("pid:%s", h.GetPid().String())
	pid := h.GetPid()
	options = append(options, WithPartition(pid.GetPartition()))
	return router.Select(pid, options...)
}

// SelectByOpt 选择服务
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

func (h *Handler) SelectSlavers(options ...inf.SelectParamBuilder) inf.IBus {
	return router.Select(h.GetPid(), append(options, WithIsSlaver(true))...)
}

func (h *Handler) SelectByServiceUid(receiverServiceUid string) inf.IBus {
	return router.SelectByServiceUid(h.GetPid(), receiverServiceUid)
}
