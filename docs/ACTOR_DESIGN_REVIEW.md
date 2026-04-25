# Actor 模块设计审查报告

> 审查日期：2026-04-10  
> 审查范围：`engine/pkg/actor/` 全目录（含 mailbox 子模块）  
> 状态：全部完成

---

## 一、总体评价

Actor 模块整体设计质量较高，具备以下优点：

- **Worker Pool + 一致性哈希** 架构成熟，支持动态扩缩容
- **RW 读写分离** 设计严谨，有完整的并发协议和测试覆盖
- **中间件洋葱模型** 使用 COW + atomic.Pointer 实现无锁热路径
- **对象池化** 在 Job、MiddlewareContext 等高频对象上减少 GC 压力
- **优雅停机** 采用 BeginStop + Wait 两阶段，StopTimeout 超时降级

以下列出发现的 Bug、设计问题及优化建议，按严重程度排序。

---

## 二、Bug

### BUG-1: DefaultSentinelSkipFunc 优先级比较方向反转 ⚠️

**文件**: `mailbox/sentinel_middleware.go` L393-401

**现象**: `DefaultSentinelSkipFunc` 意图是跳过"紧急及以上"消息的 Sentinel 限流检查，但比较方向错误，导致**几乎所有消息都被跳过**。

**分析**:

```
Priority 数值: Sys(-3) < Urgent(-2) < High(-1) < Normal(0) < Low(1) < Batch(2)
数值越小 → 优先级越高
```

```go
// 当前代码（BUG）:
return job.GetPriority() >= def.PriorityUrgent  // >= -2，即 Urgent/High/Normal/Low/Batch 全部跳过

// 正确改法（与 SuspendPolicy、RateLimitMiddleware 一致）:
return job.GetPriority() <= def.PriorityUrgent  // <= -2，即仅 Sys 和 Urgent 跳过
```

**对比同模块其他代码**:

| 位置 | 代码 | 正确性 |
|------|------|--------|
| `suspend_policy.go` L43 | `job.GetPriority() <= def.PriorityUrgent` | ✅ |
| `middleware_factory.go` L78 | `mctx.Job().GetPriority() <= def.PriorityUrgent` | ✅ |
| `sentinel_middleware.go` L398 | `job.GetPriority() >= def.PriorityUrgent` | ❌ |

**影响**: 当前该函数无实际调用方（用户需显式传入 `WithSentinelSkipFunc(DefaultSentinelSkipFunc)` 才会生效），但作为导出的默认实现，一旦被使用将导致 Sentinel 限流/熔断对该 Service 实质无效（所有消息都被 skip），保护形同虚设。

**修复**: 改 `>=` 为 `<=`，1 行改动。

---

## 三、设计问题

### DESIGN-1: PriorityQueueManager 与 DualQueueManager 的 Submit 行为不一致

**文件**: `mailbox/queue_manager_dual.go`、`mailbox/queue_manager_priority.go`

**问题**: 两种 `IQueueManager` 实现对未知优先级的处理不一致：

| 实现 | 行为 |
|------|------|
| DualQueueManager | 任何优先级都能入队（< Normal 进 system，其余进 user） |
| PriorityQueueManager | 未在配置中注册的优先级 → `return fmt.Errorf("invalid priority: %d")` → **静默丢弃消息** |

**风险**: 用户使用 `PriorityBackground(3)` 且配置中未包含该优先级时，消息被拒绝，但错误只传到 `Worker.SubmitJob` 的返回值，调用方不一定处理。

**建议**: PriorityQueueManager.Submit 对未注册优先级 fallback 到最低优先级队列，而非报错丢弃。或在 NewPriorityQueueManager 时强制注册所有 `def.Priority*` 常量。

### DESIGN-2: PriorityScheduler NextPriority 与 NextPriorityWithOrdering 逻辑重复

**文件**: `mailbox/scheduler.go`

**问题**: 存在两套功能几乎相同的调度方法：

- `NextPriority` + `absolutePriority`/`weightedPriority`/`fairnessPriority`
- `NextPriorityWithOrdering` + `weightedPriorityWithOrdering`/`fairnessPriorityWithOrdering`

唯一区别是 `WithOrdering` 版本假设输入已排序并在相同最高优先级分组内做选择，而原版在全集合做选择。但 `NextPriority` 目前实际**未被使用**（PriorityQueueManager 只调用 `NextPriorityWithOrdering`）。

**建议**: 移除未使用的 `NextPriority` 及其三个子方法，减少维护负担和认知开销。

### DESIGN-3: WorkerPool 职责过重（God Object 倾向）

**文件**: `mailbox/worker_pool.go`

**问题**: WorkerPool 承担了过多职责，字段数量达到 20+ 个：

