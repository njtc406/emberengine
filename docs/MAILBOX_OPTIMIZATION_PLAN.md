# Actor Mailbox 优化方案

> 基于 `ACTOR_MODULE_ANALYSIS.md` 第六章 7 项改进方向的深度分析  
> 编写日期: 2026-03-27  
> 状态: P0–P3 已实施，P4 不做

---

## 优先级总览

| 优先级 | 改进项 | 类型 | 工作量 | 风险 | 状态 |
|--------|--------|------|--------|------|------|
| **P0** | 6.4 RW 模式集成测试 | 正确性保障 | 大 | 无 | ✅ 已完成 |
| **P1** | 6.2 TryLock 退避优化 + 写等待 warning | 防御优化 | 极小 | 极低 | ✅ 已完成 |
| **P1** | 6.7 maxKeys 配置化 | 代码质量 | 极小 | 无 | ✅ 已完成 |
| **P2** | 6.1 读 goroutine 池化 | 性能优化 | 小 | 低 | ✅ 已完成 |
| **P3** | 6.6 AutoScaler 事件驱动 | 响应性优化 | 中 | 低 | ✅ 已完成 |
| **P3** | 6.3 context.WithValue 合并 | 微优化 | 小 | 低 | ✅ 已完成 |
| **P4** | 6.5 PriorityScheduler DRR/WFQ | 调度优化 | 大 | 中 | ❌ 不做 |

---

## P0: RW 模式集成测试

### 背景

RW 读写分离涉及 5 个关键并发协议：

1. **RLock-after-check** — 动态开关安全
2. **writeRequested** — 写饥饿防护
3. **readSem** — 读并发上限
4. **Stop 超时降级** — in-flight 读超时 → DrainDiscard
5. **SetRWEnabled** — 运行时动态切换

这些协议经过 6 轮代码审查确认设计正确，但**缺少端到端测试覆盖**，无法防止后续重构引入回归。

### 测试矩阵

| # | 测试场景 | 验证目标 | 复杂度 |
|---|----------|----------|--------|
| 1 | 读写交替正确性 | N 写 + M 读并发，写入值 = 最终读取值 | 中 |
| 2 | Stop 时序 | 大量读 → BeginStop → in-flight 读完成后才 Drain | 高 |
| 3 | SetRWEnabled 动态切换 | 运行中关闭 RW → 新读降级串行，老读正常完成 | 高 |
| 4 | readSem 满载退避 | MaxConcurrentReads=2，投 10 个读 → 最多 2 个并行 | 中 |
| 5 | 写饥饿防护 | 大量连续读 + 少量写 → 写不被无限延迟 | 中 |
| 6 | Drain + in-flight 超时 | Stop 时读阻塞超 StopTimeout → 降级 DrainDiscard | 高 |

### 实施方案

**文件位置**: `mailbox/worker_pool_rw_test.go`

**测试基础设施**:

```go
// mockInvoker 模拟业务逻辑，可配置读/写耗时和状态检查回调
type mockInvoker struct {
    readDelay   time.Duration
    writeDelay  time.Duration
    mu          sync.RWMutex
    state       int64          // 共享状态，用于验证读写一致性
    readCount   atomic.Int64
    writeCount  atomic.Int64
}
```

**建议实施顺序**: 场景 1 → 4 → 5 → 2 → 3 → 6（由简到难）

**验收标准**: `go test -race -count=5` 全部通过

### 决策

✅ 已实施。新建 `mailbox/worker_pool_rw_test.go`，6 个场景 `go test -race -count=10` 全部通过。

实施过程中额外修复了 `AdaptiveController.Idle()` 的 lost wakeup bug（`idle.go`）。

---

## P1-a: execWrite TryLock 退避优化

### 背景

当前实现（`worker.go` execWrite）：

```go
const maxBackoff = 1 * time.Millisecond
for !w.pool.rwMu.TryLock() {
    // ... 指数退避 Gosched → 1μs → 2μs → ... → 1ms 封顶
}
```

极端长读场景下 `1ms` 封顶导致写操作自旋 CPU 开销偏高。

### 改动内容

**改动 1: maxBackoff 提高到 5ms**

```go
// 修改前
const maxBackoff = 1 * time.Millisecond

// 修改后
const maxBackoff = 5 * time.Millisecond
```

> 注: Windows 下 `time.Sleep` 最小精度 ~15ms，5ms 封顶实际可能 ~15ms，
> 这反而更有利于降低 CPU 占用，不影响正确性。

**改动 2: 写等待超 100ms 输出 warning**

在 TryLock 循环退出后、`safeExec` 之前添加：

```go
writeWait := time.Since(writeWaitStart)
w.rwWriteWaitSum.Add(writeWait.Nanoseconds())
w.rwWriteWaitCount.Add(1)
if writeWait > 100*time.Millisecond {
    w.pool.logger.Warnf("Worker %d write lock wait %v (>100ms), possible long-running readers",
        w.workerId, writeWait)
}
```

### 影响范围

- `worker.go` execWrite 函数，~5 行改动
- 无接口变更，无配置变更

