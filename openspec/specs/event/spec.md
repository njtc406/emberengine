# Event 事件总线规范

## 目的

本规范描述 `engine/pkg/event/` 当前代码已经实现的事件总线初始化、分类、缓冲、限流和停止语义。

## Requirements

### Requirement: Event Bus 必须同时支持可选的 NATS 接入和本地订阅表

#### Scenario: 配置了 NATS 时建立远端连接

- **GIVEN** `EventBusConf` 包含有效的 NATS 配置
- **WHEN** 调用 `Bus.Init`
- **THEN** 系统必须连接 NATS
- **AND** 必须启用远端事件能力

#### Scenario: 未配置 NATS 时仍能初始化本地事件能力

- **GIVEN** `EventBusConf` 未提供 NATS 端点
- **WHEN** 调用 `Bus.Init`
- **THEN** 系统仍必须初始化本地订阅表、事件注册表、限流器和批处理器

### Requirement: Event Bus 必须在初始化时创建全局、服务器级和特定目标订阅表

#### Scenario: Init 为三类订阅维度创建独立索引

- **GIVEN** 调用 `Bus.Init`
- **WHEN** 初始化完成
- **THEN** 系统必须创建 global、server 和 specific 三类订阅索引

### Requirement: Event Bus 必须把 context 元数据编码进 actor.Event

#### Scenario: marshalEvent 把 dispatcherKey 和上下文头写入 actor.Event

- **GIVEN** 某个事件准备被编码为 `actor.Event`
- **WHEN** 调用 `marshalEvent`
- **THEN** 系统必须把 dispatcherKey 写入显式字段
- **AND** 必须把 context headers 编码进 `ContextHeaders`

#### Scenario: unmarshalEvent 必须从 headers 恢复 context

- **GIVEN** 某个 `actor.Event` 被反序列化
- **WHEN** 调用 `unmarshalEvent`
- **THEN** 系统必须从 `ContextHeaders` 重建 context
- **AND** 必须把 dispatcherKey 和 priority 恢复到 context/header 语义中

### Requirement: Event Bus 必须始终启动批处理定时器

#### Scenario: 非 NATS 模式下缓冲事件仍然会被 flush

- **GIVEN** Event Bus 未启用 NATS
- **WHEN** `Bus.Init` 完成
- **THEN** 批处理 ticker 仍必须启动
- **AND** 缓冲事件必须能被周期性 flush

### Requirement: Event Bus 停止时必须关闭连接、停止批处理并 flush 缓冲区

#### Scenario: Stop 时处理剩余缓冲事件

- **GIVEN** Event Bus 正在运行且缓冲区中仍有未投递事件
- **WHEN** 调用 `Bus.Stop`
- **THEN** 系统必须关闭 NATS 连接（若已启用）
- **AND** 必须停止批处理 goroutine 与 ticker
- **AND** 必须 flush 所有剩余缓冲事件

## Out of Scope

- 某个具体事件分类的业务语义
- NATS 服务端部署与集群配置
