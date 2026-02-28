// Package etcd 提供基于 etcd 的服务发现实现。
//
// # OpenSpec
//
//   - 模块:     etcd 服务发现
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/cluster/discovery/etcd
//   - 层级:     infrastructure
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// etcd 包实现了 IDiscovery 接口，基于 etcd v3 提供服务注册、
// 注销和监听能力。包含客户端连接管理、租约续期、键监听（Watcher）
// 和主从选举（Election）等核心组件。
//
// # 核心类型
//
//   - Discovery: IDiscovery 的 etcd 实现，管理服务注册生命周期。
//   - Client:    etcd 客户端封装，处理连接和重连。
//   - Lease:     租约管理器，维护注册键的 TTL。
//   - Watcher:   键前缀监听器，实时感知服务上下线。
//   - Election:  主从选举实现，基于 etcd 的分布式锁。
//
// # 依赖
//
// 内部:
//   - config:     ETCDConf 配置
//   - interfaces: IDiscovery 接口
//   - log:        日志
//
// 外部:
//   - go.etcd.io/etcd/client/v3: etcd 客户端
package etcd
