# Mailbox 设计与使用说明

Mailbox 是 Ember 中 service 的消息入口，负责接收事件、分发到 worker 并按配置的队列/扩缩容策略处理。

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
  - `WorkerNum = 1`,且没有自动扩容策略,即运行过程中始终只有一个worker。
  - WorkerPool 只创建一个 worker，所有消息在单个 goroutine 中按顺序处理。
  - 对于同一个 service：
    - 不会有多个线程同时执行业务逻辑；
    - 可以将内部状态视为“单线程上下文”；
    - 行为是经典 Actor 模型。

- **多 worker 模式（并发服务模型）**
  - `WorkerNum > 1`, 这里的workerNum指运行后的实际worker数量,不管初始是不是1。
  - WorkerPool 创建多个 worker，并基于一致性哈希环（`evt.GetDispatcherKey()`）将事件路由到具体 worker：
    - 相同 dispatcherKey 的消息会落在同一 worker 上，保持局部顺序；
    - 不同 dispatcherKey 的消息可以在多个 worker 上并发处理。
  - 此时 service 内部如有共享状态，需要自行保证并发安全（锁、无锁结构等）。

> 总结：想要“ Actor 模型”的语义，请使用单 worker 配置；想要更高吞吐量，请配置多 worker 并注意并发安全。

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

- `ExampleSingleWorkerConfig`：单 worker（Actor）模式。
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

WorkerPool 通过统一调度循环触发扩缩容：

- 定时触发：按 `ResizeCoolDown` 间隔拉取 worker 状态并调用 `AutoScaler.ShouldResize`；
- 事件触发：当入队采样发现负载变化时唤醒调度循环，仍由 `AutoScaler.ShouldResize` 做最终决策；
- 所有策略共享同一个决策入口，避免定时触发和事件触发产生两套扩缩容语义。

## 中间件与挂起机制

### Mailbox 中间件

- Mailbox 支持中间件（`IMailboxMiddleware`），用于在**消息入队前**与**消息处理完成后**执行自定义逻辑（限流、熔断、统计、埋点、追踪等）。
- 执行位置与顺序：
  - **入队前（OnReceive）**：在 `Mailbox.PostMessage` 中调用 `middlewareChain.ExecuteOnReceive`，按注册顺序依次执行。
  - **处理后（OnComplete）**：在 `Worker.safeExec` 的 defer 中调用 `middlewareChain.ExecuteOnComplete`，按逆序执行（洋葱模型）。
- `OnReceive` 的返回值决定是否入队：
  - `Continue()`：继续执行剩余中间件，最终入队。
  - `Reject(err)`：拒绝消息入队，`PostMessage` 直接返回该错误。
  - `Skip()`：跳过剩余中间件，直接入队（用于快速路径）。
- 回调一致性：
  - 只有当消息最终成功入队并被 worker 执行后，才会触发 `OnComplete`。
  - `OnComplete` 仅对实际执行过 `OnReceive` 的中间件回调。
- panic 隔离：
  - `MiddlewareChain` 会 recover `OnReceive` / `OnComplete` / `OnStart` / `OnStop` 中的 panic；
  - `OnReceive` panic 会转换为 Reject error，走正常拒绝与 Job 释放流程；
  - `OnComplete` panic 会被记录，并继续执行剩余中间件，确保 context 归池。

#### 内置中间件

- `DispatchKeyStatsMiddleware`：debug 辅助，统计 dispatcherKey 热点与 TopN（仅 debug 模式可配置启用）。
- `RateLimitMiddleware`：基于令牌桶的入队限流，支持配置跳过紧急消息。
- `SentinelMiddleware`：基于 Sentinel 的限流 / 熔断 / 系统保护；配置中的 CircuitBreakerConf 会映射到 Sentinel circuit breaker。

#### Sentinel 细粒度 resource

Sentinel 按 resource 精确匹配规则。默认 resource 是 serviceName；如需按 JobType 限流或熔断，请使用 `NewSentinelMiddlewareWithJobType`，它会把运行时入口映射为 `serviceName:jobType`，并自动声明所有内置 JobType resource。

```go
mw := NewSentinelMiddlewareWithJobType("battle",
  WithJobTypeFlowRule(def.MailboxJobTypeRpc, 1000),
  WithJobTypeFlowRule(def.MailboxJobTypeTimer, 100),
  WithJobTypeErrorCountRule(def.MailboxJobTypeRpc, 20),
)
```

- `WithFlowRule` / `WithCircuitBreakerRule` 表示默认规则，会复制到 serviceName 与已声明的细粒度 resource。
- `WithResourceFlowRule` / `WithResourceCircuitBreakerRule` 可直接给任意自定义 resource 注册规则。
- `WithSentinelFlowRulesForResource` / `WithSentinelCircuitBreakerRulesForResource` 可传入完整 Sentinel 原生规则。
- 若直接使用 `WithResourceFunc` 自定义动态 resource，应同时用 `WithSentinelRuleResources` 声明所有可能的静态 resource；否则启动时会打印 warn，提示规则只会作用于 serviceName。
- 自定义 `MailboxJobType` 不会被 `NewSentinelMiddlewareWithJobType` 自动枚举；需要额外使用 `WithSentinelRuleResources("battle:1001")`，或直接用 `WithResourceFlowRule("battle:1001", ...)` / `WithSentinelFlowRulesForResource("battle:1001", ...)` 注册规则。

在 `NewSentinelMiddlewareWithJobType` 下，serviceName 和每个声明的 JobType resource 都会独立计算 QPS / 熔断阈值。若期望整个 service 共享总 QPS，请使用 `NewSentinelMiddleware(serviceName, WithFlowRule(...))`，不要启用 JobType 细粒度 resource。

#### 启用方式：配置 + 自定义合并

- 从配置创建：使用 `CreateMiddlewaresFromConfig(conf, serviceName, logger, isDebug)` 生成中间件列表。
- 自定义注入：将你实现的 `IMailboxMiddleware` 放入 `customMiddlewares`。
- 推荐：用 `MergeMiddlewares(configMiddlewares, customMiddlewares)` 合并（配置在前，自定义在后），然后传给 `NewMailbox`。

### 挂起与恢复（Suspend/Resume）

- `defaultMailbox` 提供简单的挂起机制：
  - `Suspend()`：标记 mailbox 已挂起；
  - `Resume()`：恢复 mailbox 接收；
  - 挂起后，不满足 `ISuspendPolicy` 放行条件的消息会被拒绝，返回 `ErrMailboxSuspended`；
- 适用场景：
  - 服务进入“只处理紧急/控制消息”的降级模式；
  - 热更新 / 维护窗口中，先挂起普通业务流量，只保留管理流量。

## 使用建议与注意事项

1. **选择合适的 worker 数量**
   - 对顺序敏感、状态机类逻辑：优先考虑单 worker 模式。
   - CPU 密集或高并发场景：根据 CPU 核心数和业务特性配置 4~16 个 worker，再通过压测调整。

2. **正确使用 dispatcherKey**
   - `evt.GetDispatcherKey()` 决定了事件在多 worker 模式下被分配到哪个 worker；
   - 相同 dispatcherKey 的事件会保持顺序，适合用来绑定“会话/玩家/房间”等需要顺序的实体；
   - 不同 dispatcherKey 之间可以并行，避免所有请求都挤在同一个 worker。

3. **注意中间件的开销**
   - 在压测场景下，请关闭不必要的 debug 日志和重型中间件逻辑，以免干扰基准测试。

4. **扩缩容策略从简单开始**
   - 推荐先使用 `MaxLoadStrategy` + 合理的 `ResizeCoolDown`；
  - 再根据业务特性逐步引入 `CompositeStrategy` 或其他装饰器策略。

5. **关闭时的 drain 行为**
   - 调用 `Worker.Stop()` 时，worker 会在退出前通过 `queueManager.DrainAll` 处理完队列中的所有剩余事件；
   - 这保证了在正常关闭流程中不会丢消息，但也意味着关闭时可能需要等待一段时间。

