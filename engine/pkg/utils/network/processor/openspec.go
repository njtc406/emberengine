// Package processor 提供网络消息的编解码处理器。
//
// # OpenSpec
//
//   - 模块:     消息处理器
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/utils/network/processor
//   - 层级:     utility
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// processor 包为 network 层提供消息的编解码处理能力。
// 定义了 Processor 接口和多种实现：
//   - JsonProcessor:   JSON 格式消息处理。
//   - PbProcessor:     Protobuf 格式消息处理。
//   - PbRawProcessor:  原始 Protobuf 消息处理。
//
// # 核心类型
//
//   - Processor:       消息处理器接口。
//   - JsonProcessor:   JSON 实现。
//   - PbProcessor:     Protobuf 实现。
//   - PbRawProcessor:  原始字节 Protobuf 实现。
//
// # 依赖
//
// 外部:
//   - google.golang.org/protobuf: Protobuf 运行时
package processor
