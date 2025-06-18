// Package interfaces
// @Title  服务选择器
// @Description  根据条件选择服务
// @Author  yr  2024/11/7
// @Update  yr  2024/11/7
package interfaces

import "github.com/njtc406/emberengine/engine/pkg/actor"

type SelectOption struct {
	ServiceUid  *string
	ServerId    *int32
	ServiceName *string
	ServiceId   *string
	ServiceType *string
	IsMaster    *bool
	NodeUid     *string
	Version     *int64
	State       *int32
}

type SelectorOption func(opt *SelectOption)

type ISelector interface {
	SelectByUid(sender *actor.PID, serviceUid string) IBus

	Select(sender *actor.PID, options ...SelectorOption) IBus

	// TODO 这个接口待定,因为可以使用上面的select实现
	// 只用于搜索从服务
	SelectSlavers(sender *actor.PID, serverId int32, serviceName, serviceId string, options ...SelectorOption) IBus

	// Select 选择服务
	//Select(sender *actor.PID, serverId int32, serviceId, serviceName string) IBus
	//
	//// SelectByRule 根据自定义规则选择服务
	//SelectByRule(sender *actor.PID, rule func(pid *actor.PID) bool) IBus
	//
	//SelectByPid(sender, receiver *actor.PID) IBus
	//
	//SelectByServiceType(sender *actor.PID, serverId int32, serviceType, serviceName string) IBus
	//
	//SelectByFilterAndChoice(sender *actor.PID, filter func(pid *actor.PID) bool, choice func(pids []*actor.PID) []*actor.PID) IBus
}