6. **RW 模式只通过配置启用**
  - `EnableRWMode=true` 必须在 mailbox / worker 创建前配置；
  - 运行时允许 `SetRWEnabled(false)` 关闭 RW 用于降级或维护；
  - 不支持运行时从关闭状态动态开启 RW，`SetRWEnabled(true)` 会返回 `ErrRWDynamicEnableUnsupported`。

7. **停止顺序允许阻塞**
  - Node / Service 停止需要满足“先起后退”的依赖顺序；
  - `BeginStop` 可以阻塞等待 mailbox 内部后台流程按顺序退出，上层可按服务依赖自行设置整体 timeout。

## 相关文件

- `mailbox.go`：默认 Mailbox 实现（suspend/resume + middleware + WorkerPool 封装）。
- `worker_pool.go`：Worker 池管理、事件分发、自动扩缩容调度入口。
- `worker.go`：统一 Worker 实现，从队列中取事件并调用 `IMessageInvoker`。
- `queue_manager.go` / `queue_manager_dual.go` / `queue_manager_priority.go`：队列管理与调度策略。
- `scaler.go` / `strategy.go` / `strategy_factory.go`：扩缩容策略与执行器。
- `config_example.go`：常见 Mailbox 配置示例。
- `../../../../docs/ACTOR_REVIEW_2026_04_29.md`：2026-04-29 复审、修复记录与 Sentinel 细粒度 resource 问题说明。



## 有界 Mailbox：配置、观测与回滚

2026-10-30 用户明确批准默认采用 `bounded`（MutexRing），暂停 RW 实验。nil/零值配置、未指定后端以及 dual/priority 构造路径一致采用 bounded；显式 `unbounded` 可回退，不新增开关。历史 Task 9 性能门槛仍未通过，此次是采用决策，不是生产性能验收通过声明。生产 SpinBackoff 保持，实验 B/C 不晋升。证据见 [报告](../../../../task-2-report.md)。配置位于服务的 `Mailbox`（Go 类型 `MailboxConf`）下；可运行的解析→工厂→Worker 行为测试见 `queue_config_integration_test.go`。

```yaml
Mailbox:
  QueueBackend: "bounded" # 省略时相同；回退须显式 unbounded 并重启
  # MaxQueueStorageBytes: 67108864 # 默认布局预算，不是 RSS 上限
  # DualQueueCapacity:
  #   System: 4096
  #   User: 4096
```

显式选择 MutexRing 的 dual 示例（合并到服务配置，不修改本地环境文件）：

```yaml
Mailbox:
  QueueMode: dual
  QueueBackend: bounded
  MaxQueueStorageBytes: 67108864
  DualQueueCapacity: {System: 64, User: 4096}
  SchedulePolicy:
    InitialWorkerNum: 1
```

priority 使用相同 `QueueBackend: bounded`，将 `QueueMode` 改为 `priority`，并在 `SchedulePolicy.MultiLevelQueueConf.PriorityBatches` 的 `"-3"`、`"-2"`、`"-1"` 以及至少一个用户优先级（如 `"0"`）中分别设置 `Capacity`；完整解析样例见上述集成测试。

- `QueueBackend` 仅支持 `unbounded` / `bounded`；省略或空字符串使用 bounded。
- bounded 容量按 **Worker、物理子队列** 独立计算：dual 的 System/User 各默认 4096；priority 默认六条队列各 4096。缺失或零容量取 4096，非零容量必须是至少 2 的 2 次幂。priority 未配置映射时使用六级默认映射；自定义映射仍须包含三个系统优先级及至少一个用户优先级。负数、非法容量、预算不足在启动时失败，不静默降级。`MaxQueueStorageBytes` 为零/省略时采用 64 MiB。旧 `DefaultUserMailboxSize` / `DefaultSysMailboxSize` 不参与此默认值。
- 无 error 返回值的公开队列管理器构造器使用同样的 bounded 校验，非法配置会 panic；应用建议通过返回 error 的 `NewMailbox`/`NewWorkerPool` 构造。
- 自动扩容开启时按最大 Worker 数预估队列布局预算，关闭时按初始 Worker 数预估。`MaxQueueStorageBytes` 不覆盖 payload、RW readCh、pending、执行中 Job 等，不是 RSS 上限。
- 物理主队列满立即返回 `ErrMailboxWorkerIsFull`，不阻塞等待容量。接收成功不保证执行：RW 二级 readCh 满可异步丢弃。hash ring 无可用 Worker 也可能返回同一错误，但不是物理队列满。
- StopTimeout 不是整个 Stop 的硬期限；admission gate 和执行用户 Job 可延长停止。重试必须遵守业务幂等、原有期限和有界退避，禁止盲目即时重试。

### 抓取指标

只导出跨 Mailbox 全局聚合，无 worker ID、dispatcher key、service 或 priority 标签：

| Prometheus 指标 | 类型 | 语义 |
|---|---|---|
| `ember_mailbox_queue_full_rejected_total` | counter | Worker 主队列提交满拒绝，是 DispatchFailedTotal 的子集；不含 hash ring 失败 |
| `ember_mailbox_read_queue_full_discarded_total` | counter | RW readCh 满异步丢弃，不可与前项合并为 PostJob 同步拒绝率 |
| `ember_mailbox_queue_length` | gauge | 每条物理主队列 max(0, Len) 后求和；并发近似、静止准确 |
| `ember_mailbox_queue_capacity` | gauge | 非空且全部可观测 Mailbox 为 bounded 时才输出全局主队列容量 |
| `ember_mailbox_queue_observed_high_watermark` | gauge | ServiceManager 对每次当前全局长度取历史最大值，不是精确生命周期峰值，不相加各 Mailbox 历史峰值 |
| `ember_mailbox_queue_backend_info{backend="bounded\|unbounded\|mixed"}` | gauge | 实际标签值为三者之一，值 1；无可观测 Mailbox 时省略 |

长度/容量不含 readCh、pending 或执行中 Job。单 Mailbox 的 `QueueBounded=false` 时容量为 0；mixed 时省略全局 capacity，不能把局部 bounded 容量当全局容量。无可观测 Mailbox 时长度为 0，backend_info/capacity 均省略。峰值仅在指标/调试抓取时更新，缩容不清除，可能高于缩容后容量。主队列和 RW 满载累计计数由 Pool 持有，扩缩容不丢失；跨 Service 聚合仍按存活服务求和，服务移除可能使累计值下降，不保证跨 Service 生命周期单调。

### 采用决策与回滚

本次默认切换验证：mailbox/config/core 以及 actor/services/metrics 普通回归通过；默认值、队列配置及 Mailbox 确定性验收的定向 race 通过。RPC 定向 race 仍在既有 `utils/pool/pool.go:333–334` 的 `PerPPoolWrapper.Put` 报告竞争，未掩盖、跳过或修复为通过；普通 RPC 验收通过不代表完整 race 验收。IDE 仅报告既有 go.mod 的 x/sys direct/indirect 提示。

历史策略要求 Task 9 正确性及生产性能门槛全部通过后才灰度；2026-10-30 用户明确批准此次默认切换，取代该默认值变更限制，但不改变历史性能失败结论。默认主队列现在会满拒绝，调用方必须处理背压。部署仍需观察主队列拒绝、RW 二级丢弃、抓取峰值、RSS、GC pause、业务 p99/p999、重试率；异常时显式回退。

下方多轮实验记录保留原始结论。`.acceptance-evidence`、RW 诊断测试及 overlay 是本地实验材料，不随默认切换提交，也不承诺仅克隆仓库可完整重现这些历史实验；正式集成仅包含独立的 bounded/RPC 确定性验收测试。

回滚不需要二进制回滚：将 `QueueBackend` 改回 `unbounded`，然后重启服务。配置不支持运行时热切换，避免迁移在途槽位。

### 2026-10-29 本机过载背压验收

