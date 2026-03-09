// Package codec 提供消息编解码器及缓存池。
//
// # OpenSpec
//
//   - 模块:     编解码器
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/utils/codec
//   - 层级:     utility
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// codec 包提供多种消息编解码器实现（Proto、JSON），以及编解码
// 缓冲区的池化管理。Coder 接口定义了统一的 Marshal/Unmarshal 语义。
//
// # 核心类型
//
//   - Coder:      编解码器接口。
//   - ProtoCodec: Protobuf 编解码实现。
//   - JsonCodec:  JSON 编解码实现。
//   - Pool:       编解码缓冲区对象池。
//
// # 依赖
//
// 外部:
//   - google.golang.org/protobuf: Protobuf 运行时
package codec
