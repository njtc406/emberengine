# Actor 复审与修复记录 - 2026-04-29

## 范围

- 代码范围: `engine/pkg/actor/**`, 重点为 `engine/pkg/actor/mailbox/**` 与 `engine/pkg/actor/mailbox/job/**`。
- 验证命令: `go test -race ./engine/pkg/actor/...`, `go test` 定向覆盖 RW / middleware / job pool。

## 本轮决策

### RW 模式只支持配置启用

RW 读写分离需要 Worker 在创建时拥有 `readCh` 和 readPipeline。运行时从非 RW 模式动态开启时，已有 Worker 没有这些结构，读 Job 会进入 RW 分支后被当成 `readCh full` 丢弃。

本轮决策: RW 模式只通过 `MailboxConf.EnableRWMode` 在启动前配置启用；运行时允许 `SetRWEnabled(false)` 关闭 RW，用于维护或降级，但不支持再次动态开启。`SetRWEnabled(true)` 在当前未启用时返回 `ErrRWDynamicEnableUnsupported`。

### 多优先级队列保留插队语义

多优先级队列的目标是提供系统消息、紧急消息、普通业务之间的插队能力，不在框架层强制公平调度。高优先级流量如果长期过量，低优先级被延后是调用方的优先级设计结果。

使用建议: 系统级/紧急级优先级只用于控制消息、超时恢复、关键回调等短小任务；业务方应避免把常规高频请求长期标成高优先级。

### 停止流程允许阻塞以保证顺序

Node / Service 停止需要遵循先起后退，被依赖的基础服务必须后停止。`BeginStop` 可以阻塞等待 mailbox 内部后台流程按顺序退出，`Wait` 继续等待 worker drain 与资源释放。是否设置整体 timeout 应由上层 node/service 编排决定，而不是 mailbox 强制异步化。

## 已修复问题

### 1. RW Stop 等待 readPipeline 可能永久阻塞

问题: Stop 时先关闭 `readCh` 并等待 `readPipelineWg.Wait()`。如果 `readSem` 已被长读任务占满，readPipeline 在处理残留读 Job 时会一直等待令牌，导致后续 `inflightReads.Wait()` 的 `StopTimeout` 永远无法触发。

修复: `BeginStop` 设置统一停止 deadline。readPipeline 在 Worker 已关闭且超过 deadline 时，对尚未注册为 in-flight 的残留读 Job 走 unsafe discard，避免继续等待 `readSem`。外层等待 readPipeline 也增加 `StopTimeout` 兜底。

### 2. 中间件 panic 破坏 Job 收尾链路

问题: 自定义中间件的 `OnReceive` / `OnComplete` / `OnStart` / `OnStop` panic 会向外冒泡，可能导致 Job 不释放、middleware context 不归池、Sentinel entry 不 Exit。

修复: `MiddlewareChain` 增加统一 recover。`OnReceive` panic 转换为 Reject error，并触发 panic handler；`OnComplete` panic 被记录后继续执行剩余中间件并保证 context 归池；生命周期回调也被 recover 保护。`WorkerPool` 默认 panic handler 打印 service、phase、middleware、panic 和 stack。

### 3. `waitInflightReadsDone` 超时后等待 goroutine 泄漏

问题: 旧实现为等待 `WaitGroup.Wait()` 创建 goroutine，超时返回后 goroutine 仍可能长时间阻塞，并持有 snapshot / worker 引用。

修复: 改为轮询每个 Worker 的 `inflightReadCnt`，到 0 后同步 `Wait()` 收口，不再创建不可取消的等待 goroutine。

### 4. Job 池 debug 后开启无效

问题: Job 池第一次初始化时固定选择 stats recorder。若池先在 release/no-debug 状态初始化，再调用 `SetDebug(true)`，后续 Get/Put 仍不会计数。

修复: `pool` 包新增 `NewSwitchableStatsRecorder`，Job 池始终注册可开关 recorder。`SetDebug(true)` 后未来的 Get/Put 会开始计数，`SetDebug(false)` 后停止计数且全局 stats 输出过滤空字符串。

## 已确认但保留的设计

### 多优先级 weighted/fairness 不作为本轮框架层修复

当前多优先级队列本质是插队机制。框架不强行把高优先级流量拆成公平读取，因为这会削弱系统消息/紧急消息的语义。后续如果需要业务级公平，可新增独立策略，例如 `aging` 或 `quota`，由配置显式选择。

### WorkerPool.BeginStop 保持可阻塞

保持可阻塞是为了配合 node 级先起后退的停止编排。文档已更新，避免继续称其为非阻塞接口。

### 5. Sentinel 细粒度 resource 规则不匹配

