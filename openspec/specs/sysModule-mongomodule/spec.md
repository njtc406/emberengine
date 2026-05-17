# SysModule MongoModule Mongo 模块规范

## 目的

本规范描述 `engine/pkg/sysModule/mongomodule/` 当前代码已经实现的 MongoDB 连接、回调执行和事务执行语义。

## Requirements

### Requirement: MongoModule.Init 必须建立连接并执行健康检查

#### Scenario: 使用 URI 和可选 Auth 创建 Mongo Client

- **GIVEN** 一个 `MongoConfig`
- **WHEN** 调用 `MongoModule.Init(conf)`
- **THEN** 必须按 URI 创建 Mongo Client
- **AND** 当配置包含 Auth 时必须设置认证凭据

#### Scenario: Init 完成前必须执行 Ping

- **GIVEN** Mongo Client 已创建
- **WHEN** Init 继续执行
- **THEN** 必须在超时上下文中执行 `Ping`
- **AND** 只有 Ping 成功后才认为初始化成功

### Requirement: OnInit 必须执行全部已注册 MongoOpt

#### Scenario: Register 注册的回调在 OnInit 中逐个执行

- **GIVEN** 包级 `opts` 中已注册多个 `MongoOpt`
- **WHEN** 调用 `OnInit()`
- **THEN** 必须按顺序执行这些回调

### Requirement: ExecuteFun 必须在最大操作超时上下文中执行业务函数

#### Scenario: ExecuteFun 为回调提供带超时 context 和 client

- **GIVEN** 一个业务回调函数
- **WHEN** 调用 `ExecuteFun(f, args...)`
- **THEN** 系统必须构造带 `maxOperatorTimeOut` 的 context
- **AND** 必须把 client 与参数传给业务回调

### Requirement: ExecuteTransaction 必须通过 session.WithTransaction 执行事务回调

#### Scenario: 事务执行时创建 session 并自动结束

- **GIVEN** 一个数据库名和事务回调
- **WHEN** 调用 `ExecuteTransaction`
- **THEN** 系统必须创建 session
- **AND** 必须通过 `WithTransaction` 执行事务回调

## Out of Scope

- Mongo collection/schema 设计
- 事务冲突重试策略
