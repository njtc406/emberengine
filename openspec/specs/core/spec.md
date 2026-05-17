# Core 服务运行时规范

## 目的

本规范描述 `engine/pkg/core/` 当前代码已经实现的 Service 运行时装配、生命周期和依赖注入行为。

## Requirements

### Requirement: Service.Init 必须完成运行时依赖装配

`core.Service` 必须在初始化阶段装配 Mailbox、TimerScheduler、日志、事件处理器、路由与可选授权器等依赖。

#### Scenario: Init 失败时必须回滚已分配资源

- **GIVEN** Service 初始化过程中的任一步骤失败
- **WHEN** `Service.Init` 返回错误
- **THEN** Service 必须回滚已经创建的运行时资源
- **AND** 不得保留半初始化状态

### Requirement: Service 必须以 Mailbox 作为消息执行入口

#### Scenario: Service 持有一个 Mailbox 作为执行载体

- **GIVEN** 一个 Service 完成初始化
- **WHEN** 运行时向该 Service 投递 Job
- **THEN** Job 必须经由 Service 持有的 Mailbox 进入执行流程

### Requirement: Service 必须支持 StopPolicy 驱动的关闭语义

#### Scenario: StopPolicy 优先于旧 StopGraceTimeout 字段

- **GIVEN** 服务初始化配置同时包含旧的 `StopGraceTimeout` 和新的 `StopPolicy`
- **WHEN** Service 整理关闭参数
- **THEN** 必须优先使用 `StopPolicy` 中的 GraceTimeout 和 DrainPolicy

### Requirement: Service 必须支持可选注入 Authorizer

#### Scenario: Service 装配时可注入外部授权器

- **GIVEN** 节点在启动时已构建 RBAC Authorizer
- **WHEN** Service 被装配
- **THEN** Service 必须提供 `SetAuthorizer` 以接收授权器

## Out of Scope

- 具体业务服务的生命周期钩子实现
- 跨节点服务路由细节
