# Mailbox 设计与使用说明

Mailbox 是 Ember 中 service/actor 的消息入口，负责接收事件、分发到 worker 并按配置的队列/扩缩容策略处理。

## 核心概念

- **Mailbox**：每个 service 拥有一个 mailbox，用于接收并排队待处理的事件。
- **WorkerPool**：每个 mailbox 内部维护一个 worker 池，由一个或多个 worker 组成。
- **Worker**：实际执行消息处理逻辑的协程，从队列中拉取事件并调用 `IMessageInvoker`。
- **QueueManager**：封装队列模型，负责入队/出队策略，目前支持：
  - `dual`：双队列（系统队列 + 用户队列），系统队列优先；
  - `priority`：多优先级队列，支持 sys/urgent/high/normal/low/batch 六级优先级。
- **WorkerSchedulePolicy**：控制 worker 数量、虚拟节点、空闲策略和扩缩容策略。
- **AutoScaler / AutoScalerStrategy**：根据当前负载自动调整 worker 数量。

## 并发模型：单 worker 与多 worker

Mailbox 的并发行为由 `WorkerSchedulePolicy.InitialWorkerNum` 决定：

- **单 worker 模式（Actor-like）**
  - `InitialWorkerNum = 1`。
  - WorkerPool 只创建一个 worker，所有消息在单个 goroutine 中按顺序处理。
  - 对于同一个 service：
    - 不会有多个线程同时执行业务逻辑；
    - 可以将内部状态视为“单线程上下文”；
    - 行为接近经典 Actor 模型。

- **多 worker 模式（并发服务模型）**
  - `InitialWorkerNum > 1`。
  - WorkerPool 创建多个 worker，并基于一致性哈希环（`evt.GetDispatcherKey()`）将事件路由到具体 worker：
    - 相同 dispatcherKey 的消息会落在同一 worker 上，保持局部顺序；
    - 不同 dispatcherKey 的消息可以在多个 worker 上并发处理。
  - 此时 service 内部如有共享状态，需要自行保证并发安全（锁、无锁结构等）。

> 总结：想要“接近 Actor 模型”的语义，请使用单 worker 配置；想要更高吞吐量，请配置多 worker 并注意并发安全。

## 配置入口：MailboxConf 与 WorkerSchedulePolicy

Mailbox 相关配置由 `config.MailboxConf` 描述，其中最重要的是两部分：

- `QueueMode string`
  - `"dual"`：双队列模式（系统队列 + 用户队列），系统队列始终优先处理；
  - `"priority"`：多优先级队列模式，通过 `MultiLevelQueueConf` 控制调度策略。

- `SchedulePolicy *config.WorkerSchedulePolicy`
  - `InitialWorkerNum int`：初始 worker 数量。
    - `1`：单 worker，顺序执行，Actor-like。
    - `>1`：多 worker，并发执行，通过 hash ring 均衡分配。
  - `VirtualWorkerRate int`：hash ring 虚拟节点倍率，用于改善多 worker 场景下的负载均衡。
  - `EnableAutoScaling bool`：是否启用自动扩缩容。
  - `ScalingStrategy *config.WorkerStrategyConfig`：扩缩容策略配置（策略名称、参数、最小/最大 worker 数、冷却时间等）。
  - `IdlerConf *config.WorkerIdlerConf`：空闲控制策略（是否使用条件变量、退避时间、最大重试次数等）。
  - `MultiLevelQueueConf *config.MultiLevelQueueConf`：多优先级队列调度策略，仅在 `QueueMode = "priority"` 时生效。

### 常用配置示例

参见 `config_example.go` 中的示例函数：

- `ExampleSingleWorkerConfig`：单 worker（接近 Actor）模式。
- `ExampleDualQueueConfig`：多 worker + 双队列模式。
- `ExamplePriorityQueueConfig_Absolute`：多优先级队列 + 绝对优先策略。
- `ExamplePriorityQueueConfig_Weighted`：多优先级队列 + 加权策略。
- `ExamplePriorityQueueConfig_Fairness`：多优先级队列 + 公平策略。
- `ExampleAutoScalingConfig`：启用自动扩缩容的配置示例。

可以直接参考这些示例来为具体 service 选择合适的 mailbox 行为。

## 扩缩容与触发时机

自动扩缩容的核心在于：

