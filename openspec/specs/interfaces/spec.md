# Interfaces 抽象契约规范

## 目的

本规范描述 `engine/pkg/interfaces/` 当前代码已经定义的核心抽象边界，重点覆盖 Mailbox、Service、NodeContext 及其相关扩展接口的外部契约。

## Requirements

### Requirement: IMailboxChannel 必须声明 PostJob 所有权转移契约

`IMailboxChannel` 的 `PostJob` 必须明确：调用方在调用后不再持有 Job，无论返回成功还是失败。

#### Scenario: 调用方不能在 PostJob 失败后自行 Release Job

- **GIVEN** 调用方通过 `IMailboxChannel.PostJob(job)` 投递一个 Job
- **WHEN** `PostJob` 返回错误
- **THEN** 调用方不得再次调用 `job.Release()`
- **AND** Job 的释放责任必须由 Mailbox 实现方承担

### Requirement: IMailbox 必须把停止语义分成 BeginStop 和 Wait

#### Scenario: BeginStop 允许阻塞进行关闭编排

- **GIVEN** Mailbox 正在运行
- **WHEN** 调用 `BeginStop()`
- **THEN** Mailbox 必须停止接收新消息
- **AND** 允许阻塞等待内部关闭编排完成

#### Scenario: Wait 等待 Mailbox 完全停止

- **GIVEN** Mailbox 已经发起停止
- **WHEN** 调用 `Wait()`
- **THEN** 调用方必须等待 Mailbox 完全停止

### Requirement: IMessageInvoker 必须提供统一的丢弃回调入口

#### Scenario: Job 不会被业务执行时触发 OnJobDiscarded

- **GIVEN** 一个 Job 在进入业务执行前已经被拒绝、丢弃或终止
- **WHEN** Mailbox 或 Service 层确定该 Job 不会被业务执行
- **THEN** 必须调用 `IMessageInvoker.OnJobDiscarded(job, reason)`

### Requirement: IMailboxMiddleware 必须采用 OnReceive / OnComplete 双阶段模型

#### Scenario: 中间件在消息入队前做准入判断

- **GIVEN** 一个消息正在进入 Mailbox
- **WHEN** Mailbox 执行 `OnReceive`
- **THEN** 中间件必须能够返回 Continue、Reject 或 Skip 来控制后续流程

#### Scenario: 中间件在消息完成后做逆序收尾

- **GIVEN** 某个消息已经执行结束或进入失败收尾路径
- **WHEN** Mailbox 执行 `OnComplete`
- **THEN** 中间件必须接收到与该消息对应的上下文和错误信息

### Requirement: IService 必须组合生命周期、标识、Mailbox 与 RPC 能力

#### Scenario: Service 通过组合接口暴露统一运行时能力

- **GIVEN** 运行时需要把服务作为统一对象处理
- **WHEN** 上层依赖 `IService`
- **THEN** `IService` 必须同时提供生命周期、标识、Mailbox 投递、日志和 RPC 处理能力

## Out of Scope

- 各接口的具体实现细节
- 任一实现类的性能策略
