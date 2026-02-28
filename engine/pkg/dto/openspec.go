// Package dto 定义了 EmberEngine 的数据传输对象。
//
// # OpenSpec
//
//   - 模块:     数据传输对象
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/dto
//   - 层级:     foundation
//   - 状态:     stable
//   - 线程安全: partial（值对象本身线程安全，构建过程非并发）
//
// # 概述
//
// dto 包提供在服务间传递的数据载体定义。主要用于 RPC 调用参数封装、
// 请求头（Headers）处理和异步回调定义。这些类型作为方法签名的一部分，
// 确保调用语义的清晰和类型安全。
//
// # 核心类型
//
//   - BusOption:       消息总线调用的配置选项，用于 Call/Send 时附加超时、
//     Headers、选择策略等参数。
//   - Headers:         跨服务传递的元数据头信息 map。
//   - CompletionFunc:  异步调用成功后的回调函数类型。
//   - Concurrent:      并发上下文封装。
//   - DataDef:         通用数据定义。
//
// # 依赖
//
// 内部:
//   - def:        常量引用
//   - interfaces: 回调及选择器接口
package dto