基线 `f9b9c9c`，模块 `/v2`；只补测试，未修改生产实现、队列算法或 per-P 同步。新增 `bounded_acceptance_test.go` 通过阻塞消费者确定性制造 Dual/Priority 满载，检查拒绝错误、discard → complete → Release 顺序、精确一次释放；RW 测试分别检查主队列接收成功和 readCh 溢出的异步错误；并发投递/BeginStop 覆盖 DrainExecute、DrainDiscard 及停止后拒绝。

`engine/pkg/core/rpc_bounded_acceptance_test.go` 使用实际 MessageBus → local sender → Service → Mailbox → handler → response → monitor 链路：两个排队请求占满容量后拒绝下一请求，验证拒绝返回、已接受请求响应、容量恢复与所有 waiter 删除。通道控制步骤，每次等待上限 2 秒；不访问远端。此处验证同步 Call，不涵盖远程传输或异步 RPC callback。

从仓库根目录执行，以下命令均退出 0：

```powershell
go test ./engine/pkg/actor/mailbox ./engine/pkg/core -count=1 -timeout=120s
# mailbox 8.698s；core 2.829s
go test -race ./engine/pkg/actor/mailbox -run '^TestMutexRingMailboxAcceptance' -count=20 -timeout=120s
# 9.925s，只代表隔离的 Mailbox gate
go test ./engine/pkg/core -run '^TestLocalRPCBoundedOverloadCompletion$' -count=20 -timeout=30s
# 2.230s
```

**性能证据（有限负载，不是性能门槛通过声明）**：Windows amd64、Ryzen 7 5700X、Go 1.24.9、GOMAXPROCS=4。复用现有 `TestMailboxPerformanceSample`，三轮 unbounded/bounded 配对；每场景每轮 65,536 个投递，相同工作量、调度和容量配置，默认单窗口协议。steady 每毫秒 32 个、单 worker/hot key；overload 四 worker、一次突发、CPU work=16384。吞吐为成功执行数/投递开始至全部释放的 wall time；p99/p999 从 PostJob 前至 invoker 工作结束，仅统计成功执行，不代表 RPC 或 Release 完成延迟。拒绝不计入成功吞吐或延迟。所有样本 Drained=true，无 RW/其他拒绝。

| 场景 / backend | 成功吞吐范围（条/s） | p99 范围（ms） | p999 范围（ms） | 每轮主队列拒绝 |
|---|---:|---:|---:|---:|
| Dual steady / unbounded | 32004 | 0.044–0.638 | 0.158–1.483 | 0 |
| Dual steady / bounded | 31786–32004 | 0.026–62.779 | 0.077–62.954 | 0 |
| Priority steady / unbounded | 32004 | 0.026–0.034 | 0.064–0.133 | 0 |
| Priority steady / bounded | 31949–32004 | 0.023–28.218 | 0.061–65.807 | 0 |
| Dual overload / unbounded | 204624–213987 | 266.326–279.895 | 270.902–289.819 | 0 |
| Dual overload / bounded | 32193–221352 | 80.677–673.096 | 83.538–677.714 | 37198 / 45708 / 48036 |

steady 吞吐受投递速率限制，不是最大处理能力；尾延迟波动显著，不能宣称 bounded 更快或达到生产性能目标。单窗口 CPU 时间过短，虽 fixture 标记 CPUResolved，也不据此比较 CPU 效率；本次不是既有 fixed128 正式性能门禁。

内存复用 `TestMailboxPerformanceMemory`：一个 worker 阻塞，分八批投递共 32,768 个等待任务，每批强制 GC 后测 retained Go heap（非 RSS）。bounded 队列停在 4096，执行 4097、拒绝 28672；unbounded 队列增长到 32768，执行 32769、拒绝 0。两者均验证每个任务的执行/拒绝/释放守恒与恢复。bounded HeapAlloc 从初始 2,714,368 B 到首个满队列 3,238,752 B、最终 3,244,416 B；unbounded 从 2,594,624 B 增至 7,574,560 B。这里只说明这个固定载荷下的队列积压限制，不保证整个进程内存上界。

复现命令（原始 JSON 保存在根目录 `.acceptance-evidence`；Round 字段由旧 fixture 固定为 0，轮次以文件名为准）：

```powershell
New-Item -ItemType Directory -Force .acceptance-evidence | Out-Null
$root = (Get-Location).Path
$env:GOMAXPROCS = '4'
$env:MAILBOX_PERF_SAMPLE = '1'
$env:MAILBOX_PERF_PROTOCOL = ''
foreach ($round in 1..3) {
    foreach ($candidate in @('unbounded', 'bounded')) {
        $env:PERF_CANDIDATE = $candidate
        $env:PERF_OUTPUT = "$root\.acceptance-evidence\$candidate-$round.json"
        go test ./engine/pkg/actor/mailbox -run '^TestMailboxPerformanceSample/(dual-w1-rwfalse-hottrue-steady|priority-w1-rwfalse-hottrue-steady|dual-w4-rwfalse-hotfalse-overload)$' -count=1 -timeout=90s
        if ($LASTEXITCODE -ne 0) { throw 'sample failed' }
    }
}
$env:MAILBOX_PERF_SAMPLE = ''
$env:MAILBOX_PERF_MEMORY = '1'
foreach ($candidate in @('unbounded', 'bounded')) {
    $env:PERF_CANDIDATE = $candidate
    $env:PERF_MEMORY_OUTPUT = "$root\.acceptance-evidence\memory-$candidate.json"
    go test ./engine/pkg/actor/mailbox -run '^TestMailboxPerformanceMemory$' -count=1 -timeout=60s
    if ($LASTEXITCODE -ne 0) { throw 'memory sample failed' }
}
Remove-Item Env:GOMAXPROCS, Env:MAILBOX_PERF_SAMPLE, Env:MAILBOX_PERF_MEMORY, Env:MAILBOX_PERF_PROTOCOL, Env:PERF_CANDIDATE, Env:PERF_OUTPUT, Env:PERF_MEMORY_OUTPUT -ErrorAction SilentlyContinue
```

**结论与未关闭项**：本次有限范围内的背压/所有权/本地同步 RPC 正确性验收通过；生产性能验收与灰度禁令没有解除。原生 per-P `-race` 告警仍未解决，未加锁或 race 注解，不将 Mailbox 的隔离 race 成功扩展为完整 RPC/全仓 race 通过。未测试远端，未修改共享数据。

### 2026-10-29 尾延迟控制实验与正式门禁复核

证据目录：`.acceptance-evidence/investigation-20261029/`。主目录分支 `v2-feat/complete-framework`，HEAD `f9b9c9cc944b9609ae76a5f1d09db010830cba78`，Windows/Go 1.24.9、GOMAXPROCS=4；CPU 型号和 commit 已写入新 JSON。未修改生产代码、性能 fixture、阈值或先前验收测试；未 commit/push。

**有界实验**：8 次独立进程，顺序 U/B/B/U/U/B/B/U，每次三个原场景、60s 超时；随后 B/U 的 CPU+mutex+block+runtime trace，以及 B/U/U/B 的 trace+mutex（40s 超时）。U=unbounded、B=bounded。诊断运行与正式样本分开，不混合计算分位数。

| 4 次控制样本范围 | Dual steady p99 ms | Priority steady p99 ms | overload 成功吞吐 /s |
|---|---:|---:|---:|
| unbounded | 0.564–75.027 | 0.047–48.680 | 83787–176973 |
| bounded | 0.019–6.170 | 0.020–26.218 | 6408–234940 |

控制样本 steady 均无丢失。bounded overload 的异常样本 control-5 p99=2547.815ms、拒绝49152，其他三次 p99=71.802–76.109ms；不是稳定的队列性能估计。**高尾延迟并非 bounded 独有**，AB/BA 已否定“仅 bounded 才抖动”的现象假设，但没有证明操作系统是唯一原因。

