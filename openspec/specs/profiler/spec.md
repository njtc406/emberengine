# Profiler 性能剖析规范

## 目的

本规范描述 `engine/pkg/profiler/` 当前代码已经实现的本地调用耗时记录与报告语义。

## Requirements

### Requirement: Registry 必须按名称注册和管理多个 Profiler

#### Scenario: RegProfiler 对重复名称返回 nil

- **GIVEN** Registry 中已存在同名 profiler
- **WHEN** 再次调用 `RegProfiler`
- **THEN** 必须返回 nil

#### Scenario: Report 遍历所有已注册 profiler 并调用报告函数

- **GIVEN** Registry 中存在多个 profiler
- **WHEN** 调用 `Registry.Report()`
- **THEN** 必须复制当前 profiler 集合后逐个生成报告

### Requirement: Profiler 必须用 Push/Pop 记录调用耗时

#### Scenario: Push 返回一个 Analyzer 作为一次调用的结束句柄

- **GIVEN** 调用方开始记录一段执行时间
- **WHEN** 调用 `Profiler.Push(tag)`
- **THEN** 必须返回一个可复用池化的 `Analyzer`

#### Scenario: Pop 根据耗时决定是否生成慢调用记录

- **GIVEN** 一个 Analyzer 对应的调用已经结束
- **WHEN** 调用 `Analyzer.Pop()`
- **THEN** 系统必须更新调用次数与总耗时
- **AND** 当耗时超过 overtime 阈值时必须写入记录

### Requirement: 默认报告函数必须输出慢调用摘要

#### Scenario: 存在记录时输出平均耗时和慢调用条目

- **GIVEN** Profiler 中存在慢调用记录
- **WHEN** 调用 `DefaultReportFunction`
- **THEN** 必须输出总调用次数、总耗时、平均耗时和每条慢调用摘要

## Out of Scope

- 分布式性能追踪
- 外部监控系统集成
