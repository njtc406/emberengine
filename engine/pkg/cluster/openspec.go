// Package cluster 提供 EmberEngine 的集群管理能力。
//
// # OpenSpec
//
//   - 模块:     集群管理
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/cluster
//   - 层级:     infrastructure
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// cluster 包是多节点协同的中枢，负责协调服务发现（Discovery）、
// 端点管理（Endpoints）和跨节点事件分发。它将 discovery、endpoints
// 等子模块统一封装，为上层提供一致的集群操作界面。
//
// Cluster 支持单机模式和集群模式两种运行方式。集群模式下通过 etcd
// 实现服务注册与发现，端点管理器维护所有已知服务的 PID 列表。
//
// # 核心类型
//
//   - Cluster: 集群管理器主体，持有 IDiscovery 和 EndpointManager，
//     提供 Init/Start/Close 生命周期方法和 PushEvent 集群事件推送。
//
// # 核心函数
//
//   - NewCluster:     创建 Cluster 实例。
//   - Init:           初始化发现服务和端点管理器。
//   - Start/Close:    启动/关闭集群服务。
//   - PushEvent:      向集群推送事件。
//   - IsClusterMode:  判断是否为集群运行模式。
//
// # 依赖
//
// 内部:
//   - cluster/discovery:   服务发现抽象与实现
//   - cluster/endpoints:   端点状态管理
//   - config:              集群配置
//   - event:               事件总线
//   - interfaces:          IDiscovery, INodeContext 等
//   - log:                 日志
//   - rpc/client:          跨节点 RPC 发送
//   - rpc/remote/handler:  远程消息接收
//
// 外部:
//   - 无直接外部依赖（通过子包间接依赖 etcd）
package cluster