**可归因证据与边界**：fixture 使用绝对 deadline 的32 jobs/ms catch-up；生产者落后时会连续补投，不是每毫秒最多32个的平滑限速。计时在 PostJob 前开始，不包含此前等待 deadline 的时间，因此生成器落后可能通过补投形成队列等待，不能把 p99 直接解释为队列锁等待。该行为是正式协议显式约定，不是本轮发现后应悄悄修改的 harness bug。

诊断 B 样本 p99=0.0487ms，mutex 总累计0.525ms，其中 runtime lost contention 0.483ms、idle Wake 0.0379ms、bounded Queue.Empty 0.0040ms；未见支持几十毫秒队列锁竞争的证据。trace-only 两个 B p99=0.0309/0.0274ms，U=0.0323/0.1234ms；同组 mutex 总累计分别0.220/0.300/0.575/0.445ms。U trace-2 调度等待累计44.69ms（不是单次延迟），最长记录到的 Runnable 等待1.176ms；GC STW 开始/结束原始时间见 `trace-2-stw.txt`（展示的 GC pause 约18–29µs）。这些**正常诊断窗口没有捕获控制实验的长尾事件**，不能外推排除异常窗口的锁竞争、GC、Windows 抢占或外部负载。CPU+trace 会明显扰动 QPC/cgo 采样路径，其 pprof 仅用于定位，不用于性能门禁。Go test 成功运行时日志还包含编译/vet GC，不能当成被测进程 GC；这里以 runtime trace 为准。未闭环：异常事件同窗的投递迟到量、队列水位与线程调度关联；本轮预算用尽后未无限重跑。

**原正式门禁与实际执行**：`tools/mailboxperf/run.ps1` 的 V3 最小完整矩阵是15场景 × 3候选（73ecd28 baseline/U/B）× 5轮=225条。要求 metadata/守恒/drain/CPU有效；非 overload 必须零丢失，median 吞吐比≥0.95、CPU/success≤1.10、p99≤1.10、p999≤1.15，mostly-empty 的 CPU 比≤1.10；overload 仅描述。fixed128 是另一个预注册单场景CPU实验（128窗、每行8388608次、CPU≥3.125s、五轮、CPU比≤1.10），不能替代全矩阵，也未重启历史一次性预算。

本轮申请创建独立73ecd28 baseline worktree 的执行被工具权限拒绝（非编译失败），没有绕过权限、没有写 `.worktrees`。因此只运行可执行的 **15场景 × U/B × 5轮=150条**，顺序逐轮 AB/BA，单进程120s超时；原始日志/JSON和 `matrix-attempts.txt` 全部保留。U五轮 exit=0，B五轮 exit=1。B每轮四个RW steady场景均出现主队列拒绝：均匀key每行1398–3821，hot key每行31669–33695；RWDropped=0。这是已证实的物理主队列容量/消费积压导致的零丢失条件失败，不是仅仅比率噪声；尚未证实RW消费不足的更深层根因，不能归咎于锁。完整明细见 `matrix-summary.csv`。

执行原 SummarizeOnly 后明确拒绝：`Incomplete corrected matrix: need 225 observations`（`formal-gate.txt`）。**正式门禁不通过：既缺75条baseline，又已有20条bounded非overload丢失；不报告通过率或伪造baseline比率。** 即使补齐baseline，现有丢失也不能满足原门禁。V3短CPU窗口还存在Windows计时量化局限，不能据此替代fixed128 CPU结论；生产/灰度禁令保持。

本轮关键命令（环境及候选如上，所有输出落独立证据目录）：

```powershell
# 控制：每个候选独立进程，按 U/B/B/U/U/B/B/U 顺序
 go test ./engine/pkg/actor/mailbox -run '^TestMailboxPerformanceSample/(dual-w1-rwfalse-hottrue-steady|priority-w1-rwfalse-hottrue-steady|dual-w4-rwfalse-hotfalse-overload)$' -count=1 -timeout=60s
# 矩阵：每候选五轮，MAILBOX_PERF_SAMPLE=1，MAILBOX_PERF_PROTOCOL=''
 go test ./engine/pkg/actor/mailbox -run '^TestMailboxPerformanceSample$' -count=1 -timeout=120s
# 诊断：同一priority场景，另设 PERF_OUTPUT，不混入矩阵
 go test ./engine/pkg/actor/mailbox -run '^TestMailboxPerformanceSample$/^priority-w1-rwfalse-hottrue-steady$' -count=1 -timeout=40s -trace=<evidence-path> -mutexprofile=<evidence-path>
 .\tools\mailboxperf\run.ps1 -SummarizeOnly -InputPath .acceptance-evidence\investigation-20261029\matrix-all.json -SummaryDirectory .acceptance-evidence\investigation-20261029\matrix-summary
# 正确性复核（清除 opt-in 环境后），两包通过
 go test ./engine/pkg/actor/mailbox ./engine/pkg/core -run 'Test.*Bounded|TestMailboxPerformanceComparison|TestMailboxPerformanceRejectsUnresolvedCurrentLatency' -count=1 -timeout=120s
```

最终正确性日志 `correctness.txt`：mailbox 2.941s、core 2.245s，通过；这不是全仓或race验收。新增内容仅本节和独立证据目录（JSON、日志、trace/profile、CSV）；已移除诊断测试二进制和失败的临时解析脚本。用户既有配置与修改保持不变。

### 2026-10-29 RW 消费积压闭环补充

本次使用 systematic-debugging / GitNexus 代码追踪及独立诊断测试，未改变生产符号、原性能 fixture、容量或门槛。GitNexus `query RW`、`context execWrite` 指向 `execWithRW → dispatchRead → runReadPipeline → launchRead` 与 `execWrite`；新增 `TestMailboxRWBacklogDiagnostic` 在编辑前查询 impact，因尚未入索引返回 NOT FOUND / UNKNOWN（不是 LOW 风险结论）；没有已有调用者或生产执行流程被修改。新增测试仅显式 `MAILBOX_RW_DIAGNOSTIC=1` 执行。

**实证结论：原 steady 的 32,000/s 对本机该 RW 混合热 key 路径是持续过载，而不只是 catch-up 突发。** 同一个 hot key 只落一个 worker；每四个请求包含一个写、三个读。写必须先等先序读注册，再等待全 Service 读写锁；两个等待点均采用 `SpinBackoff`（首次 Gosched，随后 Sleep），主循环此时不消费主队列。轻量工作仅64次整数运算，RW 协调/等待成本相对突出。四 worker 不意味着同 key 四倍消费能力。readCh 很浅与主队列满并不矛盾，不能用 RWDropped=0 推断消费足够。

新诊断维持 65,536 次、dual、4 worker、容量4096、工作64、75%读、GOMAXPROCS=4；只在各对照中改变所列因素。预建 Job，无原256次串行 warmup；每批记录主队列、readCh、读注册计数、释放数、投递迟到量，因此不是正式样本。两个独立进程各运行五个对照，75s 超时；另一个45s trace进程捕获原速率 bounded/unbounded。未重启 fixed128 或创建 baseline worktree。

| 对照，两轮 | 投递时间 s | 成功 / 主拒绝 | p99 ms | 投递结束后的排空 ms |
|---|---:|---|---:|---:|
| 原32/ms + RW bounded | 2.047 / 2.047 | 33741/31795；32588/32948 | 306.769 / 303.260 | 284 / 309 |
| 每批实际开始后1ms再投，不追赶 | 2.080 / 2.101 | 34362/31174；39491/26045 | 298.261 / 301.250 | 286 / 231 |
| 仅改8/ms，RW bounded | 8.191 / 8.191 | 各65536/0 | 1.002 / 1.002 | 2.5 / 1.5 |
| 仅关闭RW，32/ms bounded | 2.047 / 2.047 | 各65536/0 | 0.020 / 0.020 | 1.5 / 1.5 |
| 仅改unbounded，32/ms RW | 2.047 / 2.047 | 各65536/0 | 3296.335 / 2371.379 | 3330 / 2168 |

