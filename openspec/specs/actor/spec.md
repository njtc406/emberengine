# Actor 运行时规范

## 目的

本规范描述 `engine/pkg/actor/` 当前代码已经实现的稳定运行时行为。

范围包括：

- PID 运行时身份与序列化契约
- Mailbox 投递、丢弃、释放与中间件收尾契约
- WorkerPool 调度拓扑与 dispatcherKey 路由语义
- RW 读写分离的启停与执行语义
- 优先级队列的回退行为

本规范不复述历史设计文档，也不描述尚未在代码中落地的目标能力。

## 运行时结构

```text
producer
  -> Mailbox.PostJob
  -> suspend gate
  -> middleware OnReceive
  -> WorkerPool.DispatchJob
  -> Worker.SubmitJob
  -> QueueManager
  -> Worker.run / read pipeline
  -> IMessageInvoker.ExecuteJob
  -> middleware OnComplete
  -> Job.Release
```

当前 `engine/pkg/actor/` 手写逻辑很薄，主要运行时语义集中在 `engine/pkg/actor/mailbox/`。

## Requirements

### Requirement: Mailbox 必须接管 Job 所有权

`Mailbox.PostJob` 返回后，调用方不得继续持有或释放该 Job。

Mailbox 必须保证：

- 成功路径由 Worker 在执行完成后释放 Job
- 失败路径由 Mailbox 自身完成丢弃通知、必要的中间件收尾和 Job 释放
- 调用方不需要根据成功或失败决定是否释放 Job

#### Scenario: PostJob 成功后由 Worker 释放 Job

- **GIVEN** 调用方创建一个 Job 并调用 `Mailbox.PostJob`
- **WHEN** Job 成功进入 Worker 队列并被执行
- **THEN** Job 必须在执行完成后由 Worker 调用 `Job.Release()`
- **AND** 调用方不得再次调用 `Release()`

#### Scenario: Mailbox 挂起时同步丢弃并释放 Job

- **GIVEN** Mailbox 处于挂起状态
- **AND** 当前 Job 不满足挂起放行策略
- **WHEN** 调用方调用 `Mailbox.PostJob`
- **THEN** `PostJob` 必须返回 `ErrMailboxSuspended`
- **AND** Mailbox 必须在返回前触发丢弃通知并释放 Job

#### Scenario: 中间件拒绝时必须完成收尾和释放

- **GIVEN** 一个 Job 已进入 `ExecuteOnReceive`
- **WHEN** 任一中间件返回 Reject
- **THEN** Mailbox 必须先触发 `OnJobDiscarded`
- **AND** 再执行 `ExecuteOnComplete`
- **AND** 最后释放 Job

#### Scenario: 分发失败时必须完成收尾和释放

- **GIVEN** 一个 Job 已完成 `ExecuteOnReceive`
- **WHEN** `WorkerPool.DispatchJob` 返回错误
- **THEN** Mailbox 必须先触发 `OnJobDiscarded`
- **AND** 再执行 `ExecuteOnComplete`
- **AND** 最后释放 Job

### Requirement: Mailbox 失败路径必须保持固定收尾顺序

对于任何“Job 不会被业务执行”的路径，只要该 Job 已经拥有中间件上下文，Mailbox 必须遵循固定的失败收尾顺序：

```text
OnJobDiscarded -> ExecuteOnComplete -> Job.Release
```

这样可以保证：

- `OnJobDiscarded` 仍能读取有效的 Job 数据
- 中间件上下文在释放前能完成 `OnComplete`
- Job 最终总能回到池中

#### Scenario: 丢弃回调在中间件上下文归还之前执行

- **GIVEN** 一个 Job 已经绑定 middleware context
- **WHEN** 该 Job 在执行前失败
- **THEN** `OnJobDiscarded` 必须发生在 `ExecuteOnComplete` 之前
- **AND** `Job.Release()` 必须发生在 `ExecuteOnComplete` 之后

### Requirement: Middleware 链必须对单个 Job 使用一致快照

Mailbox 中间件链必须保证同一个 Job 的 `OnReceive` 与 `OnComplete` 使用同一份中间件列表快照。