| 职责 | 涉及字段 |
|------|---------|
| Worker 生命周期 | workers, ctx, cancel, wg |
| 消息路由 | ring, invoker |
| 中间件管理 | middlewareChain |
| RW 读写分离 | enableRW, rwMu, writeRequested, readSem, stopTimeout, readPool |
| RW 可观测性 | rwReadTotal, rwWriteTotal, rwDrainDiscardTotal, maxJobExecTime |
| 扩缩容 | autoScaler, workerCount, scaleTrigger |
| 统计 | statsEnabled, statsInterval, dispatchCnt |
| 停机策略 | drainPolicy |
| 性能分析 | profiler |

**影响**: 新增特性时修改面大，单元测试需要 mock 大量状态。

**建议（渐进式）**:

1. 将 RW 相关字段（`enableRW`, `rwMu`, `writeRequested`, `readSem`, `stopTimeout`, `readPool` 及所有 RW 指标）抽出为独立 `RWController` 结构体
2. 将扩缩容逻辑（`autoScaler`, `scaleTrigger`, `workerCount`）抽出为 `ScaleController`
3. WorkerPool 持有这两个子模块的指针

### DESIGN-4: Worker 与 WorkerPool 紧耦合

**文件**: `mailbox/worker.go`

**问题**: Worker 通过 `w.pool` 指针直接访问 WorkerPool 的 15+ 个字段：

```
pool.rwMu, pool.writeRequested, pool.readSem, pool.enableRW,
pool.logger, pool.invoker, pool.profiler, pool.middlewareChain,
pool.readPool, pool.stopTimeout, pool.rwReadTotal, pool.rwWriteTotal,
pool.rwDrainDiscardTotal, pool.maxJobExecTime, ...
```

**影响**: Worker 无法脱离 WorkerPool 独立测试；WorkerPool 内部字段的任何重构都可能影响 Worker。

**建议**: 定义 `WorkerContext` 接口（或结构体），Worker 仅依赖该接口提供的能力：

```go
type WorkerContext interface {
    Logger() log.ILoggerX
    Invoker() inf.IMessageInvoker
    MiddlewareChain() *MiddlewareChain
    RWController() *RWController  // 可为 nil
    Profiler() *profiler.Profiler // 可为 nil
}
```

### DESIGN-5: config_example.go 不应位于生产代码包内

**文件**: `mailbox/config_example.go`

**问题**: `ExampleDualQueueConfig()` 和 `ExamplePriorityQueueConfig_Absolute()` 是纯示例代码，但作为 mailbox 包的导出函数存在，会出现在 godoc 中，增加了包的公开 API 面。

**建议**: 移至 `mailbox/config_example_test.go`（使用 `package mailbox_test`）或移至 `example/` 目录。

### DESIGN-6: job_factory.go 注册表缺乏并发安全保障

**文件**: `mailbox/job/job_factory.go`

**问题**: `jobFactory` 是普通 `map`（非 `sync.Map`），通过 `jobFactoryFrozen` 原子变量做"写后冻结"保护。但存在竞态窗口：

```
goroutine A: RegisterJobFactory → 检查 frozen=false → 写入 map
goroutine B: CreateJob → 设置 frozen=true → 读取 map
```

如果 A 和 B 并发执行，map 的读写无锁保护。虽然设计意图是"init 阶段注册，运行阶段使用"，但缺乏编译期或运行时的强制保障。

**建议**: 

- 方案 A: `RegisterJobFactory` 在 `frozen=true` 时 panic（而非返回 error），明确约束
- 方案 B: 使用 `sync.Map` 替换普通 map
- 方案 C（推荐）: 在 `init()` 中完成注册，`RegisterJobFactory` 加 `sync.Once` 或 `sync.Mutex` 保护

---

## 四、潜在风险

### RISK-1: RpcJob.Release 注释掉了 payload 释放

**文件**: `mailbox/job/job.go` L117-120

```go
func (j *RpcJob) Release() {
    // 先释放 payload (envelope)，避免 msgEnvelopePool 泄漏
    //if payload := j.GetPayload(); payload != nil {
    //    payload.Release() // TODO 不应该释放,应该由业务自己控制数据的释放
    //}
    getMsgJobPool().Put(j)
}
```

**风险**: 如果业务层未正确调用 `envelope.Release()`，envelope 对象池会持续泄漏。当前依赖 TODO 注释约束业务方行为，缺乏运行时检测。

**建议**: 在 Debug 模式下，`RpcJob.Release()` 检查 payload 是否已被释放，未释放则输出 warning 日志（leak 检测）。

### RISK-2: Sentinel systemRules 全局加载竞争

**文件**: `mailbox/sentinel_middleware.go` L286-293

```go
sentinelSystemRulesOnce.Do(func() {
    if _, err := system.LoadRules(m.systemRules); err != nil { ... }
})
```

如果多个 Service 注册了不同的 `systemRules`，只有第一个加载的会生效，其他 Service 的 systemRules 被静默忽略。`sentinelSystemRulesOnce` 保证了安全，但语义上可能不符合用户预期。

