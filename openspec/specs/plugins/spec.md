# Plugins 插件管理规范

## 目的

本规范描述 `engine/pkg/plugins/` 当前代码已经实现的最小插件注册容器语义。

## Requirements

### Requirement: PluginManager 必须按名称登记插件元信息

#### Scenario: Register 记录插件名称与路径

- **GIVEN** 调用方注册一个插件
- **WHEN** 调用 `PluginManager.Register(name, path)`
- **THEN** 必须把插件名称和路径写入 `pluginMap`

### Requirement: LoadAll 必须遍历已注册插件并调用加载入口

#### Scenario: LoadAll 顺序遍历 pluginMap 并执行 load

- **GIVEN** PluginManager 已记录多个插件
- **WHEN** 调用 `LoadAll()`
- **THEN** 必须遍历全部插件并逐个调用内部 `load(plugin)`

### Requirement: 当前插件加载逻辑仍处于占位状态

#### Scenario: load 目前不执行真实动态加载

- **GIVEN** 当前代码库中的 `plugins.load`
- **WHEN** `LoadAll()` 调用该函数
- **THEN** 当前实现不会执行真实的插件装载行为

## Out of Scope

- 动态库加载机制
- 插件生命周期回调
