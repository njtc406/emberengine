// Package leadership 实现了分布式主从选举与状态守护。
//
// # OpenSpec
//
//   - 模块:     主从选举
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/cluster/leadership
//   - 层级:     infrastructure
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// leadership 包提供 Primary/Secondary（Master/Slave）模式的选举守护能力。
// Guard 组件负责监控服务的主从状态变化，在角色切换时管理对应的
// 上下文生命周期（如启动/停止主节点专属逻辑）。
//
// # 核心类型
//
//   - Guard: 选举守护器，持有当前角色状态，在角色变更时触发回调。
//
// # 依赖
//
// 内部:
//   - interfaces: 角色状态接口
//   - log:        日志
package leadership