全部守恒、全部释放、二级丢弃0。原bounded采样主队列峰值4096，在206/219ms即达到4090以上；最大投递迟到仅0.85/0.50ms。取消catch-up后仍于242/226ms达到4090，故不能把持续拒绝归因于追赶。unbounded主队列峰值39295/39299，结束投递时仅完成26209/26208；bounded释放数包含拒绝，不能直接当消费数。8/ms主队列峰值7/8，关闭RW峰值32/0。降低诊断输入只用于确认负载边界，**不替代原门禁**。

**同窗归因与异常长尾边界**：`rw.trace` 中 bounded p99=305.760ms、拒绝31661，unbounded p99=3140.674ms，重现积压窗口。逐goroutine状态与阻塞入口栈解析：`execWrite` 第777行（先序读注册）Waiting累计1738.679ms/4565次，第793行（TryLock退避）累计5551.247ms/13746次；两个串行子场景合计7.58s wall，等待累计约7.29s。最大单次等待分别1.322/2.146ms；所有观测到的 Runnable 间隔最大1.679ms，无超过10ms的 Runnable 等待。这里是重复短等待累积成队列长尾，不是一次300ms或3s的锁/调度停顿；Sleep等待包含计时器与运行时唤醒，不能据此唯一归咎Windows计时器。分析不把后台goroutine长时间等待当请求延迟，未把重叠goroutine时间当CPU时间。

这解释了RW主队列积压机制与本机负载越界，未发现丢任务、死锁或生成器违反已约定协议的真实正确性bug，因此没有臆测性生产修复。退避策略是可见的吞吐限制，但替换为阻塞锁/通知会涉及停止、动态RW切换和读写顺序，不能仅凭性能不足认定实现不正确。均匀key还含跨worker共享锁竞争，本轮不声称已量化其各项占比。此前非RW控制中的75ms、overload 2548ms异常未在本次同窗重现；本次证据不能解释这些独立异常，也不排除外部调度/GC/竞争。调查到此有界停止，不无限追逐噪声。

**交付与复现**（根目录，文件均在 `.acceptance-evidence\investigation-20261029`）：

```powershell
$env:GOMAXPROCS='4'
$env:MAILBOX_RW_DIAGNOSTIC='1'
$env:RW_DIAGNOSTIC_OUTPUT="$PWD\.acceptance-evidence\investigation-20261029\rw-control-a"
go test ./engine/pkg/actor/mailbox -run '^TestMailboxRWBacklogDiagnostic$' -v -count=1 -timeout=75s
# 第二进程换 rw-control-b；两次均 exit 0，23.007s / 21.408s
$env:RW_DIAGNOSTIC_OUTPUT="$PWD\.acceptance-evidence\investigation-20261029\rw-trace"
go test ./engine/pkg/actor/mailbox -run '^TestMailboxRWBacklogDiagnostic$/^(original|unbounded)$' -v -count=1 -timeout=45s "-trace=$PWD\.acceptance-evidence\investigation-20261029\rw.trace"
# exit 0，9.744s；trace必须用绝对路径
node .acceptance-evidence\investigation-20261029\analyze-rw-trace.cjs .acceptance-evidence\investigation-20261029\rw.trace .acceptance-evidence\investigation-20261029\rw-trace-states.json
Remove-Item Env:MAILBOX_RW_DIAGNOSTIC
Remove-Item Env:RW_DIAGNOSTIC_OUTPUT
go test ./engine/pkg/actor/mailbox ./engine/pkg/core -count=1 -timeout=120s
# exit 0：mailbox 7.735s / core 1.916s；不是race验收
.\tools\mailboxperf\run.ps1 -SummarizeOnly -InputPath .acceptance-evidence\investigation-20261029\matrix-all.json -SummaryDirectory .acceptance-evidence\investigation-20261029\matrix-summary
# exit 1：Incomplete corrected matrix: need 225 observations
```

JSON 保存每批边界点，`rw-boundaries.csv` 保存峰值/迟到/排空汇总，`rw-trace-states.json` 保存trace等待汇总；首次原fixture重现日志为 `rw-original.txt`（exit 1，31036拒绝）。该次相对trace路径未生成可读文件，不能作为trace证据；后来绝对路径已成功重做。新增测试通过编译、gofmt及IDE诊断。**正式门禁仍失败：150/225，缺75条授权baseline，且已有20条bounded RW steady丢失；本次再次复核失败，生产/灰度禁令不解除。** 没有修改用户指令、配置、enhanced_sender_test.go或旧工作树，没有commit/push。

### 2026-10-29 分段等待、速率阶梯与等比例排列实测

独立证据：`.acceptance-evidence\rw-followup-20261029`。新增 `mailbox_rw_staircase_test.go`，不修改生产代码、此前诊断或正式门槛。使用 systematic-debugging、golang-testing、TDD；顺序生成器先失败（`red.txt`: `rrrw length=0`）后通过（`green.txt`）。GitNexus 对新增诊断符号查无结果，风险为 **UNKNOWN，不能据此声称零调用/零风险**；既有 `execWrite` upstream depth=1 报告2个直接调用（execWithRW、launchRead，置信度0.57）、0条索引流程、LOW，仅作上下文，不修改它。

**协议**：4P、4 worker、hot key、75%读、固定work=64；每档名义2秒，1ms批投递、绝对时钟catch-up，最多64000任务，投递5秒保护、排空10秒保护、进程180秒超时。先bounded→unbounded（AB），再unbounded→bounded（BA），不并行压测。每10ms保存主队列/readCh/注册进度/既有写锁等待累计；采样是非原子快照，不是精确水位最大值。成功仅计handler执行，投递结束完成数从释放数减去拒绝；全部52个非trace窗口共1536000任务，守恒、排空、二级丢弃0。实际投递约名义速率的100.04%，最大投递迟到4.0414ms。CPU增量0–125ms，52/52均未通过既有CPU分辨率判定，**不能宣称CPU改善或零CPU**。

| 输入/s | bounded拒绝（AB/BA） | unbounded投递末主队列（AB/BA） | 解释 |
|---|---:|---:|---|
| 4000 | 0/0 | 4/4 | p99约0.51ms |
| 8000 | 0/0 | 8/8 | p99约1.01ms |
| 12000 | 0/0 | 720/1467 | 临界区；bounded末队列44/16但中途峰值80/424 |
| 16000 | 3260/5290 | 8000/6168 | 持续积压；bounded p99 341/383ms |
| 24000 | 18438/19870 | 23772/25976 | bounded饱和不能用平坦水位误判稳定 |
| 32000 | 34093/35472 | 39444/41504 | unbounded排空3.419/3.387秒，p99 3.397/3.353秒 |

预算内细化10/11/13/14k：10–11k全部无拒绝、末队列8–24、排空1.5–3.5ms；13k末队列16–2720，波动明显；14k末队列2668–5683，0.5–1.9秒的端点水位斜率+1180至+3158任务/s、排空218–540ms。**本机短窗稳定容量边界位于11–14k/s，12–13k为噪声敏感过渡区，不是长期容量认证或精确单点**。未继续无限细化。

**等工作量排列对照**：16k/s、32000任务、24000读/8000写，仅改变排列；RRRW、每128个任务96R+32W、固定种子20261029随机打乱。切换次数分别15999/499/12059；AB/BA结果如下（均为完成任务p99，存在拒绝时有幸存样本偏差）：

| 排列 | bounded p99 ms（AB/BA） | unbounded p99 ms（AB/BA） | bounded拒绝 |
|---|---:|---:|---:|
| RRRW | 149.127/346.301 | 190.150/499.921 | 0/3308 |
| 批量 | 0.0336/0.0308 | 0.0350/0.0299 | 0/0 |
| 随机 | 2.520/2.509 | 2.528/5.505 | 0/0 |

批量/随机末队列10–21、排空1–2.5ms；RRRW末队列2188–5836。结果支持**读写排列/切换路径对积压有显著影响**，但不能拟合恒定“每次切换成本”：随机与批量也改变读并发、同毫秒内读写位置，批量还与16/ms投递边界对齐；没有反向排列次序复测，跨进程噪声仍在。阶梯和顺序试验的RRRW结果不同，已完整保留而非挑选。

