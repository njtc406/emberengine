# SysModule RedisModule Redis 模块规范

## 目的

本规范描述 `engine/pkg/sysModule/redismodule/` 当前代码已经实现的 Redis 连接、周期健康检查与常用访问方法语义。

## Requirements

### Requirement: RedisModule.Init 必须建立连接并注册健康检查定时器

#### Scenario: 初始化后先执行连接检查

- **GIVEN** 一个 `redis.Options`
- **WHEN** 调用 `RedisModule.Init(conf)`
- **THEN** 必须创建 Redis client
- **AND** 必须立即执行一次连接检查

#### Scenario: Init 成功后注册 30 秒一次的健康检查

- **GIVEN** Redis client 已创建并通过初始连接检查
- **WHEN** Init 完成
- **THEN** 必须注册一个周期性健康检查定时器

### Requirement: 健康检查失败时必须尝试重连

#### Scenario: 定时器检测到 Ping 失败后执行 reconnect

- **GIVEN** 周期性健康检查发现当前连接不可用
- **WHEN** 定时器回调执行
- **THEN** 模块必须尝试创建新的 client 并替换旧连接

### Requirement: OnRelease 必须取消定时器并关闭 client

#### Scenario: 模块释放时清理健康检查与连接

- **GIVEN** RedisModule 已初始化
- **WHEN** 调用 `OnRelease()`
- **THEN** 必须取消健康检查定时器
- **AND** 必须关闭当前 Redis client

### Requirement: RedisModule 必须提供字符串、JSON、Hash 和通用执行接口

#### Scenario: 常用 API 直接委托给当前 client

- **GIVEN** 调用方使用 `ApiRedisSetString`、`ApiRedisGetString`、`ApiRedisSetStringJson`、`ApiRedisGetStringJson`、`ApiRedisHSetStruct`、`ApiRedisHGetStruct` 或 `ApiRedisExecuteFun`
- **WHEN** 这些方法被调用
- **THEN** 模块必须把操作委托给当前 Redis client

## Out of Scope

- Redis Cluster 模式实现
- 更高级缓存策略与淘汰策略
