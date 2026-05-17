# Cluster 集群规范

## 目的

本规范描述 `engine/pkg/cluster/` 当前代码已经实现的集群事件处理、发现接入和关闭语义。

## Requirements

### Requirement: Cluster 必须把发现层、端点层和事件处理层组装在一起

#### Scenario: Init 装配 discovery、endpoints 和 eventProcessor

- **GIVEN** 一个 Cluster 实例
- **WHEN** 调用 `Cluster.Init`
- **THEN** 必须初始化事件处理器
- **AND** 必须初始化 EndpointManager
- **AND** 必须按配置创建并初始化 discovery

### Requirement: Cluster 必须使用按 key 分片的事件 worker 池

#### Scenario: 同一事件 key 始终进入同一 shard

- **GIVEN** Cluster 配置了多个事件 worker
- **WHEN** `PushEvent` 接收到可提取 key 的事件
- **THEN** 系统必须根据 key 的哈希结果选择固定 shard
- **AND** 同一 key 的事件必须始终进入同一 shard 队列

#### Scenario: 无法提取 key 的事件固定进入 shard 0

- **GIVEN** 一个事件无法提供分片 key
- **WHEN** `PushEvent` 计算 shard
- **THEN** 该事件必须固定路由到 shard 0

### Requirement: PushEvent 必须在关闭和上下文取消时拒绝新事件

#### Scenario: 上下文已取消时返回上下文错误

- **GIVEN** 某个事件的 context 已取消
- **WHEN** 调用 `PushEvent`
- **THEN** `PushEvent` 必须返回 context 错误

#### Scenario: Cluster 已关闭时拒绝新事件

- **GIVEN** Cluster 已关闭
- **WHEN** 调用 `PushEvent`
- **THEN** 系统必须返回 cluster closed 错误

### Requirement: Close 必须幂等并安全关闭所有 shard worker

#### Scenario: Close 只执行一次真实关闭动作

- **GIVEN** 多个 goroutine 可能同时调用 `Cluster.Close`
- **WHEN** 第一次关闭开始执行
- **THEN** 只有一次调用可以真正执行关闭序列

- **AND** 关闭序列必须包括停止 discovery、停止 endpoints、关闭所有 shard channel，并等待 worker 退出

## Out of Scope

- discovery 后端的具体实现细节
- endpoint 路由策略的内部算法
