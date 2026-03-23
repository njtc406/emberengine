# Actor 包代码审查问题清单

> 审查范围: `engine/pkg/actor/` 及 `engine/pkg/actor/mailbox/`  
> 首次审查: 2026-03-16  
> 二次审查: 2026-03-17  
> 三次审查: 2026-03-23  
> 审查人: Code Review Agent

## 问题汇总

| 编号 | 优先级 | 问题 | 文件 | 修复状态 |
|------|--------|------|------|----------|
| A-01 | P0 | 策略工厂硬类型断言导致 panic | `mailbox/strategy.go` | ✅ 已修复 |
| A-02 | P0 | `jobFactory` 全局 map 并发读写竞争 | `mailbox/job/job_factory.go` | ✅ 已修复 |
| A-03 | P0 | `CompositeSuspendPolicy.AddPolicy` 无并发保护 | `mailbox/suspend_policy.go` | ✅ 已修复 |
| A-04 | P1 | 熔断器统计字段未使用类型安全原子操作 | `mailbox/circuit_breaker_middleware.go` | ✅ 已修复 |
| A-05 | P1 | `gorm/utils` 重依赖仅用于 int→string | `mailbox/worker_pool.go` | ✅ 已修复 |
| A-06 | P1 | DispatchKeyStats 淘汰策略热路径 O(n) | `mailbox/dispatch_key_stats_middleware.go` | ✅ 已修复 |
| A-07 | P1 | `PriorityScheduler.mutex` 声明未使用 | `mailbox/scheduler.go` | ✅ 已修复 |
| A-08 | P1 | Sentinel `LoadRules` 全局覆盖风险 | `mailbox/sentinel_middleware.go` | ✅ 已修复 |
| A-09 | P2 | `SetRWEnabled(true)` 并发调用 `readSem` 竞态 | `mailbox/worker_pool.go` | ✅ 已修复 |
| A-10 | P2 | `PID.SetMaster` 与"只读值对象"语义矛盾 | `pid.go` | ✅ 已修复(补充文档) |
| A-11 | P2 | 测试用例 `TestCreateJob_Builtins` 断言为空 | `mailbox/job/job_factory_test.go` | ✅ 已修复 |
| A-12 | P2 | `NewWorkerPool` 使用 panic 校验 invoker | `mailbox/worker_pool.go` | ✅ 已修复 |
| A-13 | P2 | `DefaultSuspendPolicy.ShouldAllow` 缺少 envelope nil 保护 | `mailbox/suspend_policy.go` | ✅ 已修复 |
| A-14 | P1 | `MiddlewareChain` 动态 Add/Remove 与 OnComplete 索引不一致 | `mailbox/middleware_chain.go` | ✅ 已修复 |
| A-15 | P1 | `AutoScaler` 缩容时 GrowthFactor/ShrinkFactor 未校验 | `mailbox/scaler.go` | ✅ 已修复 |
| A-16 | P2 | `execRead` 信号量令牌释放路径中 `recover()` 吞没 panic | `mailbox/worker.go` | ✅ 已修复 |
| A-17 | P2 | `Job.Reset` 未重置 `DataRef` 基础引用计数 | `mailbox/job/job.go` | ✅ 已确认(无需修改) |
| A-18 | P2 | Sentinel `system.LoadRules` 仍为全局覆盖语义 | `mailbox/sentinel_middleware.go` | ✅ 已修复 |
| A-19 | P2 | **[NEW]** `config_example.go` ShrinkFactor 示例值超限 | `mailbox/config_example.go` | ⬜ 待修复 |

---

## 首轮问题修复确认（二次审查）

### A-01 ✅ 策略工厂硬类型断言 → 已修复

已改为 comma-ok 断言 + 安全默认值（`mode` 默认 `"any"`，`IdleThreshold` 默认 `50`，`MaxLoadThreshold` 默认 `64`）。

### A-02 ✅ `jobFactory` 并发保护 → 已修复

已添加 `jobFactoryFrozen atomic.Bool`，`CreateJob`/`GetJobPayload` 首次调用时冻结，`RegisterJobFactory` 冻结后返回 error。

### A-03 ✅ `CompositeSuspendPolicy` 并发保护 → 已修复

已添加 `sync.RWMutex`，`ShouldAllow` 使用 `RLock` 快照，`AddPolicy` 使用 `Lock`。

### A-04 ✅ 熔断器统计字段 → 已修复

`totalRequests` 和 `rejectedByBreak` 已改为 `atomic.Uint64`。

### A-05 ✅ `gorm/utils` 依赖 → 已修复

import 已移除，改用内部 `itoa` 函数。

### A-06 ✅ DispatchKeyStats O(n) 淘汰 → 已修复

采用方案 C：达到上限后直接丢弃新 key 不记录，避免热路径 O(n) 遍历。

### A-07 ✅ `PriorityScheduler.mutex` 无用字段 → 已修复

字段已移除。

### A-08 ✅ Sentinel `LoadRules` 全局覆盖 → 已修复

flow 和 circuitbreaker 规则已改用 `LoadRulesOfResource` 按资源加载，避免跨 Service 覆盖。

### A-09 ✅ `SetRWEnabled` readSem 竞态 → 已修复

`SetRWEnabled(true)` 分支已使用 `p.mu.Lock()` 保护 readSem 初始化。

### A-10 ✅ `PID.SetMaster` 语义矛盾 → 已修复（文档补充）

