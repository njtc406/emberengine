// Package actor
// @Title  请填写文件名称（需要改）
// @Description  请填写文件描述（需要改）
// @Author  yr  2024/9/4 下午5:53
// @Update  yr  2024/9/4 下午5:53
package actor

import (
	"fmt"
	"github.com/njtc406/emberengine/engine/def"
	"sync/atomic"
)

func CreateServiceUid(serverId int32, serviceName, serviceId string) string {
	return fmt.Sprintf("%d:%s@%s", serverId, serviceName, serviceId)
}

func NewPID(address, nodeUid string, serverId int32, serviceID, serviceType, serviceName string, version int64, rpcType string) *PID {
	serviceUid := CreateServiceUid(serverId, serviceName, serviceID)
	return &PID{
		Address:     address,
		Name:        serviceName,
		ServiceType: serviceType,
		ServiceUid:  serviceUid,
		State:       0,
		ServerId:    serverId,
		Version:     version,
		RpcType:     rpcType,
		NodeUid:     nodeUid,
	}
}

func IsRetired(pid *PID) bool {
	return atomic.LoadInt32(&pid.State) == def.ServiceStatusRetired
}
