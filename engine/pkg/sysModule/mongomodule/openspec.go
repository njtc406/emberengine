// Package mongomodule 提供 MongoDB 数据库连接模块。
//
// # OpenSpec
//
//   - 模块:     MongoDB 模块
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/sysModule/mongomodule
//   - 层级:     extension
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// mongomodule 包封装了 MongoDB 客户端的初始化和连接管理。
// 作为系统模块挂载到 Service 中，提供数据库操作的便捷入口。
//
// # 核心类型
//
//   - MongoModule: MongoDB 连接模块，嵌入 core.Module。
//
// # 依赖
//
// 内部:
//   - core:   Module 基类
//   - config: 数据库配置
//
// 外部:
//   - go.mongodb.org/mongo-driver: MongoDB 驱动
package mongodbmodule
