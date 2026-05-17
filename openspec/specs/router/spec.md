# Router 路由规范

## 目的

本规范描述 `engine/pkg/router/` 当前代码已经实现的路由门面语义。

## Requirements

### Requirement: Router 必须是 EndpointManager Repository 的薄封装

#### Scenario: Select 系列方法委托给 Repository

- **GIVEN** Router 持有一个 EndpointManager
- **WHEN** 调用 `Select`、`SelectByPid`、`SelectByServiceUid`、`SelectByRule`、`SelectByServiceType` 或 `SelectByFilterAndChoice`
- **THEN** Router 必须把调用委托给 EndpointManager 的 Repository

### Requirement: EndpointManager 缺失时 Router 必须返回 nil

#### Scenario: Router 没有可用 EndpointManager 时不做选择

- **GIVEN** Router 当前没有 EndpointManager
- **WHEN** 调用任意 Select 方法
- **THEN** 方法必须返回 nil

## Out of Scope

- Repository 内部的筛选与负载均衡算法
- 端点健康状态的维护逻辑
