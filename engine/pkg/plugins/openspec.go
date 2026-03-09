// Package plugins 提供 EmberEngine 的插件管理系统。
//
// # OpenSpec
//
//   - 模块:     插件系统
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/plugins
//   - 层级:     extension
//   - 状态:     experimental
//   - 线程安全: partial
//
// # 概述
//
// plugins 包提供动态插件的注册与加载能力。通过 PluginManager
// 管理插件的注册表，支持按名称注册插件路径并批量加载。
// 适用于引擎功能的热扩展场景。
//
// # 核心类型
//
//   - PluginInfo:    插件元信息（名称、路径等）。
//   - PluginManager: 插件管理器，提供 Register/LoadAll 操作。
//
// # 核心函数
//
//   - NewPluginManager: 创建插件管理器实例。
//   - Register:         注册插件（名称 + 路径）。
//   - LoadAll:          批量加载所有已注册插件。
//
// # 依赖
//
// 内部:
//   - 无
package plugins
