// Package actor
// @Title  Actor 进程标识符
// @Description  定义 PID 的创建、状态查询和主从标志管理
// @Author  yr  2024/9/4 下午5:53
// @Update  yr  2024/9/4 下午5:53
package actor

import (
	"strconv"
	"sync/atomic"

	"github.com/njtc406/emberengine/engine/pkg/def"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
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

// SetMaster 设置主从标志（并发安全）。
//
// 设计契约：
//   - 运行时唯一权威字段是 MasterFlag（atomic int32），所有读取走 IsMasterNode()；
//   - IsMaster bool 字段仅作为 protobuf 序列化的传输载体，运行时不读、不写；
//   - 因此 SetMaster 只更新 MasterFlag，避免对 IsMaster bool 字段的非原子写
//     与序列化路径形成 data race。
//
// 序列化出口（registry.RegisterService / MsgEnvelope.ToProtoMsg 等）在调用
// proto.Marshal / protojson.Marshal 之前需调用 PrepareForMarshal()，把
// MasterFlag 投影到 IsMaster 字段。
func (pid *PID) SetMaster(master bool) {
	var v int32
	if master {
		v = 1
	}
	atomic.StoreInt32(&pid.MasterFlag, v)
}

// IsMasterNode 原子化读取主从标志（并发安全）。
// 替代 protobuf 生成的 GetIsMaster()，用于所有运行时读取场景。
func (pid *PID) IsMasterNode() bool {
	return atomic.LoadInt32(&pid.MasterFlag) == 1
}

// SyncMasterFlag 将 proto 反序列化的 IsMaster 同步到 MasterFlag。
// 应在 PID 从网络/存储反序列化后立即调用。
func (pid *PID) SyncMasterFlag() {
	var v int32
	if pid.IsMaster {
		v = 1
	}
	atomic.StoreInt32(&pid.MasterFlag, v)
}

// PrepareForMarshal 在序列化出口（RPC ToProtoMsg / etcd RegisterService 等）
// 调用前，将运行时 MasterFlag 投影到 protobuf 字段 IsMaster 上。
//
// 注意：写 IsMaster 是非原子操作，调用方必须保证：
//   - 同一 PID 同时只有一个 goroutine 在执行 PrepareForMarshal + Marshal；
//   - 或者调用方在调用前已对 PID 做了独占复制（proto.Clone）。
//
// 在 ToProtoMsg / RegisterService 中，每次调用都构建独立的 wire-message，
// PID 指针虽共享，但写入值由 MasterFlag 派生，不会破坏运行时状态。
func (pid *PID) PrepareForMarshal() {
	pid.IsMaster = pid.IsMasterNode()
}

// MarshalPID 是 PID 序列化的唯一推荐出口（proto 二进制格式）。
// 内部先对 PID 做 proto.Clone 以获得独立副本，再调用 PrepareForMarshal，
// 消除跨 goroutine 并发序列化时对 IsMaster 字段的 data race 风险。
//
// 所有新增的序列化路径应使用此函数，而非直接调用 PrepareForMarshal + proto.Marshal。
// CI 可通过 grep 禁止 "pid.PrepareForMarshal()" 出现在新代码中。
func MarshalPID(pid *PID) ([]byte, error) {
	if pid == nil {
		return nil, nil
	}
	clone := proto.Clone(pid).(*PID)
	clone.PrepareForMarshal()
	return proto.Marshal(clone)
}

// MarshalPIDJSON 是 PID 序列化的唯一推荐出口（protojson 文本格式）。
// 与 MarshalPID 相同的安全保证，用于 etcd 注册等需要 JSON 格式的场景。
func MarshalPIDJSON(pid *PID) ([]byte, error) {
	if pid == nil {
		return nil, nil
	}
	clone := proto.Clone(pid).(*PID)
	clone.PrepareForMarshal()
	return protojson.Marshal(clone)
}

// SnapshotForWire 返回 PID 的独立 wire 副本：
//   - 内部 proto.Clone 一份独立 PID；
//   - 在副本上调用 PrepareForMarshal，把运行时 MasterFlag 投影到 IsMaster；
//   - 返回的副本可以安全地嵌入到任意父级 proto Message 中作为子消息字段。
//
// 用途：当 PID 不是单独被 Marshal，而是作为父级 Message（例如 wire 层的
// Message.SenderPid / Message.ReceiverPid）的子字段被一并 Marshal 时，
// 直接对原 PID 调用 PrepareForMarshal 会与并发 RPC 形成对 IsMaster 字段的
// 非原子写竞争。改用本函数获取独立 wire 副本，赋值给父级字段，可以彻底
// 消除该并发写破口。
//
// 调用代价：一次 proto.Clone（反射拷贝小消息），相比 RPC 序列化整体开销可忽略。
func SnapshotForWire(pid *PID) *PID {
	if pid == nil {
		return nil
	}
	clone := proto.Clone(pid).(*PID)
	clone.PrepareForMarshal()
	return clone
}

func (pid *PID) GetPrimarySecondaryKey() string {
	return pid.GetName() + "." + pid.GetServiceId() + "." + strconv.FormatInt(int64(pid.GetPartition()), 10)
}
