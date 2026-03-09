// Package router 提供 EmberEngine 的服务端点路由选择能力。
//
// # OpenSpec
//
//   - 模块:     路由器
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/router
//   - 层级:     infrastructure
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// router 包负责在集群端点中寻找目标服务。实现了 ISelector 接口，
// 提供基于 Partition、ServiceType、ServiceId 或自定义规则（Rule）
// 的路由选择策略。是 RPC 调用链中"寻址"环节的核心组件。
//
// # 核心类型
//
//   - Selector: 实现 interfaces.ISelector，支持以下选择方式：
//   - Select:               按服务名直接选择
//   - SelectByPid:          按指定 PID 选择
//   - SelectByRule:         按自定义规则选择
//   - SelectByServiceType:  按服务类型选择
//   - SelectByFilterAndChoice: 按过滤器+选择器组合选择
//
// # 依赖
//
// 内部:
//   - actor:      PID 类型
//   - interfaces: ISelector 接口
//   - cluster/endpoints/repository: 端点仓库查询
package router
