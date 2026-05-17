# SysService DBService 数据库服务规范

## 目的

本规范描述 `engine/pkg/sysService/dbservice/` 当前代码已经实现的多数据库子模块装配与统一访问语义。

## Requirements

### Requirement: RegisterDBService 必须注册服务工厂和配置定义

#### Scenario: 注册阶段同时写入 services 与 config 注册表

- **GIVEN** 系统加载 dbservice 包
- **WHEN** 调用 `RegisterDBService()`
- **THEN** 必须注册 `DBService` 服务工厂
- **AND** 必须注册 db 配置定义

### Requirement: DBService.OnInit 必须初始化 Redis、MySQL 和 Mongo 子模块

#### Scenario: 根据 DBService 配置构造并初始化三个数据库模块

- **GIVEN** DBService 已获取自己的配置
- **WHEN** 调用 `OnInit()`
- **THEN** 必须创建 RedisModule、MysqlModule 和 MongoModule
- **AND** 必须把它们作为子模块加入当前服务

### Requirement: OnRelease 必须释放所有数据库子模块

#### Scenario: 服务停止时释放所有子模块

- **GIVEN** DBService 已完成初始化
- **WHEN** 调用 `OnRelease()`
- **THEN** 必须释放全部子模块

### Requirement: APIExecuteMixedFun 必须把三个数据库客户端统一传给业务回调

#### Scenario: 执行混合回调时传入 redis、mysql 和 mongo 客户端

- **GIVEN** 调用方传入一个 `Callback`
- **WHEN** 调用 `APIExecuteMixedFun`
- **THEN** 系统必须把 Redis、MySQL 和 Mongo 客户端以及附加参数传给该回调

#### Scenario: 混合回调 panic 时记录错误而不是向外崩溃

- **GIVEN** 业务回调在执行期间发生 panic
- **WHEN** `APIExecuteMixedFun` 捕获该 panic
- **THEN** DBService 必须记录错误和堆栈信息

## Out of Scope

- 各数据库模块内部连接池策略
- 事务和跨库一致性方案
