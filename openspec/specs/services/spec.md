# Services 服务管理规范

## 目的

本规范描述 `engine/pkg/services/` 当前代码已经实现的服务工厂注册与 per-Node 运行时服务管理行为。

## Requirements

### Requirement: services 包必须保留包级服务工厂注册表

#### Scenario: init 阶段注册服务工厂

- **GIVEN** 某个服务包需要参与运行时装配
- **WHEN** 它在 init 阶段调用 `SetService(name, builder)`
- **THEN** 服务工厂必须被写入全局注册表

### Requirement: ServiceManager 必须按配置创建并初始化服务实例

#### Scenario: Init 根据 StartServices 逐个构建服务

- **GIVEN** 一个 ServiceConf 包含 `StartServices`
- **WHEN** 调用 `ServiceManager.Init`
- **THEN** ServiceManager 必须按配置顺序查找 builder、创建服务实例并执行 `Init`

#### Scenario: 未注册的服务类名必须导致初始化失败

- **GIVEN** 某个 `StartServices` 条目引用了未注册的 `ClassName`
- **WHEN** 调用 `ServiceManager.Init`
- **THEN** 系统必须返回错误

### Requirement: ServiceManager 必须向支持的服务注入运行时依赖

#### Scenario: 支持 runtimeDepsAware 的服务接收集群、端点、Profiler 与 Router

- **GIVEN** 某个服务实现了 `runtimeDepsAware`
- **WHEN** ServiceManager 初始化该服务
- **THEN** 必须把 Cluster、EndpointManager、ProfilerRegistry 和 Router 注入进去

#### Scenario: 支持 nodeContextAware 的服务接收 NodeContext

- **GIVEN** 某个服务实现了 `nodeContextAware`
- **WHEN** ServiceManager 初始化该服务
- **THEN** 必须把当前 NodeContext 注入进去

#### Scenario: 支持 authzAware 的服务接收 Authorizer

- **GIVEN** ServiceManager 当前持有 Authorizer
- **AND** 某个服务实现了 `authzAware`
- **WHEN** 初始化该服务
- **THEN** 必须把 Authorizer 注入进去

### Requirement: Start 失败时必须回滚已启动服务

#### Scenario: 启动中途失败时倒序停止已启动服务

- **GIVEN** 多个服务按顺序启动
- **WHEN** 其中某个服务启动失败
- **THEN** ServiceManager 必须按逆序停止之前已成功启动的服务

### Requirement: StopAll 必须按逆序停止所有服务

#### Scenario: 停止顺序与启动顺序相反

- **GIVEN** ServiceManager 管理多个运行中服务
- **WHEN** 调用 `StopAll()`
- **THEN** 必须按 runServices 的逆序逐个调用 `Stop()`

## Out of Scope

- daemon 服务的内部行为
- 单个服务的具体生命周期实现