### 决策

✅ 已实施。改动极小，零风险，线上可通过 warning 日志快速定位读锁持有过久的问题。

---

## P1-b: maxKeys 配置化

### 背景

`dispatch_key_stats_middleware.go` 中 `maxKeys = 100_000` 硬编码：

```go
return &DispatchKeyStatsMiddleware{
    maxKeys:  100_000,  // 硬编码
    // ...
}
```

大型集群（百万级 key）统计不完整，小型服务浪费内存。

### 改动内容

```go
// 修改 NewDispatchKeyStatsMiddleware 签名
func NewDispatchKeyStatsMiddleware(
    logger log.ILoggerX,
    interval time.Duration,
    topN int,
    maxKeys int,       // 新增参数
) *DispatchKeyStatsMiddleware {
    if maxKeys <= 0 {
        maxKeys = 100_000 // 默认值保持向后兼容
    }
    return &DispatchKeyStatsMiddleware{
        maxKeys: maxKeys,
        // ...
    }
}
```

### 影响范围

- `dispatch_key_stats_middleware.go` 构造函数签名 + 默认值
- `middleware_factory.go` 中创建处传入配置值
- 配置结构体增加 `MaxKeys` 字段
- ~10 行改动

### 决策

✅ 已实施。改动极简，零风险。

---

## P2: 读 goroutine 池化

### 背景

当前每个读 Job 通过 `go func()` 创建新 goroutine（`worker.go` execRead）。
Node 层已有 `AntsPool`（`*asynclib.Pool`），接口为 `INodePool.Go(f func()) error`。

### 方案对比

| 方案 | 描述 | 优点 | 缺点 |
|------|------|------|------|
| **A. 注入 Node 级 AntsPool** | NewWorkerPool 增加 INodePool 参数 | 复用已有基础设施；Node 级全局管控容量 | WorkerPool 与 Node 耦合加深；需处理 Go() 返回 error |
| B. per-WorkerPool 独立池 | 每个 WorkerPool 创建自己的 ants.Pool | 隔离性好 | 内存碎片化，大量 Service 时浪费 |
| C. 维持现状 | 依赖 readSem 控制上限 | 零改动 | 高频创建/销毁有 GC 开销 |

### 推荐: 方案 A

**理由**:

1. `AntsPool` 已存在（`node.go` 中初始化），改动量小
2. `readSem` 已限制并发上限（默认 `CPU*4, max 64`），AntsPool 作为二道防线
3. `Go()` 返回 error 时 fallback 到 `go func()`，不阻塞热路径

### 改动内容

**1. WorkerPool 增加字段**:

```go
type WorkerPool struct {
    // ...
    nodePool inf.INodePool // 可选，用于读 goroutine 池化
}
```

**2. NewWorkerPool 签名调整**:

```go
func NewWorkerPool(
    conf *config.MailboxConf,
    logger log.ILoggerX,
    invoker inf.IMessageInvoker,
    nodePool inf.INodePool,        // 新增，可为 nil
    middlewares ...inf.IMailboxMiddleware,
) (*WorkerPool, error)
```

**3. execRead 中 goroutine 创建改为池化**:

```go
// 修改前
go func() { ... }()

// 修改后
readFunc := func() { ... }
if w.pool.nodePool != nil {
    if err := w.pool.nodePool.Go(readFunc); err != nil {
        go readFunc() // fallback
    }
} else {
    go readFunc()
}
```

**4. 上层传入**:

Service 创建 Mailbox 时从 `INodeContext.GetAntsPool()` 获取并传入。

### 影响范围

- `worker_pool.go`: 新增字段 + 构造函数参数 (~5 行)
- `worker.go` execRead: goroutine 创建替换 (~8 行)
- `mailbox.go` 或上层创建处: 传入 AntsPool (~3 行)
- 预计总计 ~30 行改动

### 决策

✅ 已实施（采用方案 B: per-WorkerPool 独立 ants 池，资源隔离）。

实际实施说明：
- 每个 WorkerPool 创建独立的 `asynclib.Pool`（`ReadPoolSize` 配置，默认等于 `MaxConcurrentReads`）
- 使用 `ants.WithNonblocking(true)` 避免池满时阻塞 Worker 主循环
- `readPool.Go()` 失败时 fallback 到 `go func()`
- `SetRWEnabled(true)` 路径也会初始化 readPool
- `Wait()` 中释放池资源

---

## P3-a: AutoScaler 事件驱动触发

### 背景

当前仅定时轮询触发（`worker_pool.go` autoScaleWorkers），
`ResizeCoolDown` 周期内的突发峰值无法即时响应。

### 方案对比

| 方案 | 描述 | 热路径开销 | 复杂度 |
|------|------|-----------|--------|
| A. SubmitJob 同步检查 | 每次 SubmitJob 检查队列长度 | 高（每次 SubmitJob 多一次 check） | 低 |
| B. Worker 空闲触发 | Worker 持续空闲 N 次 → 通知缩容 | 无（不在热路径上） | 低 |
| **C. 异步 channel 通知** | SubmitJob 超阈值 → 非阻塞 send → scaler 监听 | 极低（1 次 atomic load + 非阻塞 select） | 中 |
| D. 维持定时轮询 | 现状 | 无 | 无 |

