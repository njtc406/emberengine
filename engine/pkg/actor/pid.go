// Package actor
// @Title  请填写文件名称（需要改）
// @Description  请填写文件描述（需要改）
// @Author  yr  2024/9/4 下午5:53
// @Update  yr  2024/9/4 下午5:53
package actor

import (
	"strconv"
	"sync/atomic"

	"github.com/njtc406/emberengine/engine/pkg/def"
)

func CreateInstanceId(partition int32, serviceName, serviceId, nodeUid string) string {
	// partition.serviceName.serviceId.nodeUid  集群唯一标识,在服务创建的时候生成
	return strconv.FormatInt(int64(partition), 10) + "." + serviceName + "." + serviceId + "." + nodeUid
}

func NewPID(address, nodeUid string, partition int32, serviceID, serviceType, serviceName string, version int64, rpcType string) *PID {
	return &PID{
		Address:     address,
		Name:        serviceName,
		ServiceType: serviceType,
		ServiceId:   serviceID,
		State:       0,
		Partition:   partition,
		Version:     version,
		RpcType:     rpcType,
		NodeUid:     nodeUid,
		ServiceUid:  CreateInstanceId(partition, serviceName, serviceID, nodeUid),
	}
}

// IsRetired 检查服务是否已退休（不再参与负载均衡）
func IsRetired(pid *PID) bool {
	if pid == nil {
		return true
	}
	return atomic.LoadInt32(&pid.State) == def.ServiceStatusRetired
}

// SetMaster 设置主从标志。
// 注意：IsMaster 是 protobuf 生成的 bool 字段，无法使用原子操作。
// 调用方必须保证 SetMaster 与 GetIsMaster 不会被并发调用，
// 通常应在 cluster watcher 单一 goroutine 中执行。
func (pid *PID) SetMaster(master bool) {
	pid.IsMaster = master
}

func (pid *PID) GetPrimarySecondaryKey() string {
	return pid.GetName() + "." + pid.GetServiceId() + "." + strconv.FormatInt(int64(pid.GetPartition()), 10)
}
