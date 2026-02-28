// Package remote 提供远程配置源的实现。
//
// # OpenSpec
//
//   - 模块:     远程配置
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/config/remote
//   - 层级:     infrastructure
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// remote 包实现了从远程存储（如 etcd）读取配置的能力。
// 当节点启动时，可选择从远程配置中心拉取最新配置，
// 实现配置的集中管理与动态更新。
//
// # 核心类型
//
//   - EtcdRemote: 基于 etcd 的远程配置实现。
//
// # 依赖
//
// 外部:
//   - go.etcd.io/etcd/client/v3: etcd 客户端
package remote
