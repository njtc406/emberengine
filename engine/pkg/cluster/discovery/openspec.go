// Package discovery 定义了服务发现的抽象接口与注册中心。
//
// # OpenSpec
//
//   - 模块:     服务发现
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/cluster/discovery
//   - 层级:     infrastructure
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// discovery 包提供服务发现的核心抽象。通过 IDiscovery 接口定义了
// 注册、注销、监听等标准语义，具体实现（如 etcd）位于子包中。
// Registry 负责管理发现实现的注册与获取。
//
// # 核心类型
//
//   - IDiscovery: 服务发现接口（定义于 interfaces.go），规定 Init/Start/Close 契约。
//   - Registry:   发现实现的注册中心，支持按名称注册和获取具体实现。
//
// # 依赖
//
// 内部:
//   - interfaces: IDiscovery 接口复用
package discovery
