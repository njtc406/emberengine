# Actor 包代码审查问题清单

> 审查范围: `engine/pkg/actor/` 及 `engine/pkg/actor/mailbox/`  
> 首次审查: 2026-03-16  
> 二次审查: 2026-03-17  
> 三次审查: 2026-03-23  
> 四次审查: 2026-03-23  
> 五次审查: 2026-03-25  
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
| A-19 | P2 | `config_example.go` ShrinkFactor 示例值超限 | `mailbox/config_example.go` | ✅ 已修复 |
| A-20 | P0 | `SentinelMiddleware.OnComplete` 将所有成功请求标记为错误 | `mailbox/sentinel_middleware.go` | ✅ 已修复 |

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

---

## 四次审查（2026-03-23）

### A-19 确认

`config_example.go` 中 `ExampleAutoScalingConfig` 的 `ShrinkFactor: 0.75` **仍未修复**，超出 `fixConf` 限制 `0.5`。

### 新发现问题

### A-20 [P0] `SentinelMiddleware.OnComplete` 将所有成功请求标记为错误

**文件:** `engine/pkg/actor/mailbox/sentinel_middleware.go` — `OnComplete` 方法

**问题描述:**

`OnComplete` 的 `if/else` 逻辑缺少 `panicVal != nil` 条件判断，导致所有 `err == nil` 的请求（包括完全正常的成功请求）都进入 `else` 分支，调用 `sentinel.TraceError(e, fmt.Errorf("panic: <nil>"))`。

这意味着 **每一个成功请求都会被 Sentinel 视为一次错误**，将导致：
1. 错误率统计被严重污染，接近 100% 的请求被标记为"失败"
2. 配置了 `WithCircuitBreakerRule(errorRatio)` 的服务在正常流量下也会迅速触发熔断
3. 线上问题难以定位 — 熔断器频繁打开但无实际错误

**当前代码:**

```go
// 标记错误（影响熔断统计）
if err != nil {
    sentinel.TraceError(e, err)
} else {
    sentinel.TraceError(e, fmt.Errorf("panic: %v", panicVal))
}
```

**修复方案:**

```go
// 标记错误（影响熔断统计）
if err != nil {
    sentinel.TraceError(e, err)
} else if panicVal != nil {
    sentinel.TraceError(e, fmt.Errorf("panic: %v", panicVal))
}
```

---

### 全面验证通过的模块

以下模块在本次审查中未发现新问题：

| 模块 | 验证重点 |
|------|----------|
| `pid.go` | `IsRetired` atomic 读、`SetMaster` 约束文档、`GetPrimarySecondaryKey` 无 fmt.Sprintf |
| `mailbox.go` | `PostJob` 挂起→中间件→分发完整链路、Suspend/Resume CAS |
| `worker_pool.go` | `DispatchJob` RLock 快照 + SubmitJob 状态保护、`resizeWorkers` 缩容先摘环再停 Worker 避免死锁、`SetRWEnabled` TryLock 超时保护、`fixConf` 因子校验 |
| `worker.go` | `SubmitJob` double-check 门控、`BeginStop` 三阶段状态机（Running→Closing→Closed）、`execRead` Gosched 退避 + RLock-after-check 重检查、`execWrite` 指数退避 TryLock + defer LIFO 保证 Unlock 先于 writeRequested 递减、Drain 阶段 enableRW 检查不存在 TOCTOU（因为 SetRWEnabled 需要获取同一把锁） |
| `middleware_chain.go` | COW `atomic.Pointer` 读路径无锁、`middlewareSnapshot` 保证 OnReceive→OnComplete 使用同一份切片、`ctxPool` 池化回收正确（Put 时 reset，Get 后显式赋值） |
| `circuit_breaker_middleware.go` | CAS 状态转换覆盖所有路径（Closed→Open、Open→HalfOpen、HalfOpen→Closed/Open）、窗口重置 CAS 容忍微小误差 |
| `rate_limit_middleware.go` | 底层 `rate.Limiter` CAS 无锁、skipFunc 可选跳过 |
| `dispatch_key_stats_middleware.go` | `reportAndReset` swap-map 模式正确、达到 maxKeys 丢弃新 key 避免 O(n) |
| `suspend_policy.go` | `CompositeSuspendPolicy` RWMutex + slice-header 快照安全、`AddPolicy` append 不影响已快照的旧 slice |
| `scheduler.go` | 单线程 Worker 内调用无需加锁、`resetCountersIfNeeded` 归一化策略保持相对比例 |
| `scaler.go` / `strategy.go` / `strategy_factory.go` | clamp 边界保护、comma-ok 断言 + 默认值、syncx.Map 注册表 |
| `job/job.go` | `Reset` 清零所有字段、`DataRef` 由 Pool 管理 |
| `job/job_factory.go` | `jobFactoryFrozen` 冻结保护、init 时注册无并发风险 |
| `queue_manager_dual.go` / `queue_manager_priority.go` | MPSC 队列、栈分配 buf 优化 |

---

## 五次审查（2026-03-25）

### 修复确认

#### A-20 ✅ `SentinelMiddleware.OnComplete` 错误标记 → 已修复

原代码 `if err != nil { ... } else { TraceError(...) }` 已修改为 `if err != nil || panicVal != nil { if err != nil { ... } else { ... } }`。
外层先判断 `err != nil || panicVal != nil`，成功请求不再进入任何 TraceError 分支。Sentinel 熔断统计不再被污染。✅

