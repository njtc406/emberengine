# SysModule Gate 网关模块规范

## 目的

本规范描述 `engine/pkg/sysModule/gate/` 当前代码已经实现的协议适配器驱动、监听监督与重启退避语义。

## Requirements

### Requirement: Gate.Start 必须根据配置选择具体监听配置并启动 supervisor

#### Scenario: 根据 conf.Type 选择 ws、http、tcp 或 udp 配置

- **GIVEN** 一个 `GateService` 配置
- **WHEN** 调用 `Gate.Start(conf)`
- **THEN** 必须根据 `conf.Type` 选择对应的子配置
- **AND** 必须启动独立 supervisor goroutine

### Requirement: 未设置协议适配器时 Gate.Start 不启动监听

#### Scenario: adapter 为空时直接返回

- **GIVEN** Gate 当前未注入协议适配器
- **WHEN** 调用 `Start(conf)`
- **THEN** 方法必须直接返回且不启动监听

### Requirement: superviseServe 必须在可恢复错误上执行退避重启

#### Scenario: 临时监听错误按策略退避并重试

- **GIVEN** ListenAndServe 返回一个非永久错误
- **WHEN** 重启策略启用且未超过最大次数
- **THEN** Gate 必须按退避时间等待后重试

#### Scenario: 永久错误或达到最大重启次数时停止重启

- **GIVEN** 监听错误被判定为永久错误
- **OR** 已超过 `MaxRestart`
- **WHEN** supervisor 处理该错误
- **THEN** Gate 必须停止重试并退出 supervisor

### Requirement: OnRelease 必须关闭适配器并等待 supervisor 退出

#### Scenario: 释放 Gate 时执行 shutdown 并阻塞等待 serveDone

- **GIVEN** Gate 已启动
- **WHEN** 调用 `OnRelease()`
- **THEN** 必须取消内部 context
- **AND** 必须调用协议适配器的 `Shutdown`
- **AND** 必须等待 supervisor 退出

## Out of Scope

- WebSocket、HTTP、TCP、UDP 各协议的具体处理逻辑
- 协议适配器内部的会话管理细节