**分段trace（另6窗口，不混入非trace汇总）**：`stages.trace` 使用测试侧region标识每个场景，`analyze.cjs` 按生产栈第777行注册等待、第793行TryLock等待及Runnable阶段分类，无生产探针。RRRW bounded/unbounded注册Waiting累计570.938/880.858ms，TryLock Waiting累计1437.884/1380.930ms；相关Runnable（含Gosched及等待唤醒后排队）分别46.027/43.290ms。随机注册648.415/729.578ms，TryLock887.153/969.012ms；批量注册仅0.111/0.049ms，未观察到TryLock Waiting。RRRW单次Waiting最大0.917ms、相关Runnable最大0.065/0.067ms量级。trace的RRRW p99为112/348ms，不能代替无trace主结果。

**必要等待 vs 额外退避：仍不可唯一拆分。** read job完成/Release发生在RUnlock之前，既有计数和trace均不标识最后一个读锁释放的精确时刻，也不记录每次TryLock结果；测试侧轮询或锁探针会扰动调度/锁竞争，因此未加入。上述Waiting只是退避阻塞区间总量，可作为其中“锁已可用而仍在睡眠”部分的上界；下界只能为0，不能把全部sleep算作额外成本，更不能将累计跨goroutine等待当CPU或端到端延迟。既有WriteWaitNS还包含运行/调度时间，仅作交叉观测。结论足以区分积压趋势与排列效应，但不能据此直接实现通知式等待。

复现（项目根目录；无需远端/依赖安装）：

```powershell
.\.acceptance-evidence\rw-followup-20261029\run.ps1 -Phase stairs
.\.acceptance-evidence\rw-followup-20261029\run.ps1 -Phase refine
.\.acceptance-evidence\rw-followup-20261029\run.ps1 -Phase orders
$env:GOPROXY='off'; $env:GOTOOLCHAIN='local'
$env:GOTMPDIR="$PWD\.acceptance-evidence\go-build"
$env:GOMAXPROCS='4'; $env:MAILBOX_RW_STAIRCASE='1'
$env:RW_RATES='16000'; $env:RW_ORDERS='rrrw,batch,random'; $env:RW_REVERSE='0'
$env:RW_STAIRCASE_OUTPUT="$PWD\.acceptance-evidence\rw-followup-20261029\trace"
go test ./engine/pkg/actor/mailbox -run '^TestMailboxRWStaircase$' -v -count=1 -timeout=90s "-trace=$PWD\.acceptance-evidence\rw-followup-20261029\stages.trace"
node .acceptance-evidence\rw-followup-20261029\analyze.cjs
Remove-Item Env:MAILBOX_RW_STAIRCASE
go test ./engine/pkg/actor/mailbox -count=1 -timeout=120s
```

`summary.json`含52窗口实际速率/成功/拒绝/p99/CPU/排空/水位斜率；每窗口JSON保存原始采样；`stages.json`保存分段trace汇总。验证：六组AB/BA进程和trace均exit 0；最后mailbox全包测试exit 0（5.524s，`correctness.txt`），IDE无错误，gofmt完成。**正式门禁未修改、未重新宣称通过；既有baseline权限阻塞与生产/灰度禁令保持不变。** 未访问远端、旧工作树或附件，未commit/push。


### 2026-10-29：RW 等待策略隔离 A/B（不默认采用）

**决策：保留生产 SpinBackoff，实现仅作为可撤销 overlay 实验保留。** 本轮不新增生产配置、不切换默认、不 commit/push。证据在 `.acceptance-evidence\rw-wakeup-20261029`；原 worker.go、rw_controller.go 无差异。

机制：B 使用真实 Worker/RWController，仅替换 writer 的前序读注册等待及 TryLock 失败等待。先订阅代际 channel，再检查状态；readsLaunched 注册及共享锁的 RUnlock/Unlock 广播通知。5ms timer 保留 closed 检查，避免停止时永久停车；不是阻塞 Lock。读 gate、readSem、Disable 退避及 drain 协议不变。共享广播和每次等待 timer 有分配/惊群成本，尚不能推广到所有负载。GitNexus execWrite 上游 5 符号、launchRead 2 符号，LOW；完整结果保存在 impact-*.json，无识别命名流程不等于无语义风险。

正确性新增 `mailbox_rw_wakeup_test.go`：真实读派发/注册、readSem=1、读写互斥、stop/drain、动态 disable、每个任务精确 Release；动态 disable 检查同 hotkey worker 契约，不错误要求关闭 RW 后跨 worker 全局互斥。候选专用 notification_test.go 覆盖订阅后停车前通知和代际更新、读写解锁通知。空 signal 负控因 `lost notification before wait` 失败；实现后新增和既有 TestRW_ 10 轮通过。两版全包测试通过；两版目标 `-race -count=2` 通过（9.236s/9.095s）。最终新增测试两版各 100 轮通过。测试不能证明任意调度下完全等价。

性能：4P、轻 handler、75% 读、hotkey；A 原实现、B 通知候选。每窗口输入 8s，AB/BA 两次，吞吐为成功数/包括 drain 的 Wall。64 个计划窗口中63个完成；失败窗口不排除或补跑掩盖。以下为两次观测范围（吞吐 k/s，P99及drain为ms，拒绝为每窗口计数）：

| 模式/后端/输入 | A吞吐 | B吞吐 | A拒绝 → B拒绝 | A P99 → B P99 | A drain → B drain |
|---|---:|---:|---:|---:|---:|
| RRRW/bounded/8k | 7.998–7.999 | 7.999 | 0 → 0 | 1.008–1.014 → .041–.045 | 1.50–2.50 → 1.50 |
| RRRW/bounded/12k | 11.996–11.998 | 11.999 | 0 → 0 | 7.011–13.010 → .055 | 2.00–3.50 → 1.50 |
| RRRW/bounded/16k | 13.043–13.943 | 15.998 | 12471–19079 → 0 | 332.788–343.789 → .068–.070 | 286–351 → 1.50 |
| RRRW/bounded/32k | 14.200–20.674 | 31.997–31.998 | 86552–137736 → 0 | 317.278–743.114 → .116–.119 | 197–329 → 1.04–1.50 |
| RRRW/unbounded/16k | 12.340–15.703 | 15.998–15.999 | 0 → 0 | 1232.990–2348.513 → .068–.069 | 152–2374 → 1.00–1.50 |
| RRRW/unbounded/32k | 18.791（仅1次完成） | 31.996 | 0 → 0 | 5492.203 → .115–.119 | 5624 → 1.50 |
| random/bounded/16k | 15.997 | 15.998–15.999 | 0 → 0 | 3.013–3.022 → .069 | 2.50 → 1.05–1.50 |
| random/bounded/32k | 20.368–21.188 | 31.997–31.998 | 81702–88705 → 0 | 218.246–240.721 → .108–.113 | 214–227 → 1.50 |
| random/unbounded/32k | 20.499–20.562 | 31.997–31.998 | 0 → 0 | 4403.305–4437.807 → .111–.112 | 4451–4489 → 1.04–1.50 |

batch 在16/32k、两种后端全部0拒绝，两版吞吐约15.998/31.997k/s，P99 A .030–.048ms、B .031–.046ms，没有稳定吞吐收益。RRRW 32k unbounded 的 A/BA 超过10s drain预算，仅释放229143/256000，保留 stairs-BA-A.txt；不能据其缺少JSON声称全部完成。其他有效窗口 Dropped=0，完成后释放数守恒。全部细项由 summarize.cjs 生成 summary.json，原JSON含P999/输入时长/最大提交迟到；最大迟到496.876ms，绝非硬实时均匀流量证明。

