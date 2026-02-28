// Package serializer 提供消息序列化器。
//
// # OpenSpec
//
//   - 模块:     序列化器
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/utils/serializer
//   - 层级:     utility
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// serializer 包提供多种消息序列化/反序列化实现，用于 RPC
// 消息的编解码传输。支持 Protobuf 和 JSON 两种格式。
//
// # 核心类型
//
//   - Serializer:      序列化器接口。
//   - ProtoSerializer: Protobuf 序列化实现。
//   - JsonSerializer:  JSON 序列化实现。
//   - Messages:        消息注册表，管理消息类型到序列化器的映射。
//
// # 依赖
//
// 外部:
//   - google.golang.org/protobuf: Protobuf 运行时
package serializer
