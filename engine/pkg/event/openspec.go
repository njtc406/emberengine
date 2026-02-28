// Package event 实现了 EmberEngine 的分布式事件总线系统。
//
// # OpenSpec
//
//   - 模块:     事件总线
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/event
//   - 层级:     infrastructure
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// event 包提供三种作用域的事件发布/订阅机制：
//   - Global（全局）:   跨集群所有节点的事件广播，基于 NATS。
//   - Server（节点内）: 单节点内所有服务间的事件分发。
//   - Specific（特定）: 向指定目标对象发送的定向事件。
//
// 支持限流（Throttle）、批处理（Batch）、事件类型过滤等高级特性。
// 本地事件通过 Processor/Trigger 机制分发，全局事件通过 NATS
// 消息队列传递。
//
// # 核心类型
//
//   - Bus:        事件总线实例，管理 NATS 连接和订阅。
//   - Processor:  本地事件处理器，维护 Handler 注册表。
//   - Trigger:    本地事件触发器。
//   - Handler:    事件处理器接口实现。
//   - EventType:  事件类型注册与管理。
//   - Category:   事件分类枚举。
//   - Throttle:   限流器，控制事件发布速率。
//
// # 核心函数
//
//   - NewEventBus:         创建事件总线实例。
//   - Publish/Subscribe:   发布/订阅事件。
//   - RegisterGlobHandler: 泛型辅助函数，注册全局事件处理器。
//
// # 依赖
//
// 内部:
//   - config:     事件总线配置
//   - def:        事件类型常量
//   - interfaces: IEventProcessor, IEventHandler 等
//   - log:        日志
//
// 外部:
//   - github.com/nats-io/nats.go: NATS 客户端
package event