**CPU门禁未满足**：Windows进程CPU按15.625ms量化，既有有效性要求累计CPU >=3125ms。另做40s、16k bounded RRRW：A成功561601/640000、拒绝78399、P99 317.2695ms、drain307.764ms；B成功640000、拒绝0、P99 .0701ms、drain1.5011ms。CPU A578.125ms、B312.5ms，均 CPUResolved=false；65个有效窗口均不满足CPU有效性，不能宣称节省CPU或把墙钟变长视为达标。预算到此停止，不无限延长压测。

因果结论：仅 writer 等待变化即可大幅改善交错读写而 batch 无明显收益，强烈支持额外等待策略是此前拐点的重要来源；不能断言消除了全部必要锁等待。采用门禁仍缺有效CPU成本、广泛业务handler/多worker公平性及分配成本验证；本轮局部正确性/race通过不替代此前全局门禁。保留实验但不发布生产修复。

复现（仓库根目录，离线、本地Go，脚本设置GOMAXPROCS=4及仓库内GOTMPDIR）：

```powershell
.\.acceptance-evidence\rw-wakeup-20261029\verify.ps1
.\.acceptance-evidence\rw-wakeup-20261029\run.ps1 -Phase stairs
.\.acceptance-evidence\rw-wakeup-20261029\run.ps1 -Phase orders
node .acceptance-evidence\rw-wakeup-20261029\summarize.cjs
```

40s补测由 `node .acceptance-evidence\rw-wakeup-20261029\prepare-diagnostic.cjs --cpu` 生成诊断副本，临时复制为包内 rw_wakeup_experiment_test.go；设置 MAILBOX_RW_STAIRCASE=1、RW_RATES=16000、RW_ORDERS=rrrw、RW_REVERSE=0，分别执行 `go test -run '^TestMailboxRWWakeupExperiment$/^rrrw-16000-bounded$' -count=1 -timeout=120s`（B加 -overlay），RW_STAIRCASE_OUTPUT分别为证据目录 cpu-A/cpu-B，结束删除临时副本。不要同时运行这些复制临时测试的脚本。

### 2026-10-29：通知候选生产适用性验证（明确不采用）

**结论：不采用，生产仍 SpinBackoff；不进入条件性的本地 RPC 恢复/生命周期阶段。** 多 worker 局部延迟改善不能抵消尚未有效比较的 CPU 成本和明显增加的分配/GC。既有正式门禁未解除，未访问历史 baseline 工作树或远端，无生产配置/实现更改，无 commit/push。

证据 `.acceptance-evidence\rw-suitability-20261029`：预注册 `protocol.json`、隔离 `suitability_test.go`、`prepare.cjs`、`run.ps1`、`verify.ps1`、36 个最终非 race JSON、`summary.csv/json`、逐 worker 的 `workers.csv`、广播/timer 微基准及日志。GitNexus 实测 execWrite 上游 LOW：2直接调用/5符号/0索引流程；launchRead LOW：1/2/0。新测试未入索引，UNKNOWN，不等于无风险。仅两版 overlay 添加相同最大写锁等待探针，B controller 直接引用旧候选，未重生成/覆盖旧证据。

**协议/边界**：4P、bounded dual4096、readSem32，真实 mailbox PostJob 路由；均匀为每 worker 等量 key，热点约82.5%到 worker0、剩余均分。9个代表场景×AB/BA顺序反转（不是笛卡尔积）：1/4/8 worker，75/50/25%读，轻量64次、CPU32768次确定性运算、50µs Sleep handler；另8k/s限速控制。单 worker 被生产配置强制关闭 RW，作为不应受策略影响的对照。每窗1024请求后排空；最多256窗/262144请求，累计 active wall 达12s后不再开窗（允许当前窗完成，drain硬限5s）；累计有效进程CPU≥3.125s即停止。工作/测量间隙的CPU不计入，未靠无限延长低负载墙钟凑CPU。CPU窗口数不同，指标按成功数归一。吞吐包括投递及排空；波次饱和并非持续开放环系统极限，8k控制明显输入限速。

| 场景 | A→B吞吐(k/s) | A→B P99(ms) | A→B分配次数/成功 | A→B字节/成功 |
|---|---|---|---|---|
| 4W均匀75%读/轻量 | 23.80–27.03 → 480.59–481.95 | 42.89–46.89 → 1.97–2.05 | 6.278 → 12.438–12.443 | 111.1 → 652.5–652.9 |
| 8W热点50%读/轻量 | 20.94–21.58 → 405.46–406.42 | 48.33–50.35 → 2.124 | 5.609 → 8.849–8.863 | 87.8 → 382.6–383.8 |
| 8W均匀25%读/轻量 | 36.48–37.83 → 408.66–409.96 | 28.92–29.90 → 2.125–2.133 | 4.847 → 12.986–13.044 | 61.4 → 767.6–772.3 |
| 4W均匀75%读/CPU | 10.26–10.29 → 54.51–55.19 | 97.88–98.01 → 17.98–18.31 | 6.278 → 15.740–15.833 | 111.1 → 925.5–933.2 |
| 8W热点50%读/CPU | 8.19–8.23 → 40.62–40.66 | 122.03–122.94 → 24.54 | 5.610 → 12.002–12.003 | 87.9 → 651.1–651.2 |
| 4W均匀75%读/等待 | 3.59–3.60 → 4.90 | 281.54–281.58 → 206.48–206.49 | 6.285–6.287 → 16.169–16.220 | 111.6–111.8 → 966.3–970.6 |
| 8W热点50%读/等待 | 2.36 → 2.87–2.88 | 428.62–429.17 → 351.65–352.10 | 5.616 → 12.043–12.100 | 88.1–88.2 → 655.1–659.9 |
| 4W均匀75%读/8k限速 | 7.993–7.995 → 8.022–8.023 | .516–.517 → .050 | 6.278 → 7.435–7.443 | 111.0 → 218.4–219.2 |

**公平性不是总吞吐替代物**：36窗组所有worker成功数等于各自offered，零主拒绝/二级丢弃，精确一次Release，全部排空，无零进度worker。例B/AB/8W热点轻量：各worker成功 `[216320,6400,6656,6400,6656,6656,6400,6656]`，不是饥饿造成的不均匀。该场景A最大写锁等待17.51–22.52ms，B .612–.731ms；最大writer完成A56.31–61.36ms，B2.63–2.70ms。等待handler的8W热点仍有B最大锁等待41.54–46.56ms、最大完成间隔337.87–355.82ms（A205.18–264.23ms及539.96–612.02ms）；不能称为有界公平锁。每worker完整最大值在CSV；完成间隔包含波次间隙/不同输入量，不能单独判定饥饿；写锁探针使用生产time.Since，约0.5ms粒度，且不含前序读注册等待，writer端到端QPC延迟另报。有限波次未证明持续无限流无饥饿。

**广播及timer成本**：每次RUnlock/Unlock都走signal并获取eventMu，即使无订阅；有代际channel时关闭广播，无订阅时不创建channel。snapshot为后续代际分配。独立微基准（两轮200ms，非宏观吞吐门禁）：无订阅unlock 10.63–10.77ns/0分配；snapshot+signal+接收63.46–66.27ns/112B/1分配；已关闭通知仍调用wakeupWait 191.9–194.8ns/248B/3分配；timer单独134.5–138.7ns/248B/3分配。不能从无等待者微基准推出广播唤醒8个worker的成本。实际8W写多轻量的内存61.4→767.6–772.3B/成功，GC4→40–41次（同262144成功）；4W轻量GC6–8→41次。无效唤醒未直接计数：未改旧candidate signal/等待算法，不能拿锁失败或广播次数冒充无效唤醒数。handler CPU、调度、QPC采样及通知成本共同影响总数，不将全部差值归因于timer。

