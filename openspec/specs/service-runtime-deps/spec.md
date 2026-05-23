# Service Runtime Deps 规范

## 目的

本规范描述 `core.Service` 运行时依赖字段的收拢方式，以及 Profiler 注册/注销桥接逻辑的组织约束。

## Requirements

### Requirement: 运行时依赖必须收拢为 runtimeDeps 内部结构体

`Service` 必须将 `nodeCtx`、`cluster`、`endpointManager`、`profilerRegistry`、`router` 五个运行时依赖字段收拢为一个 `runtimeDeps` 内部 sub-struct。

#### Scenario: SetRuntimeDeps 设置 runtimeDeps 结构体

- **WHEN** `ServiceManager` 调用 `SetRuntimeDeps(cluster, endpointManager, profilerRegistry, router)`
- **THEN** Service 必须将这些依赖存储在内部 `runtimeDeps` 结构体中
- **AND** `SetRuntimeDeps` 方法签名不变

#### Scenario: SetNodeContext 设置到 runtimeDeps 中

- **WHEN** `ServiceManager` 调用 `SetNodeContext(ctx)`
- **THEN** Service 必须将 nodeCtx 存储在 `runtimeDeps` 结构体中
- **AND** `GetNodeContext()` 返回值不变

#### Scenario: 公开 getter 行为不变

- **WHEN** 调用 `GetEndpointManager()`、`GetRouter()`、`GetNodeContext()`
- **THEN** 返回值和 fallback 逻辑（先查直属字段，后查 nodeCtx）必须与重构前完全一致

### Requirement: profilerBridge 必须封装 Profiler 注册与注销逻辑

`Service` 必须将 Profiler 的 Open/Close 逻辑提取到内部 `profilerBridge` helper 中，消除重复的 registry adapter 构造。

#### Scenario: OpenProfiler 委托给 profilerBridge

- **WHEN** 调用 `Service.OpenProfiler()`
- **THEN** 必须通过 `profilerBridge` 完成 profiler 注册
- **AND** 行为与重构前一致（优先使用直属 registry，fallback 到 nodeCtx）

#### Scenario: closeProfiler 委托给 profilerBridge

- **WHEN** Service 释放时调用 `closeProfiler()`
- **THEN** 必须通过 `profilerBridge` 完成 profiler 注销
- **AND** profiler 引用被清空
