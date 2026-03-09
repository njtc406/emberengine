// Package pool 提供 RPC 服务端的连接池管理。
//
// # OpenSpec
//
//   - 模块:     服务端连接池
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/rpc/remote/pool
//   - 层级:     infrastructure
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// pool 包为 RPC 服务端提供连接级别的池化管理，复用已建立的连接
// 以减少握手开销和资源消耗。
//
// # 核心类型
//
//   - Pool: 连接池，提供 Get/Put/Close 操作。
//
// # 依赖
//
// 内部:
//   - log: 日志
package pool
