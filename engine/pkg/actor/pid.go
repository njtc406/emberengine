// Package actor
// @Title  请填写文件名称（需要改）
// @Description  请填写文件描述（需要改）
// @Author  yr  2024/9/4 下午5:53
// @Update  yr  2024/9/4 下午5:53
package actor

import (
	"fmt"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"sync/atomic"
)

const (
	RoleTypeMaster = "master"
	RoleTypeSlaver = "slaver"
)

func CreateInstanceId(serverId int32, serviceName, serviceId, nodeUid string) string {
	// serverId.serviceName.serviceId.nodeUid  集群唯一标识,在服务创建的时候生成
	return fmt.Sprintf("%d.%s.%s.%s", serverId, serviceName, serviceId, nodeUid)
}

func NewPID(address, nodeUid string, serverId int32, serviceID, serviceType, serviceName string, version int64, rpcType string) *PID {
	return &PID{
		Address:     address,
		Name:        serviceName,
		ServiceType: serviceType,
		ServiceId:   serviceID,
		State:       0,
		ServerId:    serverId,
		Version:     version,
		RpcType:     rpcType,
		NodeUid:     nodeUid,
		ServiceUid:  CreateInstanceId(serverId, serviceName, serviceID, nodeUid),
	}
}

func IsRetired(pid *PID) bool {
	return atomic.LoadInt32(&pid.State) == def.ServiceStatusRetired // TODO 状态需要重新定,需要区分服务状态和节点状态,节点状态的退休是不再参与负载均衡那种,服务退休就是不再接收新信息
}

func (pid *PID) SetMaster(master bool) {
	pid.IsMaster = master
}

func (pid *PID) GetRoleType() string {
	if pid.IsMaster {
		return RoleTypeMaster
	}
	return RoleTypeSlaver
}

func (pid *PID) GetServiceGroup() string {
	return fmt.Sprintf("%s.%s.%d", pid.GetName(), pid.GetServiceId(), pid.GetServerId())
}

func (e *Event) IncRef() {}