#### Scenario: OnReceive 与 OnComplete 使用同一份快照

- **GIVEN** 某个 Job 在投递时执行了 `ExecuteOnReceive`
- **WHEN** 运行期间有其它 goroutine 动态 Add 或 Remove 中间件
- **THEN** 该 Job 的 `ExecuteOnComplete` 仍必须使用 `OnReceive` 时捕获的中间件快照
- **AND** 不得看到新的中间件集合

#### Scenario: 中间件 panic 不得破坏收尾链路

- **GIVEN** 中间件在 `OnReceive`、`OnComplete`、`OnStart` 或 `OnStop` 中 panic
- **WHEN** Mailbox 执行中间件逻辑
- **THEN** panic 必须被 recover
- **AND** panic 信息必须通过已配置的 panic handler 上报
- **AND** Job 的后续收尾链路不得被中断

### Requirement: WorkerPool 必须通过不可变快照进行分发

WorkerPool 的热路径分发必须基于不可变快照读取，而不是在每次分发时持有拓扑读锁。

#### Scenario: 分发通过快照读取当前拓扑

- **GIVEN** WorkerPool 已启动
- **WHEN** `DispatchJob` 被调用
- **THEN** 它必须通过当前 `workersSnapshot` 读取 workers 和 dispatch ring
- **AND** 不得依赖热路径上的拓扑写锁

#### Scenario: 拓扑变更通过冷路径发布新快照

- **GIVEN** Worker 数量发生变化
- **WHEN** WorkerPool 扩容或缩容
- **THEN** 它必须在冷路径中构建新的拓扑快照
- **AND** 通过原子发布替换旧快照

### Requirement: dispatcherKey 必须通过稳定哈希路由到活跃 Worker

当 Worker 数量不变时，相同 dispatcherKey 必须稳定映射到相同 Worker。

#### Scenario: dispatcherKey 稳态命中同一 Worker

- **GIVEN** 当前活跃 Worker 集合不变
- **WHEN** 多个 Job 使用相同 dispatcherKey 投递
- **THEN** 它们必须经由 dispatch ring 命中同一个 Worker

#### Scenario: 拓扑变化后允许重映射

- **GIVEN** Worker 拓扑发生变化
- **WHEN** 新快照生效
- **THEN** dispatcherKey 可以按 jump consistent hash 规则重映射到新的 Worker
- **AND** 映射规则必须由当前活跃 Worker 集合决定

### Requirement: RW 模式必须在启动前启用，在运行时只允许关闭

RW 读写分离所需的读流水线、读通道和共享控制器属于启动时拓扑，不支持在一个未启用 RW 的 Mailbox 上运行时开启。

#### Scenario: 未启用 RW 的 Mailbox 不能运行时开启 RW

- **GIVEN** 一个 Mailbox 在创建时未启用 RW 模式
- **WHEN** 运行时请求启用 RW
- **THEN** 系统必须返回 `ErrRWDynamicEnableUnsupported`

#### Scenario: 已启用 RW 的 Mailbox 可以运行时关闭 RW

- **GIVEN** 一个 Mailbox 在创建时启用了 RW 模式
- **WHEN** 运行时请求关闭 RW
- **THEN** 系统必须允许执行安全关闭流程
- **AND** 关闭流程必须受 stop timeout 约束

### Requirement: RW 模式必须把读和写分配到不同执行路径

RW 模式下，写 Job 必须在 Worker 主循环中执行，读 Job 必须经过专门的读流水线路径执行。

#### Scenario: 写 Job 在主循环内执行并持有共享写锁

- **GIVEN** 一个 Job 被标记为写模式
- **WHEN** Worker 主循环取到该 Job
- **THEN** Worker 必须在主循环中执行该 Job
- **AND** 必须通过共享写锁与所有读操作互斥

#### Scenario: 读 Job 经由读流水线执行

- **GIVEN** 一个 Job 被标记为读模式
- **WHEN** Worker 主循环取到该 Job
- **THEN** Worker 必须把该 Job 投递到 `readCh`
- **AND** 由 `runReadPipeline` 完成 gate、注册 inflight 状态并启动读执行

