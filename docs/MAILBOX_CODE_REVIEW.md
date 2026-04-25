# Mailbox 模块代码审查报告（第二轮）

> 审查范围：`engine/pkg/actor/mailbox/` 全部 Go 源文件（22 个）+ `mailbox/job/` 子包  
> 审查时间：2025 年  
> 前置：所有 ACTOR_DESIGN_REVIEW.md 中 12 项（BUG-1 ~ QUALITY-2）已全部修复完成

---

## 审查结论

经过逐文件深度审查与代码级交叉验证，**未发现阻塞性 BUG**。发现 **1 个潜在风险（RISK）**、**3 个设计改进（DESIGN）**、**2 个性能优化点（PERF）**、**2 个质量改进（QUALITY）**，共 8 项。

整体评价：Mailbox 核心机制（Worker 生命周期、Double-Check + Submitters 停机协议、RW RLock-after-check、中间件 COW + atomic.Pointer、熔断器全无锁状态机）设计精良，并发安全性经过验证。

---

## P1 — 潜在风险

### RISK-1: AutoScaler `GrowthFactor/ShrinkFactor = 0` 产生静默无效扩缩容

**文件**: `scaler.go` L53-57  
**问题**:  
- 当 `GrowthFactor = 0` 时，`add = ceil(cur * 0) = 0`，扩容后 `newSize == cur`，逻辑正确跳过（L67 `if newSize != cur`），但无任何日志提示，策略触发了扩容但实际未执行。
- 当 `ShrinkFactor = 0` 时同理，缩容无效。
- 当 `MinWorkerNum = 0` 时，`clamp` 允许 Worker 数降为 0，系统完全无法处理消息。

**影响**: 配置错误导致系统静默降级，运维难以定位。  
**建议**: 在 `fixConf`（`worker_pool.go`）中增加下限校验：`MinWorkerNum >= 1`、`GrowthFactor > 0`、`ShrinkFactor > 0`。

---

## P2 — 设计改进

### DESIGN-1: PriorityQueueManager Submit Fallback 缺少可观测性

**文件**: `queue_manager_priority.go` L93-101  
**问题**: 当 Submit 的优先级未在配置中注册时 fallback 到 `fallbackPriority`，但无任何日志或指标。开发者误用优先级时，高优先级任务静默混入低优先级队列处理变慢，极难排查。  
**建议**: 在 fallback 分支增加 `WarnOnce` 日志或 `unmatchedPriorityCount` 计数器。

### DESIGN-2: MiddlewareContext 池回收时 `data` map 不收缩

**文件**: `middleware_chain.go` L155 (ctxPool WithReset)  
**问题**: `Reset` 回调使用 `delete(mc.data, k)` 逐 key 清理 map。Go 的 map `delete` 不会释放底层哈希桶内存。若某次请求 `data` 扩容到大量 key，回收后 map 的底层容量永久保持高水位。  
**影响**: 长周期运行后池中的 MiddlewareContext 累积的 map 内存不会自然下降。  
**建议**: 在 `Reset` 中判断 `len(mc.data) > 阈值` 时直接 `mc.data = make(map[string]any, initialCap)` 释放旧 map。

### DESIGN-3: CompositeSuspendPolicy 热路径加锁

**文件**: `suspend_policy.go` L84-90  
**问题**: `ShouldAllow` 在 `PostJob` 热路径上被调用，使用 `mu.RLock()`。`AddPolicy` 理论上仅在初始化阶段调用，但接口未强制约束。如果运行时频繁动态修改策略，RLock/Lock 竞争将影响 PostJob 吞吐量。  
**建议**: 两种方案择一：
- (A) `AddPolicy` 仅允许在 Start 前调用，取消锁，运行时策略不可变。
- (B) 改为 `atomic.Pointer[[]ISuspendPolicy]` COW 模式，读无锁。

---

## P2 — 性能优化

### PERF-1: DispatchKeyStatsMiddleware 全局互斥锁

**文件**: `dispatch_key_stats_middleware.go` L103-121 (`OnReceive`)  
**问题**: 在 `OnReceive` 热路径上使用 `m.mu.Lock()` 保护 `map[string]uint64`。当 DispatchKey 空间大（如用户 ID 路由、会话 ID）时，锁竞争对投递吞吐量产生显著影响。  
**建议**: 该中间件定位为辅助诊断工具，两种改进方案：
- (A) 使用分段锁（Sharded Map：hash(key) % N → 每段独立 mutex）
- (B) 每个 Worker 维护本地统计 map，定时合并到全局（无热路径锁）