- `AutoScalerStrategy`：给定当前所有 worker 的状态，判断是否需要扩容或缩容；
- `AutoScaler`：基于策略结果、最小/最大 worker 数和冷却时间，计算新的 worker 数量。

当前版本中：

- WorkerPool 内部通过一个 goroutine 定期（`ResizeCoolDown` 间隔）拉取 worker 状态并调用 `AutoScaler.ShouldResize`；
- 所有策略共享同一个检查周期，属于“拉模式”（polling）。

未来演进方向：

- 将“触发时机”抽离为独立调度器（scheduler）：
  - 自动模式：scheduler 使用 `time.Ticker` 等方式周期性调用统一的检查入口；
  - 手动模式：外部代码在需要时显式调用检查入口（例如管理命令、监控告警）。

## 中间件与挂起机制

### Mailbox 中间件

- Mailbox 支持中间件（`IMailboxMiddleware`），可以在消息进入 worker 之前/之后执行自定义逻辑（如限流、统计、埋点等）。
- 当前实现中：
  - 在 `defaultMailbox.PostMessage` 中，在消息入队前调用一次 `MessageReceived`；
  - 在 `Worker.safeExec` 中，在消息处理完成后再次调用一次 `MessageReceived`。
- 如果你的中间件对调用时机敏感（例如需要区分“入队前”和“处理后”），请在实现中自行区分上下文，或仅在一个阶段中使用。

> 注意：未来可能会将中间件拆分为 `BeforeEnqueue` / `AfterHandle` 两类 hook，目前 `MessageReceived` 可能被调用两次。

### 挂起与恢复（Suspend/Resume）

- `defaultMailbox` 提供简单的挂起机制：
  - `Suspend()`：标记 mailbox 已挂起；
  - `Resume()`：恢复 mailbox 接收；
  - 挂起后，优先级 **低于紧急级别（Urgent）** 的消息会被拒绝，返回 `ErrMailboxNotRunning`；
- 适用场景：
  - 服务进入“只处理紧急/控制消息”的降级模式；
  - 热更新 / 维护窗口中，先挂起普通业务流量，只保留管理流量。

## 使用建议与注意事项

1. **选择合适的 worker 数量**
   - 对顺序敏感、状态机类逻辑：优先考虑单 worker 模式（`InitialWorkerNum=1`）。
   - CPU 密集或高并发场景：根据 CPU 核心数和业务特性配置 4~16 个 worker，再通过压测调整。

2. **正确使用 dispatcherKey**
   - `evt.GetDispatcherKey()` 决定了事件在多 worker 模式下被分配到哪个 worker；
   - 相同 dispatcherKey 的事件会保持顺序，适合用来绑定“会话/玩家/房间”等需要顺序的实体；
   - 不同 dispatcherKey 之间可以并行，避免所有请求都挤在同一个 worker。

3. **注意中间件的时机与开销**
   - 中间件可能在入队前和处理后各被调用一次，避免在中间件里做非常重的操作；
   - 在压测场景下，请关闭不必要的 debug 日志和重型中间件逻辑，以免干扰基准测试。

4. **扩缩容策略从简单开始**
   - 推荐先使用 `MaxLoadStrategy` + 合理的 `ResizeCoolDown`；
   - 再根据业务特性逐步引入 `CompositeStrategy` 或其他装饰器策略（见 `SCALING_STRATEGIES_TODO.md` 设计文档）。

5. **关闭时的 drain 行为**
   - 调用 `Worker.Stop()` 时，worker 会在退出前通过 `queueManager.DrainAll` 处理完队列中的所有剩余事件；
   - 这保证了在正常关闭流程中不会丢消息，但也意味着关闭时可能需要等待一段时间。

## 相关文件

- `mailbox.go`：默认 Mailbox 实现（suspend/resume + middleware + WorkerPool 封装）。
- `worker_pool.go`：Worker 池管理、事件分发、自动扩缩容调度入口。
- `worker.go`：统一 Worker 实现，从队列中取事件并调用 `IMessageInvoker`。
- `queue_manager.go` / `queue_manager_dual.go` / `queue_manager_priority.go`：队列管理与调度策略。
- `scaler.go` / `strategy.go` / `strategy_factory.go`：扩缩容策略与执行器。
- `config_example.go`：常见 Mailbox 配置示例。
- `SCALING_STRATEGIES_TODO.md`：扩缩容策略设计与待实现列表。

