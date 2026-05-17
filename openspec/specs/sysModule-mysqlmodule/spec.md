# SysModule MysqlModule MySQL 模块规范

## 目的

本规范描述 `engine/pkg/sysModule/mysqlmodule/` 当前代码已经实现的 MySQL 连接、事务执行和建表迁移语义。

## Requirements

### Requirement: MysqlModule.Init 必须创建主连接并配置连接池

#### Scenario: Init 使用基础 DSN 建立 gorm 连接

- **GIVEN** 一个 `mysqlmodule.Conf`
- **WHEN** 调用 `Init(conf)`
- **THEN** 必须先创建不指定 database 的主连接
- **AND** 必须把该连接保存为模块主 client

#### Scenario: Init 根据 CPU 核数设置连接池容量

- **GIVEN** 主连接已建立
- **WHEN** Init 完成连接池配置
- **THEN** 必须按当前 CPU 核数设置 `MaxIdleConns` 和 `MaxOpenConns`

### Requirement: ApiMysqlExecuteFun 必须执行回调并捕获 panic

#### Scenario: 业务回调发生 panic 时记录错误而不向外崩溃

- **GIVEN** 一个业务回调在执行时 panic
- **WHEN** 调用 `ApiMysqlExecuteFun`
- **THEN** 模块必须捕获 panic 并记录错误堆栈

### Requirement: ApiMysqlExecuteTransaction 必须在单个事务中顺序执行回调列表

#### Scenario: 任一事务回调失败时整个事务回滚

- **GIVEN** 多个 `TransactionCallback`
- **WHEN** 调用 `ApiMysqlExecuteTransaction`
- **THEN** 必须在同一事务中按顺序执行这些回调
- **AND** 任一回调返回错误时必须中止事务

### Requirement: ApiInitTables 必须按指定数据库进行自动迁移

#### Scenario: 数据库不存在时先创建，再执行 AutoMigrate

- **GIVEN** 一个数据库名和若干表模型
- **WHEN** 调用 `ApiInitTables`
- **THEN** 模块必须先确保数据库存在
- **AND** 必须对给定表模型执行 `AutoMigrate`

## Out of Scope

- 业务 SQL 组织方式
- 分库分表策略
