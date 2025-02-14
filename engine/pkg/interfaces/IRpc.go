// Package interfaces
// @Title  title
// @Description  desc
// @Author  pc  2024/11/5
// @Update  pc  2024/11/5
package interfaces

import (
	"github.com/njtc406/emberengine/engine/pkg/actor"
)

type IRpcHandler interface {
	IRpcSelector
	HandleRequest(msg IEnvelope)  // 处理请求
	HandleResponse(msg IEnvelope) // 处理回复

	GetMethods() []string // 获取所有对外接口名称
}

type IRpcSelector interface {
	Select(serverId int32, serviceId, serviceName string) IBus

	SelectSameServer(serviceId, serviceName string) IBus

	SelectByPid(receiver *actor.PID) IBus

	// SelectByRule 根据自定义规则选择服务
	SelectByRule(rule func(pid *actor.PID) bool) IBus

	// SelectByServiceType 根据服务类型选择服务
	SelectByServiceType(serverId int32, serviceType, serviceName string, filters ...func(pid *actor.PID) bool) IBus

	SelectSameServerByServiceType(serviceType, serviceName string, filters ...func(pid *actor.PID) bool) IBus
}

type IRpcChannel interface {
	PushRequest(req IEnvelope) error
}

type IHttpChannel interface {
	PushHttpEvent(e interface{}) error
}
