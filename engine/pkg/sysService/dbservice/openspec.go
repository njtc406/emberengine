// Package dbservice 提供内置的数据库服务。
//
// # OpenSpec
//
//   - 模块:     数据库服务
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/sysService/dbservice
//   - 层级:     extension
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// dbservice 包提供内置的数据库管理系统服务。作为系统服务注册到
// Node 中，集中管理数据库连接池和数据库操作，为其他业务服务
// 提供统一的数据库访问入口。
//
// # 核心类型
//
//   - DbService: 数据库系统服务，嵌入 core.Service。
//
// # 子包
//
//   - config: 数据库服务配置定义。
//
// # 依赖
//
// 内部:
//   - core:          Service 基类
//   - config:        服务配置
//   - sysModule:     数据库模块（mysql/mongo 等）
package dbservice
