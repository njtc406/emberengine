# DTO 数据传输对象规范

## 目的

本规范描述 `engine/pkg/dto/` 当前代码已经实现的跨模块参数对象、回调类型和中间件结果载体语义。

## Requirements

### Requirement: dto 包必须提供异步 RPC 回调类型与取消函数类型

#### Scenario: CompletionFuncs 逐个执行异步回调

- **GIVEN** 一个 `CompletionFuncs`
- **WHEN** 调用 `DoCallback(ctx, data, err, params...)`
- **THEN** 必须按顺序执行所有回调函数

### Requirement: Headers 必须提供独立拷贝和字段转换能力

#### Scenario: ToMap 返回新的 map 副本

- **GIVEN** 一份 `Headers`
- **WHEN** 调用 `ToMap()`
- **THEN** 必须返回一份新的 map 副本而不是原始引用

#### Scenario: ToFields 把 Headers 转成日志字段集合

- **GIVEN** 一份 `Headers`
- **WHEN** 调用 `ToFields()`
- **THEN** 必须把所有键值转换到日志字段集合中

### Requirement: BusOption 必须通过 builder 模式构造调用参数

#### Scenario: NewBusOption 依次应用所有 builder

- **GIVEN** 多个 `BusOptionBuilder`
- **WHEN** 调用 `NewBusOption(builders...)`
- **THEN** 系统必须按传入顺序应用所有 builder

#### Scenario: Reset 把 BusOption 恢复到默认状态

- **GIVEN** 一个已使用的 `BusOption`
- **WHEN** 调用 `Reset()`
- **THEN** 必须清空上下文、方法、输入输出、回调和回调参数
- **AND** 必须把调用模式重置为 `CallModeAny`

### Requirement: MiddlewareResult 必须表达继续、拒绝和跳过三种中间件动作

#### Scenario: Continue、Reject、Skip 分别构造固定动作结果

- **GIVEN** 上层代码需要表达中间件处理结果
- **WHEN** 调用 `Continue()`、`Reject(err)` 或 `Skip()`
- **THEN** 必须分别返回 Continue、Reject、Skip 三种动作结果

## Out of Scope

- DTO 在网络协议中的编码细节
- 各业务请求响应结构体定义