### PERF-2: PriorityScheduler `counters` map 非原子但单线程安全

**文件**: `scheduler.go` L160-200 (`NextPriorityWithOrdering` + `resetCountersIfNeeded`)  
**问题**: `counters map[def.Priority]int` 非并发安全，但由于 `NextJob` 仅在单个 Worker 的 `run` 循环中调用且 PriorityQueueManager 被单 Worker 独占，当前安全。但这一假设未在代码中显式约束——如果未来 QueueManager 被多 Worker 共享，将产生数据竞争。  
**建议**: 添加 `// NOTE: NOT goroutine-safe, must be used by single owner goroutine.` 文档注释，或改用 `atomic.Int64` 计数器以防御未来变更。

---

## P3 — 质量改进

### QUALITY-1: `format.go` `itoa` 对负数处理不安全

**文件**: `format.go` L4  
**问题**: `itoa(n int)` 直接 `itoaU64(uint64(n))`，当 n < 0 时，`uint64(-1)` = `18446744073709551615`，输出极大正数而非预期的负数字符串。当前所有调用点传入的值均为非负，但接口签名 `int` 未约束正数。  
**建议**: 添加负数处理（与 `formatFloat1` 保持一致）：
```go
func itoa(n int) string {
    if n < 0 {
        return "-" + itoaU64(uint64(-n))
    }
    return itoaU64(uint64(n))
}
```

### QUALITY-2: `strategy_factory.go` `BuildStrategy` 递归无深度限制

**文件**: `strategy_factory.go` L40-58  
**问题**: `BuildStrategy` 递归处理 `cfg.Subs`，如果配置文件存在循环引用（Subs 引用自身），将无限递归直到栈溢出。  
**建议**: 添加 `maxDepth` 参数或内部计数器，超过阈值（如 10）返回错误。

---

## 已验证的安全机制（正面发现）

以下关键设计经逐行验证确认正确：

| 机制 | 文件 | 验证结论 |
|------|------|----------|
| Worker 停机协议 (Double-Check + submitters) | worker.go L132-158 + L258-276 | Closing→等submitters=0→Closed，逻辑严密 |
| RW readSem 令牌释放 | worker.go L509 (readFunc defer) | defer 确保任何路径（含 panic）均归还令牌 |
| RLock-after-check 降级 | worker.go L495-502 | enabled=false 时正确释放令牌+RUnlock+回退 safeExec |
| 熔断器全无锁状态机 | circuit_breaker_middleware.go | CAS 驱动 Closed↔Open↔HalfOpen，无死锁风险 |
| 缩容时先摘 Ring 后 Stop | worker_pool.go L290-310 | 先 RemoveMany→Store workerCount→Unlock→BeginStop，消除路由到已停止 Worker 的窗口 |
| 中间件链 COW + atomic.Pointer | middleware_chain.go | 读路径 Load() 无锁，UpdateMiddlewares 替换整个快照 |

---

## 优先级与实施建议

| 编号 | 等级 | 优先级 | 影响面 | 工作量 |
|------|------|--------|--------|--------|
| RISK-1 | 风险 | P1 | 配置错误时系统静默降级 | 小（fixConf 加校验） |
| DESIGN-1 | 设计 | P2 | 排查困难 | 小（加日志） |
| DESIGN-2 | 设计 | P2 | 长期内存 | 小（条件重建 map） |
| DESIGN-3 | 设计 | P2 | 热路径性能 | 小（COW 替换 RLock） |
| PERF-1 | 性能 | P2 | 辅助工具锁竞争 | 中（分段锁/Worker-local） |
| PERF-2 | 性能 | P3 | 未来防御 | 小（加注释或 atomic） |
| QUALITY-1 | 质量 | P3 | 正确性防御 | 极小（3行） |
| QUALITY-2 | 质量 | P3 | 配置安全 | 极小（加深度限制） |

---

## 总体评价

Mailbox 模块经两轮审查 + 12 项修复后：
- **并发安全**：核心投递/停机/RW 路径经 `-race` 验证无竞争
- **可维护性**：RWController 提取 + WorkerEnv 注入大幅降低 Worker/WorkerPool 耦合度
- **扩展性**：中间件洋葱模型 + 策略工厂模式支持灵活扩展
- **需关注**：配置校验（RISK-1）应优先修复，其余可作为迭代优化项
