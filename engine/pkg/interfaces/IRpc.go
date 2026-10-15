// Package interfaces
// @Title  title
// @Description  desc
// @Author  pc  2024/11/5
// @Update  pc  2024/11/5
package interfaces

import (
	"context"

	"github.com/njtc406/emberengine/engine/pkg/actor"
)

// IRpcHandler 完整的 RPC 处理器接口（框架内部使用）
// 组合了高层调用能力和低层消息处理能力
type IRpcHandler interface {
	IRpcInvoker
	IRpcProcessor
}

// IRpcInvoker 高层 RPC 调用接口（用户使用）
// 提供服务选择和方法查询能力
type IRpcInvoker interface {
	IRpcSelector
	GetMethods() []string // 获取所有对外接口名称
}

// IRpcProcessor 低层 RPC 消息处理接口（框架内部使用）
// 处理原始的 RPC 请求和响应消息
type IRpcProcessor interface {
	// HandleRequest 处理请求消息
	HandleRequest(ctx context.Context, msg IEnvelope) error

	// HandleResponse 处理回复消息
	HandleResponse(ctx context.Context, msg IEnvelope) error
}

type IRpcSelector interface {
	// 选择相同Partition的服务,如果需要选择其他Partition的服务,使用下面的SelectByOpt
	Select(options ...SelectParamBuilder) IBus

	SelectByOpt(options ...SelectParamBuilder) IBus

	SelectByPid(receiver *actor.PID) IBus

	RouteByPid(receiver *actor.PID) IBus

	SelectByServiceUid(receiverServiceUid string) IBus

	// SelectByRule 根据自定义规则选择服务
	SelectByRule(rule func(pid *actor.PID) bool) IBus

	// SelectSlavers 选择从服务
	SelectSlavers(options ...SelectParamBuilder) IBus

	//SelectWithFilter(filter func(pid *actor.PID) bool, options ...SelectParamBuilder) IBus // 这个目前有问题，请勿使用
}

type SelectParamBuilder func(param *SelectParam)

type SelectParam struct {
	Partition   *int32
	ServiceId   *string
	ServiceName *string
	ServiceType *string
	IsSlaver    bool
}

type IRpcCallback interface {
	DoCallback(ctx context.Context, data interface{}, err error, params ...interface{})
}