**CPU门禁未过**：仅B的4个CPU-handler行累计CPU达到3.125–3.1875s；全部A行不达标，36行中32行未达标。A CPU-handler累计仅31.25–171.875ms，尽管累计wall约12s；Windows计时量化/采样与稀疏短活动的偏差仍不可忽略，不能将这些值当成精确实际CPU开销，尤其不能报告A/B节省百分比。B有效行本身不构成有效配对。预注册预算到此停止，没有放宽CPU阈值、吞吐≥.95、CPU/成功≤1.10、P99≤1.10/P999≤1.15及非过载零丢失门槛，也未用本轮两次配对替代正式五轮。single-worker对照P99有噪声，AB从.0735到.1368ms、BA从.1202到.0714ms，没有一致收益。

**测量有效性与验证**：最初fixture在Start前读取未建立的worker快照触发panic（`fixture-red.txt`）；只修隔离fixture启动顺序。随后发现自定义job缺SetRWMode导致不能满足IRWModeJob，已添加编译期接口断言，原错误的全写结果移入`invalid-write-only`，不能用于结论。初版Go时钟有0延迟值，移入`coarse-clock`；最终用现有QPC采样并完整重跑。QPC每成功请求自身约4次/32B分配，对两版相同，表格保留而非扣除；因此不是纯业务分配。36最终场景全部执行通过；A/B已有TestRWWakeup及TestRW_各3轮通过（11.447/11.416s），B目标race通过（4.971s）；新fixture 8W热点轻量A/B race通过（7.666/2.320s），race输出不纳入性能汇总。包内临时文件均由finally删除，未留下服务/进程。

复现（根目录、禁止并发运行复制fixture脚本）：
```powershell
.\.acceptance-evidence\rw-suitability-20261029\run.ps1 -Phase light
.\.acceptance-evidence\rw-suitability-20261029\run.ps1 -Phase costs
.\.acceptance-evidence\rw-suitability-20261029\verify.ps1
node .acceptance-evidence\rw-suitability-20261029\summarize.cjs
```

### 2026-10-29 第一阶段 timer 实验（隔离 C，不替换生产 A）

证据：`.acceptance-evidence\rw-timer-20261029`，`summary.csv/json` 保留24个观测及每个原始JSON（含P999、GC pause、每worker最大等待/完成间隔）。A保持生产SpinBackoff；B保留原代际广播；C仅提前检查已关闭通知并复用等待timer，不新增配置、不改成固定channel。B的`signal`原本已有`changed == nil`不创建channel路径，本轮未重复优化它。

**影响及所有权**：修改前已保存GitNexus `impact-*.json`。execWrite直接调用者execWithRW、launchRead，5个上游符号，工具LOW；Worker影响Start流程，run直接调用者Start。隔离wakeupWait及fixture未入索引，返回UNKNOWN而非“无风险”；静态检查补齐其两个等待点。特别注意：不能把一个timer直接共享给整个Worker！RW关闭时readPipeline的launchRead降级也调用execWrite，可与main run并发。C按实际goroutine所有者分开mainWait/readWait，传入execWrite；Stop只由各自所有者执行，外部BeginStop/Wait不触碰timer；goroutine退出defer清理。timer每次等待后Stop，Stop=false时非阻塞drain，Reset前再清理，兼容Go1.24及asynctimerchan=1。直接测试调用execWrite可用局部owner，避免共享隐式timer。

**TDD及验证**：`red-fast.txt`记录B已通知仍3分配的确定性失败；增加提前返回后，`red-reuse.txt`记录重复等待timer身份改变的确定性失败。最终已通知首次不创建timer、冷/热快速路径0分配，复用后的超时等待0额外分配；重复超时、通知/超时竞争、Stop后无残留及重新等待不会误消费旧到期均通过。竞争循环是有界压力覆盖，不声称穷举调度。已有TestRWWakeupRegistrationAndExclusion、Lifecycle及TestRW_验证先读后写、互斥、stop/drain、RW关闭、精确一次Release。`final-green.txt`、`race.txt`、`final-legacy-race.txt`、`final-fixture-race.txt`通过；生产A正确性也通过。Go1.24.9 windows/amd64，4P。所有复制测试finally清理，无后台进程。

**相同负载及计时**：沿用1024-job确定性波次、真实Worker、QPC端到端延迟及相同max-lock-wait探针，ABC/CBA各一次；4W读多轻量、8W写多轻量、8W热点CPU、4W读多等待，共24观测。预算仍为最多256窗、累计wall12s或CPU3.125s先到者；最后波次允许完整排空。不增加压测轮次。不同停止条件下成功总数可不同，吞吐/分配归一化，GC原始次数不可直接跨不同成功量比较。QPC自身分配保留不扣除。

| 场景 | A kops/s；P99 ms；alloc/成功 | B kops/s；P99 ms；alloc/成功 | C kops/s；P99 ms；alloc/成功 |
|---|---|---|---|
| 4W均匀75%读轻量 | 22.23–24.31；45.37–48.40；6.28 | 407.66–489.59；1.85–3.28；12.29–12.44 | 486.84–538.93；1.50–2.02；7.38–7.39 |
| 8W均匀25%读轻量 | 34.74–39.44；29.88–31.87；4.85 | 389.78–406.11；2.20–2.48；12.95–12.98 | 451.25–451.60；1.92–1.99；5.98–5.99 |
| 8W热点50%读CPU | 8.22–8.24；122.45–123.54；5.61 | 40.47–40.82；24.44–24.75；12.03–12.04 | 41.03–41.12；24.28–24.34；6.80 |
| 4W均匀75%读等待 | 3.57；283.57–292.02；6.28 | 4.84–4.95；204.95–211.92；16.24–16.29 | 4.81–4.83；209.48–210.00；7.61 |

C相对B每成功分配减少约40–54%，字节减少约63–75%；4W轻量639.5–652.3→234.4–235.5B/成功、同262144成功GC40→13；8W写多764.9–766.9→188.2–188.8B/成功、GC43–47→9。热点CPU653.8–654.4→221.2–221.3B；等待972.3–977.0→260.2–260.4B。等待吞吐C/B最低约.975，其余场景均保留/提高B吞吐；P99/P999保留收益。24观测全部零拒绝、零二级丢弃，排空且Release恰好一次，各worker成功等于offered。

**独立CPU核查（未发现fixture截断bug）**：CPUTime直接将GetProcessTimes内核+用户100ns累计tick转duration，没有逐窗取整或负值截断；原fixture只累加相邻窗起止差。新fixture额外保存连续进程CPUStart/CPUEnd/ContinuousCPU及GapCPU，覆盖整个循环，包括窗间MemStats/统计边界。热点CPU的A两轮wall12.060/12.091s，分窗CPU46.875/250ms，连续起止也是46.875/250ms，gap均0；B为3.140625/3.125s，C为3.171875/3.140625s，连续与分窗也完全相同。故本轮证据排除“逐小窗差值截断/边界遗漏”解释，但不能证明Windows累计CPU对稀疏短活动准确；等待C甚至出现连续CPU=0，不可解释为免费执行。未伪造fixture失败测试或无证据修改CPU计量。连续计数校验保留原3.125s门槛，A没有有效CPU配对，不能报告A/B/C CPU节省百分比。

**采用决策：保留C为下一候选，不晋升生产。** 本轮功能及分配目标达成，B的吞吐收益保留，生产仍A。只有热点CPU的B/C形成两组有效CPU配对；ABC中C/B CPU/成功约1.120（超过原1.10门槛），CBA约1.067，因此即便B/C也不能宣称全门禁通过。两轮不替代正式五轮。未做固定channel第二阶段、未commit/push；旧证据、worktree和用户文件保留。

复现（根目录，复制fixture脚本禁止并发）：
```powershell
.\.acceptance-evidence\rw-timer-20261029\verify.ps1
.\.acceptance-evidence\rw-timer-20261029\verify.ps1 -Label race -Race
$env:GODEBUG='asynctimerchan=1'
.\.acceptance-evidence\rw-timer-20261029\verify.ps1 -Label legacy-race -Race
Remove-Item Env:GODEBUG
.\.acceptance-evidence\rw-timer-20261029\run.ps1
node .acceptance-evidence\rw-timer-20261029\summarize.cjs
```