### Requirement: RW 模式必须限制读并发并支持停机兜底

RWController 必须提供共享读并发限制和停机超时控制，避免读路径无限膨胀或在关闭过程中永久悬挂。

#### Scenario: 读并发受共享信号量控制

- **GIVEN** RW 模式启用了最大读并发数
- **WHEN** 多个读 Job 同时进入执行阶段
- **THEN** 同时持有读执行资格的 Job 数量不得超过配置上限

#### Scenario: 关闭过程中读路径必须受 stop timeout 约束

- **GIVEN** Worker 正在关闭
- **WHEN** 仍存在 read pipeline 残留 Job 或 in-flight 读任务
- **THEN** 系统必须使用 stop timeout 作为关闭兜底条件
- **AND** 不得无限等待

#### Scenario: 背压或关闭时读 Job 可以被丢弃

- **GIVEN** 读通道满载或 Worker 已进入关闭阶段
- **WHEN** 某个读 Job 无法安全进入执行路径
- **THEN** 该 Job 可以被丢弃
- **AND** 丢弃必须走统一的丢弃通知与释放路径

### Requirement: 多优先级队列必须对未注册优先级执行回退

当 Job 的优先级在优先级队列中没有显式注册时，系统不得静默丢弃该 Job，只要存在可用 fallback 队列，就必须把该 Job 路由到 fallback 优先级。

#### Scenario: 未注册优先级被改写到 fallback 优先级

- **GIVEN** PriorityQueueManager 已配置 fallback 优先级
- **WHEN** 一个 Job 使用未注册的优先级提交
- **THEN** 该 Job 必须被提交到 fallback 队列
- **AND** Job 的 priority 字段必须被改写为 fallback 优先级

#### Scenario: 首次发生回退时输出告警

- **GIVEN** PriorityQueueManager 首次收到未注册优先级的 Job
- **WHEN** 回退逻辑生效
- **THEN** 系统必须输出一次 warn-once 告警

### Requirement: PID 的运行时权威状态必须来自 MasterFlag

PID 的主从运行时状态必须由 `MasterFlag` 表示，而不是直接依赖 protobuf 的 `IsMaster` 字段。

#### Scenario: SetMaster 只更新运行时原子字段

- **GIVEN** 一个 PID 实例
- **WHEN** 调用 `SetMaster(true)` 或 `SetMaster(false)`
- **THEN** 运行时主从状态必须写入 `MasterFlag`
- **AND** 不得直接把 protobuf `IsMaster` 作为运行时权威字段

#### Scenario: PrepareForMarshal 在序列化出口投影主从状态

- **GIVEN** 一个 PID 实例已经有最新的 `MasterFlag`
- **WHEN** 该 PID 即将被序列化到 wire message
- **THEN** `PrepareForMarshal` 必须把 `MasterFlag` 投影到 protobuf `IsMaster` 字段

#### Scenario: 嵌入父级消息时必须使用独立 wire 副本

- **GIVEN** PID 将作为父级 protobuf 消息的子字段参与序列化
- **WHEN** 调用方为该父级消息准备 PID
- **THEN** 调用方必须使用 `SnapshotForWire` 获得独立副本

### Requirement: PID 反序列化入口必须支持同步运行时主从状态

当 PID 从网络或存储反序列化后，运行时逻辑不能直接假定 protobuf `IsMaster` 已经同步到了运行时原子状态。

#### Scenario: legacy 反序列化后需要显式同步 MasterFlag

- **GIVEN** 一个 PID 只通过 protobuf `IsMaster` 字段完成了反序列化
- **WHEN** 运行时需要读取主从状态
- **THEN** 调用方必须先调用 `SyncMasterFlag()`
- **AND** 之后才能把 `IsMasterNode()` 结果作为运行时判断依据

## Out of Scope

以下内容不由本规范定义：

- 生产环境的 worker 数量与 read pool 容量推荐值
- Autoscaler 各策略的生产阈值建议
- `engine/pkg/core`、`engine/pkg/rpc`、`engine/pkg/node` 对 actor 的完整集成时序
- 面向业务开发者的高层使用指南
