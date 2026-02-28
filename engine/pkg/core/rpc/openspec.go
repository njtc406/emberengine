// Package rpc 提供 core 层的 RPC 方法管理与路由选择。
//
// # OpenSpec
//
//   - 模块:     核心 RPC 管理
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/core/rpc
//   - 层级:     core
//   - 状态:     stable
//   - 线程安全: partial（方法注册在初始化阶段完成，运行时只读）
//
// # 概述
//
// core/rpc 包负责管理模块导出的 RPC 方法表（MethodMgr）和
// 方法前缀路由匹配。每个 Module 通过 MethodMgr 注册可被远程
// 调用的方法，Handler 负责将收到的 RPC 请求分发到正确的方法。
//
// Selector 实现了 IRpcSelector，提供基于服务名、分区、自定义规则
// 等多种路由选择策略。Prefix 处理方法名前缀匹配逻辑（api/rpc 前缀区分）。
//
// # 核心类型
//
//   - MethodMgr: 方法管理器，实现 interfaces.IMethodMgr，提供
//     AddMethodFunc/GetMethodFunc/RemoveMethods 操作。
//   - Handler:   RPC 请求处理器，实现 interfaces.IRpcProcessor。
//   - Selector:  RPC 选择器，实现 interfaces.IRpcSelector。
//
// # 依赖
//
// 内部:
//   - interfaces: IRpcHandler, IRpcSelector, IMethodMgr 等
//   - router:     路由选择
//   - log:        日志
package rpc
