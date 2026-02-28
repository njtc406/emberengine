// Package services 提供 EmberEngine 的服务工厂与运行时管理。
//
// # OpenSpec
//
//   - 模块:     服务管理器
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/services
//   - 层级:     core
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// services 包管理所有业务逻辑服务的工厂注册、实例创建、初始化及
// 运行时依赖注入。ServiceManager 作为 Node 的一部分，持有当前节点
// 所有服务实例的注册表，并负责服务的有序启动和停止。
//
// 支持服务诊断（Diagnostics）和守护进程（Daemon）模式。
//
// # 核心类型
//
//   - ServiceManager: 服务管理器，管理服务工厂注册和实例生命周期。
//
// # 核心函数
//
//   - SetService:          注册服务工厂函数（名称 → 构造函数）。
//   - GetServiceFactory:   获取已注册的服务工厂。
//
// # 依赖
//
// 内部:
//   - config:     服务配置
//   - core:       Service 基类
//   - interfaces: IService 接口
//   - log:        日志
package services
