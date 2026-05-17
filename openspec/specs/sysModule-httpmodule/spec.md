# SysModule HttpModule HTTP 模块规范

## 目的

本规范描述 `engine/pkg/sysModule/httpmodule/` 当前代码已经实现的通用 HTTP 服务器模块语义。

## Requirements

### Requirement: HttpModule 必须把启动与关闭委托给底层 GinServer

#### Scenario: OnInit 初始化底层服务器

- **GIVEN** 一个 `HttpModule`
- **WHEN** 调用 `OnInit()`
- **THEN** 必须调用底层 `server.Init(logger, systemMod, conf)`

#### Scenario: OnStart 只允许在未运行状态下启动

- **GIVEN** HttpModule 当前未运行
- **WHEN** 调用 `OnStart()`
- **THEN** 必须把 running 从 0 切换为 1
- **AND** 必须启动底层 server

#### Scenario: 重复启动必须返回 ErrServiceIsRunning

- **GIVEN** HttpModule 已处于运行状态
- **WHEN** 再次调用 `OnStart()`
- **THEN** 必须返回 `ErrServiceIsRunning`

### Requirement: OnRelease 必须停止底层 server 并清空引用

#### Scenario: 模块释放时停止 server

- **GIVEN** HttpModule 已经初始化
- **WHEN** 调用 `OnRelease()`
- **THEN** 必须停止底层 server
- **AND** 必须清空 server 引用

### Requirement: HttpModule 必须支持链式注入 Hook、Router 和 Middleware

#### Scenario: WithBeforeServHook、WithInitHook、WithRunHook、WithStopHook、SetRouter、WithMiddleware 返回自身

- **GIVEN** 调用方需要在构造期配置 HttpModule
- **WHEN** 调用这些配置方法
- **THEN** 必须把配置委托给底层 server
- **AND** 必须返回当前 HttpModule 以支持链式调用

## Out of Scope

- GinServer 内部实现细节
- 具体业务路由定义
