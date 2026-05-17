# Metrics 指标聚合规范

## 目的

本规范描述 `engine/pkg/metrics/` 当前代码已经实现的指标样本聚合和快照输出语义。

## Requirements

### Requirement: metrics 包必须作为纯聚合层，不反向依赖 node

#### Scenario: SnapshotInfo 由调用方填充

- **GIVEN** 调用方需要导出节点运行时指标
- **WHEN** 使用 metrics 包
- **THEN** 调用方必须先构造 `SnapshotInfo`
- **AND** metrics 包只负责把该快照转换成样本和文本

### Requirement: SnapshotToSamples 必须按稳定顺序输出样本

#### Scenario: 样本输出顺序固定为 Node、RPC、Mailbox、Event、Pool

- **GIVEN** 一个完整的 `SnapshotInfo`
- **WHEN** 调用 `SnapshotToSamples`
- **THEN** 样本输出顺序必须固定为 Node -> RPC -> Mailbox -> Event -> Pool

### Requirement: SnapshotToText 必须把样本转换成 Prometheus exposition text

#### Scenario: SnapshotToText 直接委托样本转换链路

- **GIVEN** 一个 `SnapshotInfo`
- **WHEN** 调用 `SnapshotToText`
- **THEN** 系统必须先构造样本列表
- **AND** 再把样本列表转换为文本格式

## Out of Scope

- 指标抓取端点的 HTTP 暴露方式
- Prometheus 服务端配置
