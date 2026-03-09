// Package config 提供 EmberEngine 的可实例化配置系统。
//
// # OpenSpec
//
//   - 模块:     配置系统
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/config
//   - 层级:     foundation
//   - 状态:     stable
//   - 线程安全: partial（加载阶段非并发安全，加载完成后只读安全）
//
// # 概述
//
// config 包是引擎的配置中枢。每个 Node 拥有独立的 Config 实例，
// 支持从本地 YAML 文件加载配置，并自动解析 ${VAR} 环境变量替换。
// 配置内容涵盖节点信息、集群设置、服务定义、邮箱参数等。
//
// 业务服务可通过 RegisterServiceConf 在 init 阶段预注册自定义配置，
// 在 Config.Load 时自动合并到配置树中。
//
// # 核心类型
//
//   - Config:          顶级配置容器，包含 NodeConf, ClusterConf, ServiceConf 等。
//   - NodeConf:        节点级配置（名称、调试、超时等）。
//   - ClusterConf:     集群配置（etcd 地址、发现模式等）。
//   - ServiceInitConf: 单个服务的初始化配置。
//   - MailboxConf:     邮箱行为配置（队列大小、Worker 数等）。
//   - ETCDConf:        etcd 连接配置。
//   - RPCServer:       RPC 服务端配置。
//   - ServiceConfig:   业务自定义配置注册项。
//   - ConfMap:         配置映射表，提供运行时配置查询。
//
// # 核心函数
//
//   - NewConfig:            创建 Config 实例。
//   - Load:                 从文件路径加载配置。
//   - RegisterServiceConf:  预注册自定义服务配置（init 阶段调用）。
//
// # 依赖
//
// 内部:
//   - def:             默认值常量
//   - log:             日志
//   - config/remote:   远程配置源（etcd）
//   - utils/validate:  配置校验引擎
//
// 外部:
//   - github.com/spf13/viper: 配置文件解析
package config
