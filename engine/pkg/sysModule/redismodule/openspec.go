// Package redismodule 提供 Redis 缓存连接模块。
//
// # OpenSpec
//
//   - 模块:     Redis 模块
//   - 包路径:   github.com/njtc406/emberengine/engine/pkg/sysModule/redismodule
//   - 层级:     extension
//   - 状态:     stable
//   - 线程安全: yes
//
// # 概述
//
// redismodule 包封装了基于 go-redis 的 Redis 客户端连接管理。
// 作为系统模块挂载到 Service 中，提供 KV 存储、缓存等
// Redis 操作的便捷入口。包含基础连接管理和高级封装两层。
//
// # 核心类型
//
//   - RedisModule: Redis 连接模块，嵌入 core.Module。
//   - RedisBase:   Redis 基础操作封装。
//
// # 依赖
//
// 内部:
//   - core:   Module 基类
//   - config: Redis 配置
//
// 外部:
//   - github.com/redis/go-redis/v9: Redis 客户端
package redismodule
