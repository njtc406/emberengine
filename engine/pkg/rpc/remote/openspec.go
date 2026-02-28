// Package remote 封装了 RPC 服务端的监听与远程消息接收。
//
// # OpenSpec
//
//   - 模块:     RPC 远程服务
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/rpc/remote
//   - 层级:     infrastructure
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// remote 包封装了 RPC 服务端的监听逻辑。Remote 组件在节点启动时
// 创建监听器（gRPC/NATS/RPCX），接收来自其他节点的 RPC 请求，
// 并通过 Handler 将请求派发到本地 Service 的 Mailbox 中。
//
// 支持多协议并存，通过子包 gr（gRPC）、nt（NATS）、rx（RPCX）
// 提供不同传输层实现。
//
// # 核心类型
//
//   - Remote: RPC 服务端主体，管理监听器生命周期。
//
// # 子包
//
//   - remote/gr:      gRPC 服务端实现
//   - remote/nt:      NATS 服务端实现
//   - remote/rx:      RPCX 服务端实现
//   - remote/handler: 远程消息处理器
//   - remote/pool:    服务端连接池
//
// # 依赖
//
// 内部:
//   - config:     RPC 服务配置
//   - interfaces: IRemoteServer 接口
//   - log:        日志
package remote
