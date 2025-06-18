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

func CreateServiceUid(serverId int32, serviceName, serviceId, nodeUid string) string {
	return fmt.Sprintf("%s@%d:%s.%s", serviceName, serverId, serviceId, nodeUid)
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
		ServiceUid:  CreateServiceUid(serverId, serviceName, serviceID, nodeUid),
	}
}

func IsRetired(pid *PID) bool {
	return atomic.LoadInt32(&pid.State) == def.ServiceStatusRetired
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
