# Authz 授权规范

## 目的

本规范描述 `engine/pkg/authz/` 当前代码已经实现的 Principal 提取、RBAC 授权器和策略 Watcher 行为。

## Requirements

### Requirement: Principal 必须从 PID 提取调用方身份

#### Scenario: PrincipalFromPID 提取 ServiceType、ServiceName 和 NodeUid

- **GIVEN** 一个有效的 `actor.PID`
- **WHEN** 调用 `PrincipalFromPID`
- **THEN** 必须提取 `ServiceType`、`ServiceName` 和 `NodeUid`

### Requirement: Authorizer 必须支持基于 serviceType 的角色绑定

#### Scenario: 角色绑定后按 serviceType 聚合权限

- **GIVEN** 一个 serviceType 绑定了多个 role
- **WHEN** 调用 `Authorize`
- **THEN** 该 serviceType 的权限集合必须是所有绑定角色权限的并集

### Requirement: Authorizer 未启用时必须默认放行

#### Scenario: IsEnabled=false 时跳过授权检查

- **GIVEN** Authorizer 当前未启用
- **WHEN** 调用 `Authorize`
- **THEN** 必须直接返回允许

### Requirement: 权限匹配必须支持精确匹配、前缀匹配和全匹配

#### Scenario: 星号表示全部权限

- **GIVEN** 角色包含 `*`
- **WHEN** 校验任意资源
- **THEN** 必须视为允许

#### Scenario: ServiceName.MethodPrefix* 表示前缀匹配

- **GIVEN** 权限模式为 `ServiceName.MethodPrefix*`
- **WHEN** 资源以前缀开头
- **THEN** 必须匹配成功

### Requirement: PolicyWatcher 必须先做初始加载，再决定是否启动 watch 循环

#### Scenario: fail-closed 下初始加载失败必须阻断启动

- **GIVEN** PolicyWatcher 配置为 `FailOpen=false`
- **WHEN** 初始策略加载失败或应用失败
- **THEN** `Start` 必须返回错误

#### Scenario: fail-open 下初始加载失败不阻断启动

- **GIVEN** PolicyWatcher 配置为 `FailOpen=true`
- **WHEN** 初始策略加载失败或应用失败
- **THEN** `Start` 不得阻断启动
- **AND** Authorizer 保持当前策略状态

#### Scenario: Store 返回 watch channel 时启动后台 watchLoop

- **GIVEN** PolicyStore 成功返回 watch channel
- **WHEN** `PolicyWatcher.Start` 完成初始加载
- **THEN** 系统必须启动后台 `watchLoop`

### Requirement: Watch 更新失败时必须保留旧快照

#### Scenario: 收到非法快照时不清空现有策略

- **GIVEN** watch 收到一个无法成功应用的策略快照
- **WHEN** `handleEvent` 处理该事件
- **THEN** 系统必须保留旧快照
- **AND** 不得把授权器清空到无策略状态

### Requirement: Watcher.Stop 必须幂等

#### Scenario: 多次调用 Stop 只执行一次真实关闭

- **GIVEN** 多个调用方可能重复调用 `PolicyWatcher.Stop`
- **WHEN** Stop 被多次调用
- **THEN** 只有第一次调用可以真正执行 cancel、等待 goroutine 和关闭 store

## Out of Scope

- PID 真实性验证增强模式
- 策略来源的外部存储模型设计
