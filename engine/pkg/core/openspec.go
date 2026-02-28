// Package core 是 EmberEngine 的核心逻辑层。
//
// # OpenSpec
//
//   - 模块:     核心引擎
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/core
//   - 层级:     core
//   - 状态:     stable
//   - 线程安全: partial（Service/Module 通过 Mailbox 保证消息顺序处理）
//
// # 概述
//
// core 包定义了引擎的两大基石：Service（服务）和 Module（模块）。
// Service 是可独立部署的运行单元，拥有自己的 Mailbox、事件处理器和
// RPC 方法表。Module 是 Service 内的逻辑拆分单元，支持树状层级结构
// 和独立的事件/RPC 处理。
//
// 消息处理流程：外部消息 → Mailbox → Handler → Module 方法。
// 通过 Mailbox 的 Single-Writer 模型保证了同一 Service 内的消息
// 有序执行，避免了锁竞争。
//
// # 核心类型
//
//   - Service: 服务容器基类，实现 interfaces.IService 和 IMessageInvoker。
//     持有 PID、Mailbox、EventProcessor、NodeContext 等关键组件。
//     所有业务服务均嵌入此结构体。
//   - Module:  模块基类，实现 interfaces.IModule。支持父子层级、
//     事件绑定、RPC 方法注册。是逻辑拆分的最细粒度单元。
//
// # 核心函数
//
//   - Service.Init:          初始化服务，注入运行时依赖。
//   - Service.SetNodeContext: 设置节点上下文。
//   - Module.AddModule:      挂载子模块。
//   - Module.ReleaseModule:  释放指定子模块。
//
// # 子包
//
//   - core/rpc:       RPC 方法管理器和选择器
//   - core/component: 组件基类（如有）
//
// # 依赖
//
// 内部:
//   - actor:      PID 定义
//   - mailbox:    消息邮箱
//   - cluster:    集群信息
//   - config:     服务配置
//   - def:        常量和错误码
//   - event:      事件发布/订阅
//   - interfaces: 全部核心接口
//   - log:        日志
//   - profiler:   性能分析
//   - router:     路由选择
//   - utils:      并发工具等
package core