**建议**: 在 OnStart 中检测到 systemRules 非空但 `sentinelSystemRulesOnce` 已执行时，输出 warning 日志。

### RISK-3: PID.IsMaster 无原子保护

**文件**: `actor/pid.go` L49-53

```go
// 注意：IsMaster 是 protobuf 生成的 bool 字段，无法使用原子操作。
// 调用方必须保证 SetMaster 与 GetIsMaster 不会被并发调用
func (pid *PID) SetMaster(master bool) {
    pid.IsMaster = master
}
```

PID 在集群中被多个 goroutine 引用（`selector.go` 中路由查找读取 `GetIsMaster`），`SetMaster` 在 etcd watcher goroutine 中调用，两者分属不同 goroutine，存在 data race 风险。

> **注**: 此问题在 `ACTOR_CODE_REVIEW.md` A-10 中已标记为"已修复（文档补充）"——仅通过注释约束调用方，未做代码级修复。但注释约束无法被 `-race` 检测器验证。

**建议**: 如果 protobuf 生成字段不便修改，可在 PID 上增加 `masterFlag atomic.Bool` 独立字段，`SetMaster`/`IsMasterNode` 使用该字段。protobuf 的 `IsMaster` 仅用于序列化传输。

---

## 五、代码质量改进

### QUALITY-1: 文件头部注释模板未清理

多个文件仍保留模板占位符：

```go
// @Title  请填写文件名称（需要改）
// @Description  请填写文件描述（需要改）
```

涉及文件: `pid.go`

**建议**: 使用实际内容替换或移除。

### QUALITY-2: DispatchKeyStatsMiddleware 中 itoa/itoaU64/formatPct 应提取为公共工具

**文件**: `mailbox/dispatch_key_stats_middleware.go`

`itoa`, `itoaU64`, `formatPct` 定义在 `dispatch_key_stats_middleware.go`，`formatFloat1` 定义在 `worker_pool.go`。虽同属一个包可共享，但定义分散在与职责无关的文件中，不便查找和维护。

**建议**: 提取到 `mailbox/format.go` 统一维护。

### QUALITY-3: autoScaleWorkers 中多处 TODO 待决

**文件**: `mailbox/worker_pool.go` L505-510

```go
// TODO 定时触发检查这部分先这么用吧,主要还没想到什么好的方式来为每种策略定制一个检查机制
// TODO 主要是嵌套策略里面可能包含了自驱动和外部驱动两种类型的策略,不太好分开
// TODO 下一步的改动可能是把触发时机抽离出来...
```

**建议**: 将 TODO 转化为 issue 跟踪，并在 ROADMAP 中记录。

---

## 六、优先级建议

| 优先级 | 编号 | 类型 | 说明 | 工作量 | 状态 |
|--------|------|------|------|--------|------|
| **P0** | BUG-1 | Bug 修复 | DefaultSentinelSkipFunc 比较方向修复 | 1 行 | ✅ 已修复 |
| **P1** | DESIGN-1 | 设计修正 | PriorityQueueManager Submit fallback | ~10 行 | ✅ 已修复 |
| **P1** | DESIGN-6 | 安全加固 | jobFactory 并发安全 | ~15 行 | ✅ 已修复 |
| **P1** | RISK-1 | 防御增强 | RpcJob.Release leak 检测 | ~10 行 | ✅ 已加 warn 日志 |
| **P2** | DESIGN-2 | 代码清理 | 移除未使用的 NextPriority | ~60 行删除 | ✅ 已清理 |
| **P2** | DESIGN-5 | 代码质量 | config_example 移至 example/ 目录 | 移动文件 | ✅ 已移动 |
| **P2** | RISK-2 | 日志增强 | Sentinel systemRules 重复加载 warning | ~5 行 | ✅ 已修复 |
| **P2** | RISK-3 | 并发安全 | PID.IsMaster atomic 化 | ~20 行 | ✅ 已修复 (proto MasterFlag int32) |
| **P3** | DESIGN-3 | 架构优化 | WorkerPool 拆分 RWController | 中 | ✅ 已拆分 |
| **P3** | DESIGN-4 | 架构优化 | Worker 依赖接口化 | 中 | ✅ 已完成 (WorkerEnv) |
| **P3** | QUALITY-1 | 代码质量 | pid.go 头部注释清理 | 小 | ✅ 已清理 |
| **P3** | QUALITY-2 | 代码质量 | 辅助函数提取到 format.go | 小 | ✅ 已提取 |

---

## 七、总结

Actor 模块的核心架构（Mailbox → WorkerPool → Worker → QueueManager）设计合理，RW 读写分离和中间件机制的工程质量较高。

关键改进点：

1. **立即修复** BUG-1（Sentinel skip 方向错误），否则 Sentinel 限流/熔断形同虚设
2. **短期** 统一 QueueManager 行为、加固 jobFactory 并发安全
3. **中期** 考虑 WorkerPool 职责拆分，降低模块间耦合度
