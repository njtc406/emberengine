// Package interfaces
// @Title  服务选择器
// @Description  根据条件选择服务
// @Author  yr  2024/11/7
// @Update  yr  2024/11/7
package interfaces

import "github.com/njtc406/emberengine/engine/pkg/actor"

// 每条筛选条件
type PIDPredicate func(pid *actor.PID) bool

// 所有筛选条件的容器
type FilterCriteria struct {
	Predicates []PIDPredicate
}

// 用于组合一组筛选条件的构建器函数
type CriteriaBuilder func(*FilterCriteria)

// 单个构建器（语义清晰）
type CriteriaFunc = CriteriaBuilder

type ISelector interface {
	//FindOne(sender *actor.PID, serviceUid string) IBus

	//Select(sender *actor.PID, options ...CriteriaBuilder) IBus

	// TODO 这个接口待定,因为可以使用上面的select实现
	// 只用于搜索从服务
	SelectSlavers(sender *actor.PID, serverId int32, serviceName, serviceId string) IBus

	//Select 选择服务
	Select(sender *actor.PID, serverId int32, serviceId, serviceName string) IBus

	// SelectByRule 根据自定义规则选择服务
	SelectByRule(sender *actor.PID, rule func(pid *actor.PID) bool) IBus

	SelectByPid(sender, receiver *actor.PID) IBus

	SelectByServiceType(sender *actor.PID, serverId int32, serviceType, serviceName string) IBus

	SelectByFilterAndChoice(sender *actor.PID, filter func(pid *actor.PID) bool, choice func(pids []*actor.PID) []*actor.PID) IBus
}