### 推荐: 方案 C + 定时轮询双模

**设计草案**:

```go
type WorkerPool struct {
    // ...
    scaleTrigger chan struct{} // 容量 1，非阻塞通知
}

// SubmitJob 热路径添加（仅当开启 AutoScaling 时）
if w.GetJobLen() > threshold {
    select {
    case w.pool.scaleTrigger <- struct{}{}:
    default: // 已有信号待处理，跳过
    }
}

// autoScaleWorkers 改为 select 双监听
select {
case <-p.ctx.Done():
    return
case <-ticker.C:
    // 定时兜底
case <-p.scaleTrigger:
    // 事件驱动，仍受 CoolDown 限制
}
```

### 影响分析

- 游戏服务器负载模式通常渐进上升（玩家逐步登录），非 spike 型
- `ResizeCoolDown = 2s` 在大多数场景已足够
- 改动价值在需要快速弹性（如大型活动开场瞬间涌入）时才明显

### 决策

✅ 已实施。采用方案 C + 定时轮询双模：
- WorkerPool 新增 `scaleTrigger chan struct{}` （容量 1）
- `DispatchJob` 检测到 worker 队列有积压时非阻塞通知
- `autoScaleWorkers` 改为 `select { ticker.C | scaleTrigger }` 双监听

---

## P3-b: context.WithValue 合并

### 背景

读 goroutine 路径中连续两次 `context.WithValue`（`worker.go` safeExecInternal）：

```go
ctx = context.WithValue(ctx, def.RWModeContextKey, def.RWModeRead)
ctx = context.WithValue(ctx, def.RWSourceServiceKey, w.pool.invoker.GetServiceName())
```

每次 `WithValue` 创建 32 字节 `valueCtx`，两次 = 64 字节/次读操作。

### 方案

合并为单结构体注入：

```go
// def/mailbox.go 新增
type RWContextInfo struct {
    Mode          RWMode
    SourceService string
}
type rwContextKeyType struct{}
var RWContextKey = rwContextKeyType{}

// worker.go 修改
ctx = context.WithValue(ctx, def.RWContextKey, def.RWContextInfo{
    Mode:          def.RWModeRead,
    SourceService: w.pool.invoker.GetServiceName(),
})
```

### 收益 vs 代价

| 收益 | 代价 |
|------|------|
| 每次读减少 1 次 WithValue 分配 (~32 字节) | 需同步修改所有读取端 |
| 10 万 QPS 下减少 ~3.2MB/s 短命分配 | 新旧 key 不兼容，需全量替换 |

### 决策

✅ 已实施。合并为 `RWContextKey` + `RWContextInfo` 结构体，旧 key 保留并标记 Deprecated。
读取端（`service.go PostJob`）和测试已同步更新。

---

## P4: PriorityScheduler 公平策略

### 背景

`StrategyFairness` 在相同最高优先级分组内选计数最少，
持续有 Sys 消息时 Normal/Low 可能延迟较大。

### 分析

**游戏场景下无实际问题**:
- Sys 消息 = 心跳/生命周期 → 低频、短耗时，快速消费完
- Urgent = 紧急 RPC → 偶发
- Normal/Low = 常规业务 → 高频

引入 DRR/WFQ 的代价:
- 调度器复杂度大幅增加（虚拟时间 / deficit 计数器）
- 破坏"高优先级**必须**优先"语义（心跳超时、踢人等不能被延迟）
- 热路径 NextJob 开销增加

### 决策

**不做改动**。当前设计对游戏场景足够。
如果将来出现 Sys 消息持续高频的场景，可考虑 Weighted Round Robin 作为折中方案。

---

## 实施计划

### 第一批（立即）

- [x] **P0**: 新建 `mailbox/worker_pool_rw_test.go`，补充 6 个集成测试场景
- [x] **P1-a**: `worker.go` execWrite — maxBackoff 改 5ms + 写等待 warning
- [x] **P1-b**: `dispatch_key_stats_middleware.go` — maxKeys 参数化

### 第二批（近期）

- [x] **P2**: 读 goroutine 池化 — per-WorkerPool 独立 ants 池 + `ants.WithNonblocking(true)` + fallback

### 第三批

- [x] **P3-a**: AutoScaler 异步 channel 触发（scaleTrigger + 定时轮询双模）
- [x] **P3-b**: context.WithValue 合并（RWContextKey + RWContextInfo）

### 不做

- **P4**: PriorityScheduler DRR/WFQ — 当前场景无需

### 额外修复

- **AdaptiveController lost wakeup**: `idle.go` 中 `AdaptiveController.Idle()` 缺少 `notified` 标记保护，导致 `BeginStop()` 的 `Wake()` 信号可能丢失。已修复为与 `Controller` 一致的 `for !c.notified { c.cond.Wait() }` 模式。
