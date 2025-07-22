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
	// 选择相同serverId的服务,如果需要选择其他serverId的服务,使用下面的SelectByOpt
	Select(options ...SelectParamBuilder) IBus

	SelectByOpt(options ...SelectParamBuilder) IBus

	SelectByPid(receiver *actor.PID) IBus

	SelectByServiceUid(receiverServiceUid string) IBus

	SelectSlavers(options ...SelectParamBuilder) IBus

	// SelectByRule 根据自定义规则选择服务
	SelectByRule(rule func(pid *actor.PID) bool) IBus

	//SelectWithFilter(filter func(pid *actor.PID) bool, options ...SelectParamBuilder) IBus // 这个目前有问题，请勿使用
}

type IRpcChannel interface {
	PushRequest(req IEnvelope) error
}

type IHttpChannel interface {
	PushHttpEvent(e interface{}) error
}

type SelectParamBuilder func(param *SelectParam)

type SelectParam struct {
	ServerId    *int32
	ServiceId   *string
	ServiceName *string
	ServiceType *string
}
