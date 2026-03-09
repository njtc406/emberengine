// Package actor 定义了 EmberEngine 中 Actor 实体的标识与消息协议。
//
// # OpenSpec
//
//   - 模块:     Actor 标识
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/actor
//   - 层级:     foundation
//   - 状态:     stable
//   - 线程安全: yes（PID 创建后为只读值对象）
//
// # 概述
//
// actor 包是整个引擎中最基础的标识层，定义了进程标识符 PID（Process ID）
// 和事件协议 Event。PID 是 Actor 在集群中的唯一标识，包含地址、服务名、
// 分区、版本及 NodeUID 等元数据。所有跨服务通信均通过 PID 进行寻址。
//
// 本包的类型由 protobuf 定义生成（actor.proto），确保了跨语言兼容性
// 和高效序列化。
//
// # 核心类型
//
//   - PID:              Actor 的进程标识符，包含 Address、Name、ServiceType、
//     ServiceId、Partition、Version、RpcType、NodeUid、ServiceUid 等字段。
//     ServiceUid 是全集群唯一的复合标识。
//   - Event:            Actor 间通信的标准事件格式（Proto 生成），携带 EventType 和数据负载。
//
// # 核心函数
//
//   - NewPID:           根据地址、节点UID、分区、服务信息等创建新的 PID 实例。
//   - CreateInstanceId: 根据 partition + serviceName + serviceId + nodeUid 生成集群唯一标识。
//   - IsRetired:        检查 PID 对应的服务是否已进入退休状态。
//
// # 依赖
//
// 内部:
//   - def: 引用 EventType 等基础常量定义
//
// 外部:
//   - google.golang.org/protobuf: protobuf 运行时
package actor