#### A-19 ✅ `config_example.go` ShrinkFactor 示例值超限 → 已修复

`ExampleAutoScalingConfig` 中 `ShrinkFactor` 已从 `0.75` 修正为 `0.25`，在 `fixConf` 限制 `> 0.5` 的合法范围内。

### CODE_REVIEW_FIXLIST.md NEW-1 确认

**NEW-1 ✅ `ConnectionPool.Stop()` 幂等保护 → 已修复**

`ConnectionPool` 结构体已添加 `stopOnce sync.Once` 字段。`Stop()` 方法使用 `cp.stopOnce.Do(...)` 包裹 `cancel()`、ticker Stop 和 channel close 操作。`wg.Wait()` 和连接关闭置于 `Once` 之外，重复调用安全。

### 完整代码审计结果

本次对 `engine/pkg/actor/` 及 `engine/pkg/actor/mailbox/` 全部源文件进行了第 5 轮完整审计，**未发现新的正确性/并发/安全问题**。

逐文件验证结果：

| 文件 | 验证重点 | 结论 |
|------|----------|------|
| `pid.go` | `IsRetired` atomic 读、`SetMaster` 文档约束、字符串拼接无 fmt | ✅ 无问题 |
| `event.go` | 简单类型转换 | ✅ 无问题 |
| `mailbox.go` | PostJob 完整链路（挂起→中间件→分发）、Suspend/Resume CAS | ✅ 无问题 |
| `worker_pool.go` | DispatchJob RLock、resizeWorkers 缩容死锁防护、SetRWEnabled TryLock 超时、fixConf 因子校验、BeginStop wg.Wait 先于 worker 停止 | ✅ 无问题 |
| `worker.go` | SubmitJob double-check 门控、三阶段状态机、execRead Gosched 退避 + RLock-after-check、execWrite 指数退避 + defer LIFO、Drain WLock/非 WLock 分支、watchdog timer | ✅ 无问题 |
| `middleware_chain.go` | COW atomic.Pointer、middlewareSnapshot 快照一致性、ctxPool reset 完整性、ExecuteOnComplete 洋葱逆序正确 | ✅ 无问题 |
| `circuit_breaker_middleware.go` | 所有 CAS 状态转换路径覆盖、窗口重置 CAS 容忍微小偏差、`mu` 字段声明但未在 OnReceive/OnComplete 中使用（状态转换完全靠 CAS，`mu` 可移除） | ✅ 无正确性问题 |
| `rate_limit_middleware.go` | `rate.Limiter` 内部 CAS 无锁、skipFunc 可选跳过 | ✅ 无问题 |
| `sentinel_middleware.go` | OnComplete 已修复为 `err != nil \|\| panicVal != nil` 前置判断、LoadRulesOfResource 按资源加载、sentinelSystemRulesOnce 保护全局规则 | ✅ 无问题 |
| `dispatch_key_stats_middleware.go` | reportAndReset swap-map 模式、maxKeys 丢弃新 key、stopOnce 保护 | ✅ 无问题 |
| `suspend_policy.go` | CompositeSuspendPolicy RWMutex、DefaultSuspendPolicy envelope nil 保护 | ✅ 无问题 |
| `stop_policy.go` | DrainPolicy 枚举 + ParseDrainPolicy | ✅ 无问题 |
| `middleware_factory.go` | 配置驱动创建、MergeMiddlewares | ✅ 无问题 |
| `config_example.go` | ShrinkFactor 已修正为 0.25（A-19 已修复） | ✅ 无问题 |
| `scheduler.go` | 单线程调用无并发风险、resetCountersIfNeeded 归一化保持比例 | ✅ 无问题 |
| `scaler.go` | clamp 边界保护、GrowthFactor/ShrinkFactor 使用 | ✅ 无问题 |
| `strategy.go` | comma-ok 断言 + 默认值 | ✅ 无问题 |
| `strategy_factory.go` | syncx.Map 注册表、递归 BuildStrategy | ✅ 无问题 |
| `queue_manager.go` | 接口定义 | ✅ 无问题 |
| `queue_manager_dual.go` | MPSC 队列、优先级分流 | ✅ 无问题 |
| `queue_manager_priority.go` | 栈分配 buf[16] 优化、调度器集成 | ✅ 无问题 |
| `job/job.go` | Reset 清零完整、DataRef 由 Pool 管理 | ✅ 无问题 |
| `job/job_factory.go` | jobFactoryFrozen 保护、init 静态注册 | ✅ 无问题 |
| `job/job_factory_test.go` | 断言正确、resetFactoryFrozenForTest 辅助函数 | ✅ 无问题 |
| `job/job_factory_export_test.go` | 测试辅助 | ✅ 无问题 |
| `middleware_bench_test.go` | 基准测试覆盖 OnReceive/OnComplete/并发状态转换 | ✅ 无问题 |
| `openspec.go` (actor) | 包文档 | ✅ 无问题 |
| `openspec.go` (mailbox) | 包文档 | ✅ 无问题 |
| `job/openspec.go` | 包文档 | ✅ 无问题 |

### 剩余待处理

| 编号 | 优先级 | 问题 | 状态 |
|------|--------|------|------|
| A-19 | P2 | `config_example.go` ShrinkFactor 0.75 → 应 ≤ 0.5 | ✅ 已修复 |
