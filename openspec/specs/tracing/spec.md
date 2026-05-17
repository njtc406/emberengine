# Tracing 追踪规范

## 目的

本规范描述 `engine/pkg/tracing/` 当前代码已经实现的最小追踪抽象与全局 tracer 注入模型。

## Requirements

### Requirement: tracing 包必须提供最小可替换的 ITracer 与 ISpan 抽象

#### Scenario: 上层通过 ITracer.Start 创建 Span

- **GIVEN** 上层代码需要创建一个追踪 Span
- **WHEN** 调用 `ITracer.Start(ctx, operationName)`
- **THEN** tracer 必须返回新的 context 和对应的 `ISpan`

### Requirement: 默认全局 tracer 必须是 noop 实现

#### Scenario: 未显式注入 tracer 时返回 noop tracer

- **GIVEN** 系统尚未调用 `SetGlobalTracer`
- **WHEN** 调用 `GlobalTracer()`
- **THEN** 必须返回 noop tracer
- **AND** `IsEnabled()` 必须返回 false

### Requirement: SetGlobalTracer 必须允许把 nil 回退为 noop tracer

#### Scenario: 注入 nil 时自动回退 noop tracer

- **GIVEN** 调用方把 nil 传给 `SetGlobalTracer`
- **WHEN** 设置全局 tracer
- **THEN** 系统必须自动回退到 noop tracer

## Out of Scope

- OpenTelemetry 具体接入方案
- 跨进程 trace 传播格式
