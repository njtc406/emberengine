// Package def 定义了 EmberEngine 全局通用的常量、枚举和错误码。
//
// # OpenSpec
//
//   - 模块:     全局定义
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/def
//   - 层级:     foundation
//   - 状态:     stable
//   - 线程安全: yes（纯常量和类型定义，无可变状态）
//
// # 概述
//
// def 包是整个引擎的"词汇表"，集中定义了跨包共享的常量、枚举类型、
// 错误码和默认配置值。所有包均可引用 def 中的定义，避免魔法数字
// 和字符串散落在代码各处。
//
// # 核心常量/类型
//
//   - 服务状态: ServiceStatusInit, ServiceStatusRunning, ServiceStatusRetired 等。
//   - RPC 类型: RpcTypeGrpc, RpcTypeNats, RpcTypeRpcx。
//   - 事件类型: EventType 枚举及预定义事件。
//   - 优先级:   Priority 类型及预定义级别。
//   - 邮箱:     默认缓冲区大小、Worker 数等默认值。
//   - 错误码:   ErrServiceNotFound, ErrMailboxSuspended, ErrTimeout 等标准错误。
//   - 方法:     MethodPrefix 相关常量（api/rpc 前缀区分）。
//   - 选择器:   Selector 类型定义（Partition, ServiceType 等）。
//   - NATS 前缀: NatsDefaultGlobalPrefix 等消息总线前缀。
//
// # 依赖
//
// 无内部或外部依赖。def 是依赖树的叶子节点。
package def