问题: `NewSentinelMiddlewareWithJobType(serviceName, ...)` 通过 `WithResourceFunc` 把运行时 resource 变成 `serviceName:jobType`，但旧实现只把 flow / circuit breaker 规则加载到 `serviceName`。Sentinel 按 resource 精确匹配规则，导致 `battle:1` / `battle:3` 这类真实入口不会命中 `battle` 上的规则。

修复: `SentinelMiddleware` 新增完整的 resource 规则模型。

- `WithFlowRule` / `WithCircuitBreakerRule` 仍表示 service 级默认规则；当声明了细粒度 resource 时，会复制到每个声明 resource。
- 新增 `WithResourceFlowRule` / `WithResourceCircuitBreakerRule` / `WithSentinelFlowRulesForResource` / `WithSentinelCircuitBreakerRulesForResource`，支持直接按 resource 注册专属规则。
- 新增 `WithJobTypeFlowRule` / `WithJobTypeCircuitBreakerRule` / `WithJobTypeErrorCountRule` / `WithJobTypeSlowRatioRule`，支持按 `serviceName:jobType` 注册专属规则。
- `NewSentinelMiddlewareWithJobType` 自动声明所有内置 JobType resource，并通过统一 helper 生成运行时 resource，保证 `OnReceive` 的 `sentinel.Entry(resource)` 与 `OnStart` 的 `LoadRulesOfResource(resource, rules)` 使用同一命名规则。
- `OnStop` 遍历完整 resource 集合，对 flow / circuit breaker 规则做对称清理。
- 若用户直接使用自定义 `WithResourceFunc` 但没有声明任何 rule resource，启动时打印 warn，提示规则只作用于 `serviceName`。

### 原现象

`NewSentinelMiddlewareWithJobType(serviceName, ...)` 会通过 `WithResourceFunc` 把运行时 resource 变成 `serviceName:jobType`。例如 serviceName 为 `battle`，RPC JobType 为 `1`，`OnReceive` 调用的是:

```go
sentinel.Entry("battle:1", sentinel.WithTrafficType(base.Inbound))
```

问题根因在于 `OnStart` 加载规则时把 flow / circuit breaker 规则注册到 `m.serviceName`，也就是:

```go
flow.LoadRulesOfResource("battle", rules)
circuitbreaker.LoadRulesOfResource("battle", rules)
```

结果是规则挂在 `battle`，流量进入的是 `battle:1`，二者不是同一个 resource。Sentinel 按 resource 精确匹配规则，因此 `battle:1` 不会命中 `battle` 上的 QPS / 熔断规则。

### 使用例子

期望: `battle` 服务里 RPC 消息限流 1000 QPS，Timer 消息限流 100 QPS。

```go
mw := NewSentinelMiddlewareWithJobType("battle",
	WithJobTypeFlowRule(def.MailboxJobTypeRpc, 1000),
	WithJobTypeFlowRule(def.MailboxJobTypeTimer, 100),
	WithJobTypeErrorCountRule(def.MailboxJobTypeRpc, 20),
)
```

这会分别加载规则到 `battle:1`、`battle:3` 等真实 runtime resource。若使用 `WithFlowRule(1000)`，该默认规则会复制到 `battle` 以及所有内置 JobType resource，适合所有 JobType 共用同一阈值的场景。

### 已消除的风险

- 用户以为已经按 JobType 限流，线上实际没有生效。
- 熔断规则同样不会按 JobType 生效。
- `OnStop` 清理的是 `m.serviceName`，即使未来手动加载了 `battle:1` 规则，也不会被当前中间件对称清理。

## 回归测试

- `TestRW_SetRWEnabledDisableOnly`: 验证 RW 可关闭、不可动态开启。
- `TestRW_DrainInflightTimeout`: 覆盖 readSem 满载 + readCh 残留读 + StopTimeout。
- `TestMiddlewareChainRecoverOnReceive`: 验证 OnReceive panic 转 Reject。
- `TestMiddlewareChainRecoverOnCompleteAndContinue`: 验证 OnComplete panic 不影响后续中间件和 context 回收。
- `TestJobPoolDebugCanBeEnabledAfterPoolInit`: 验证 Job 池初始化后再 `SetDebug(true)` 仍能统计。
- `TestSentinelJobTypeRulesLoadToRuntimeResources`: 验证 `NewSentinelMiddlewareWithJobType + WithFlowRule/WithCircuitBreakerRule` 会把默认规则加载到真实 `serviceName:jobType` resource。
- `TestSentinelJobTypeSpecificRules`: 验证不同 JobType 可以拥有不同 flow / circuit breaker 阈值，未配置的 JobType 不会误挂规则。
- `TestSentinelOnStopClearsFineGrainedRules`: 验证 Stop 时对所有细粒度 resource 做对称清理。
