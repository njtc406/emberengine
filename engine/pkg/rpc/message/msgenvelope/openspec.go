// Package msgenvelope 提供 RPC 消息信封的封装与管理。
//
// # OpenSpec
//
//   - 模块:     消息信封
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/rpc/message/msgenvelope
//   - 层级:     infrastructure
//   - 状态:     stable
//   - 线程安全: partial（单个 Envelope 非线程安全，通过池化使用）
//
// # 概述
//
// msgenvelope 包实现了 RPC 通信的消息信封机制。信封（Envelope）
// 由元数据（Meta）和数据部（Data）组成，封装了一次 RPC 调用的
// 完整上下文：发送方/接收方 PID、请求ID、超时、方法名、
// 请求/响应数据和错误信息。
//
// 信封对象通过池化复用，减少高频调用下的 GC 压力。
//
// # 核心类型
//
//   - Envelope: 实现 interfaces.IEnvelope，RPC 消息的完整载体。
//   - Meta:     实现 interfaces.IEnvelopeMeta，元数据部分
//     （发送方/接收方 PID、ReqId、Deadline、回调等）。
//   - Data:     实现 interfaces.IEnvelopeData，数据部分
//     （Method、Request、Response、Error）。
//   - Message:  消息辅助类型。
//
// # 依赖
//
// 内部:
//   - actor:      PID 类型
//   - interfaces: IEnvelope, IEnvelopeMeta, IEnvelopeData
//   - log:        日志（调试模式）
package msgenvelope