已补充注释说明 `SetMaster` 的调用约束：仅在 cluster watcher 单一 goroutine 中执行，protobuf 生成字段无法使用原子操作。

### A-11 ✅ 测试断言为空 → 已修复

已改为 `if job.GetType() != tc.jobType { t.Fatalf(...) }`。

### A-12 ✅ `NewWorkerPool` panic → 已修复

签名已改为 `(*WorkerPool, error)`，连带 `NewMailbox` 也改为返回 `(*Mailbox, error)`。

### A-13 ✅ envelope nil 保护 → 已修复

已添加 `if envelope != nil` 保护。

---

## 二次审查问题修复确认（三次审查）

### A-14 ✅ `MiddlewareChain` 快照不一致 → 已修复

MiddlewareChain 整体重写为 COW（Copy-on-Write）模式：使用 `atomic.Pointer[[]inf.IMailboxMiddleware]` 替代 `sync.RWMutex` + slice。`ExecuteOnReceive` 通过原子 Load 获取不可变快照并保存到 `MiddlewareContext.middlewareSnapshot`，`ExecuteOnComplete` 使用同一份快照逆序执行，彻底消除了 Add/Remove 期间的不一致问题。同时作为 P-05 性能优化的一部分，读路径完全无锁。

---

### A-15 ✅ `AutoScaler` GrowthFactor/ShrinkFactor 校验 → 已修复

在 `fixConf` 中添加了 GrowthFactor 和 ShrinkFactor 的合法性校验：`GrowthFactor <= 0` 时默认 0.5，`ShrinkFactor <= 0 || > 0.5` 时默认 0.25。

---

### A-16 ✅ `execRead` 无意义 `recover()` → 已修复

已移除信号量释放路径中无意义的 `recover()` 包装，直接执行 `<-w.pool.readSem`。

---

### A-17 ✅ `Job.Reset` DataRef 引用计数 → 已确认（无需修改）

经确认，`DataRef` 的引用计数由 Pool 层面通过 `WithRef`/`WithUnRef`/`WithReset` 回调统一管理，`Job.Reset` 不需要额外重置 `DataRef`。此项无需修改。

---

### A-18 ✅ Sentinel `system.LoadRules` 全局覆盖 → 已修复

已使用 `sync.Once`（`sentinelSystemRulesOnce`）包装 `system.LoadRules` 调用，保证全局 system rules 只加载一次，后续 Service 初始化时跳过。

---

## 三次审查（2026-03-23）

### 修复确认

A-14 至 A-18 共 5 项问题已全部验证：

- **A-14**: MiddlewareChain 重写为 COW 模式（`atomic.Pointer` + 不可变切片），`MiddlewareContext` 持有 `middlewareSnapshot`，OnComplete 使用同一份快照。同时引入 `ctxPool` 池化 MiddlewareContext，消除热路径分配。✅
- **A-15**: `fixConf` 中新增 `GrowthFactor`/`ShrinkFactor` 边界校验。✅
- **A-16**: 读 goroutine defer 中无意义的 `recover()` 包装已移除。✅
- **A-17**: 经确认 `DataRef` 引用计数由 Pool 的 `WithRef`/`WithUnRef` 钩子管控，`Reset()` 不需要重置。误报已关闭。✅
- **A-18**: `system.LoadRules` 已用 `sentinelSystemRulesOnce` 保护，只加载一次。✅

### 额外改进确认

- **Worker 状态机**: `closed`/`closing` 两个 `atomic.Bool` 合并为 `state atomic.Int32`（`workerStateRunning` / `workerStateClosing` / `workerStateClosed`），状态转换更清晰。
- **execRead 退避策略**: `time.Sleep` 改为 `runtime.Gosched()` 循环，避免 Windows 下 15ms 最小精度问题。
- **Profiler 热路径优化**: `reflect.TypeOf(job).String()` 替换为 `strconv.Itoa(int(job.GetType()))`，消除反射开销。
- **pid.go**: `CreateInstanceId` 和 `GetPrimarySecondaryKey` 改用 `strconv.FormatInt` + 字符串拼接替代 `fmt.Sprintf`，减少分配。

### 新发现问题

### A-19 [P2] `config_example.go` ShrinkFactor 示例值超出 fixConf 限制

**文件:** `engine/pkg/actor/mailbox/config_example.go` L240

**问题描述:**  
`ExampleAutoScalingConfig` 中 `ShrinkFactor: 0.75`，但 `fixConf` 已限制 `ShrinkFactor > 0.5` 时重置为 `0.25`。示例配置与实际生效值不一致，会误导用户。

```go
ShrinkFactor:   0.75,  // ← fixConf 会强制改为 0.25，示例值无效
```

**修复方案:**  
将示例值改为合法范围内的值：

```go
ShrinkFactor:   0.25,  // 缩容因子：每次减少 25%
```

---

## 非功能性建议

| 建议 | 说明 |
|------|------|
| 补充 RW 模式集成测试 | RW 读写分离逻辑复杂，当前缺少端到端测试（Stop 时序、动态开关等） |
| 中间件 bench 用例补充 OnComplete 路径 | 当前 bench 主要覆盖 OnReceive，OnComplete 路径的性能验证不足 |
| `DispatchKeyStatsMiddleware` maxKeys 应可配置 | 当前硬编码 100,000，不同场景需求差异大 |
| COW MiddlewareChain 补充并发 Add/Remove 测试 | 验证 COW 切换期间 OnReceive→OnComplete 快照一致性 |
