# Actor 模块代码审查报告（2026 Q2）

- **审查范围**：`engine/pkg/actor/`（含 `mailbox/`、`mailbox/job/`）
- **审查时间**：2026-04-22
- **审查类型**：正确性 / 并发安全 / 性能 / 可维护性
- **审查人**：Code Review Agent

---

## 1. 总体评价

整体质量较高，代码经过多轮性能与并发修复（注释中可见 P1 修复、§10.14 RW 安全协议）。

**优点**

- **接口分层清晰**：`Mailbox` → `WorkerPool` → `Worker` → `IQueueManager` 解耦；中间件链与 RW 控制器抽离为独立组件。
- **并发原语使用得当**：大量 `atomic` + COW（中间件链、挂起策略列表）替代 RWMutex 热路径，配合 RLock-after-check 协议处理动态开关。
- **可观测性较完整**：`RWMetrics`、dispatchKey 分布统计、长 Job watchdog、unsafeDrainEvents 等。
- **资源契约写得很细**：`PostJob` 文档明确 "err 时由调用方 Release"，OnComplete + mctx 归还路径完整。
- **测试覆盖了关键并发点**：PID master flag、protojson 反序列化漏调 SyncMasterFlag 的提示性测试都齐备。

**主要短板**

- 部分清理路径（Sentinel 规则、PriorityQueueManager fallback）未启动期前置；
- 个别热点（per-Job timer、busy-spin Disable）有性能优化空间；
- 跨 goroutine 隐式契约较多（`PrepareForMarshal`、`MiddlewareContext.Reset`）缺乏强约束。

---

## 2. 发现的问题

### P1 优先级（建议立即修复）

#### 2.1 SentinelMiddleware.OnStop 未卸载已注册的规则

- **位置**：[engine/pkg/actor/mailbox/sentinel_middleware.go](engine/pkg/actor/mailbox/sentinel_middleware.go#L257-L275)
- **类型**：内存泄漏 / 全局表污染
- **原因**：`OnStart` 调用 `flow.LoadRulesOfResource(serviceName, rules)` 注册资源规则，`OnStop` 仅打印日志，未调用 `flow.ClearRulesOfResource` / `circuitbreaker.ClearRulesOfResource`。Service 反复创建/销毁时 Sentinel 全局注册表中累积已死资源规则，影响新 Service 加载同名资源的行为。
- **影响**：长期运行的节点反复重启 Service 时会出现规则数量持续增长。

#### 2.2 RWController.Disable 纯忙等到 deadline

- **位置**：[engine/pkg/actor/mailbox/rw_controller.go](engine/pkg/actor/mailbox/rw_controller.go#L83-L98)
- **类型**：CPU 浪费 / 性能
- **原因**：

  ```go
  for !rw.mu.TryLock() {
      if time.Now().After(deadline) { return ErrRWDisableTimeout }
      runtime.Gosched()
  }
  ```

  当存在长时间持锁的读 goroutine 时，`stopTimeout` 默认 10 秒会让当前 goroutine 满核 CPU 自旋。
- **影响**：动态切换 RW 模式或 Service 关闭路径出现 CPU 飙升。

#### 2.3 PriorityQueueManager fallback 队列可能不存在

- **位置**：[engine/pkg/actor/mailbox/queue_manager_priority.go](engine/pkg/actor/mailbox/queue_manager_priority.go#L94-L113)
- **类型**：运行时静默失败
- **原因**：未注册优先级走 fallback 时若 `m.fallbackPriority` 对应队列也不存在则返回 error；`Mailbox.PostJob` 调用 `ExecuteOnComplete` 后直接返回，按文档约定调用方需 `job.Release()`，但运行时丢消息问题被推后到了上层。
- **影响**：错误的配置（PriorityBatches 为空 map）会在运行时才暴露而非启动期。

---

### P2 优先级（性能 / 健壮性）

#### 2.4 safeExecInternal 每个 Job 都创建 time.AfterFunc

- **位置**：[engine/pkg/actor/mailbox/worker.go](engine/pkg/actor/mailbox/worker.go#L370-L380)
- **类型**：性能
- **原因**：watchdog 在每个 Job 入口处 `time.AfterFunc(maxExec, ...)` + `defer timer.Stop()`，在百万 QPS 场景下产生大量 Timer 入堆/出堆；项目已有 `utils/timingwheel` 可复用。
- **影响**：高 QPS 下 GC 压力与 timer 调度抖动。

#### 2.5 Worker.BeginStop 阶梯退避缺总体超时

- **位置**：[engine/pkg/actor/mailbox/worker.go](engine/pkg/actor/mailbox/worker.go#L246-L268)
- **类型**：可能永久阻塞
- **原因**：等待 `submitters.Load() != 0` 的循环没有 deadline，若上层存在死循环投递将永远阻塞。`sleep` 上限 100μs 后保持稳定但不退出。
- **影响**：故障状况下 Worker 关闭悬挂。

#### 2.6 discardExec rwUnsafe=true 路径下中间件 OnComplete 仍执行

- **位置**：[engine/pkg/actor/mailbox/worker.go](engine/pkg/actor/mailbox/worker.go#L286-L310)
- **类型**：潜在 race
- **原因**：rwUnsafe 路径跳过 `invoker.OnJobDiscarded` 是为避免与泄漏读 goroutine 争抢 Service 状态，但 `ExecuteOnComplete` 仍执行。如果中间件 `OnComplete` 访问 invoker 共享字段（自定义中间件回写 invoker 状态），同样会出现 race。
- **影响**：取决于中间件实现，框架无法保证安全。

#### 2.7 resizeWorkers 缩容后扩容会复用已删除 ID

- **位置**：[engine/pkg/actor/mailbox/worker_pool.go](engine/pkg/actor/mailbox/worker_pool.go#L260-L300)
- **类型**：可观测性 / 监控差分异常
- **原因**：扩容用 `for i := currentCount; i < newSize; i++` 作为 ID。反复缩-扩会复用刚被 delete 的 ID。一致性哈希语义下不影响正确性，但 `dispatchCnt[id]` 的累计监控做差分时会出现负值。
- **影响**：监控/告警误报。

---

### P3 优先级（可维护性 / 小优化）

#### 2.8 MiddlewareContext.Reset 与池 reset 钩子双实现

- **位置**：[engine/pkg/actor/mailbox/middleware_chain.go](engine/pkg/actor/mailbox/middleware_chain.go#L113-L131)
- **类型**：维护风险
- **原因**：`Reset(ctx, job, serviceName)` 公开方法没有任何调用方；池的 reset 钩子是另一段代码，两者行为略有差异（池钩子在 `len > 64` 时重建 map，`Reset` 不重建）。
- **影响**：未来修改易出现不一致。

#### 2.9 PID PrepareForMarshal 跨 goroutine 隐式契约

- **位置**：[engine/pkg/actor/pid.go](engine/pkg/actor/pid.go#L57-L93)
- **类型**：易踩坑约束
- **原因**：注释要求"调用方保证同一 PID 同时只有一个 goroutine 在执行 PrepareForMarshal + Marshal，否则需 proto.Clone"。仓库内多处 RPC envelope / etcd 注册路径都会触发，缺少编译期/运行期检查。新加的序列化路径忘记复制 PID 即可重新引入 race。
- **影响**：长期演进风险。

#### 2.10 CircuitBreaker windowStart 仅在 OnComplete 触发刷新

- **位置**：[engine/pkg/actor/mailbox/circuit_breaker_middleware.go](engine/pkg/actor/mailbox/circuit_breaker_middleware.go#L227-L243)
- **类型**：策略偏差
- **原因**：failure 计数不会随时间自动衰减；突发尖峰错误后流量归零，failures 累积到下一次 OnComplete 才被重置。若计数已达阈值无影响；接近阈值但未达到时会有"虚假累加"。
- **影响**：边界场景下熔断更激进。

#### 2.11 DispatchKeyStatsMiddleware maxKeys 按 shard 平均上限

- **位置**：[engine/pkg/actor/mailbox/dispatch_key_stats_middleware.go](engine/pkg/actor/mailbox/dispatch_key_stats_middleware.go#L142-L150)
- **类型**：统计偏差
- **原因**：`len(s.counts) < m.maxKeys/dispatchKeyShards` 强制按 shard 平均分配上限。热点 key 集中到一两个 shard 时，部分 shard 早早达到上限，与文档语义"maxKeys 总键数上限"不符。
- **影响**：调试统计可能丢失部分长尾 key。

#### 2.12 execRead 内层 spin 不响应 Closed 标志

- **位置**：[engine/pkg/actor/mailbox/worker.go](engine/pkg/actor/mailbox/worker.go#L437-L518)
- **类型**：响应延迟
- **原因**：`for i:=0; i<yieldCount; i++ { runtime.Gosched() }` 内层 spin 进入后不再检查 closed 状态，最多需 64 次 Gosched 后才在外层循环顶部响应停机。
- **影响**：极端写饥饿场景下 Stop 响应略有延迟，可接受。

---

## 3. 改进建议（按优先级）

| 优先级 | 项 | 建议 |
|--------|-----|-----|
| P1 | 2.1 | `SentinelMiddleware.OnStop` 调用 `flow.ClearRulesOfResource` 等 API |
| P1 | 2.2 | `RWController.Disable` 改用阶梯退避（μs → ms 上限） |
| P1 | 2.3 | `NewPriorityQueueManager` 启动期强制注入默认队列 |
| P2 | 2.4 | watchdog 迁移到时间轮，消除 per-Job AfterFunc |
| P2 | 2.5 | `BeginStop` 加总体 deadline，超时打 error 日志 |
| P2 | 2.6 | rwUnsafe 路径下中间件 OnComplete 也跳过，或文档明示 race 风险 |
| P2 | 2.7 | worker ID 改为单调递增（注意：需同步改造缩容路径，见 §4.4 说明） |
| P3 | 2.8 | 删除 `MiddlewareContext.Reset` 或让池 reset 钩子调用它 |
| P3 | 2.9 | 提供 `MarshalPID(pid) []byte` 封装，强约束序列化出口 |
| P3 | 2.10 | CircuitBreaker 在 OnReceive 也检查 windowStart 过期 |
| P3 | 2.11 | reportAndReset 时统一裁剪 maxKeys，而非 shard 平均 |
| P3 | 2.12 | execRead 内层 spin 增加 closed 状态检查 |

---

## 4. 示例修改

### 4.1 RWController.Disable 退避（P1）

```go
func (rw *RWController) Disable() error {
    if !rw.enabled.Load() {
        return nil
    }
    deadline := time.Now().Add(rw.stopTimeout)
    backoff := time.Duration(0)
    const maxBackoff = 5 * time.Millisecond
    for !rw.mu.TryLock() {
        if time.Now().After(deadline) {
            return ErrRWDisableTimeout
        }
        if backoff == 0 {
            runtime.Gosched()
            backoff = time.Microsecond
        } else {
            time.Sleep(backoff)
            backoff *= 2
            if backoff > maxBackoff {
                backoff = maxBackoff
            }
        }
    }
    rw.enabled.Store(false)
    rw.mu.Unlock()
    return nil
}
```

### 4.2 SentinelMiddleware.OnStop 卸载规则（P1）

> `flow.ClearRulesOfResource` / `circuitbreaker.ClearRulesOfResource` 签名均为
> `func ClearRulesOfResource(res string) error`（单返回值）。

```go
func (m *SentinelMiddleware) OnStop() {
    if len(m.flowRules) > 0 {
        if err := flow.ClearRulesOfResource(m.serviceName); err != nil && m.logger != nil {
            m.logger.Warnf("Sentinel clear flow rules failed: %v", err)
        }
    }
    if len(m.circuitBreakerRules) > 0 {
        if err := circuitbreaker.ClearRulesOfResource(m.serviceName); err != nil && m.logger != nil {
            m.logger.Warnf("Sentinel clear circuit breaker rules failed: %v", err)
        }
    }
    // system rules 全局共享，由 sync.Once 控制，OnStop 不卸载
    if m.logger != nil {
        m.logger.Infof("SentinelMiddleware stopped: service=%s", m.serviceName)
    }
}
```

### 4.3 NewPriorityQueueManager 启动期校验（P1）

```go
// 在 sortedPriorities 与 fallbackPriority 计算后追加：
if _, ok := m.queues[m.fallbackPriority]; !ok {
    m.queues[def.PriorityNormal] = mpsc.New[inf.IMailboxJob]()
    m.batchSizes[def.PriorityNormal] = 8
    m.sortedPriorities = append(m.sortedPriorities, def.PriorityNormal)
    sort.Slice(m.sortedPriorities, func(i, j int) bool {
        return m.sortedPriorities[i] < m.sortedPriorities[j]
    })
    m.nextJobBuf = make([]def.Priority, len(m.sortedPriorities))
    m.fallbackPriority = def.PriorityNormal
}
```

### 4.4 worker ID 单调递增（P2）

> **注意**：此修改**不可单独应用于扩容路径**。当前缩容路径假设 worker ID 为
> `[0, currentCount)` 连续区间（`for i := newSize; i < currentCount; i++`），
> 改为单调递增后活跃 ID 不再连续，缩容循环将查不到任何 worker。
>
> **完整方案**：引入 `nextWorkerID atomic.Int32` 用于分配 ID，同时改造缩容路径
> 为遍历 `workers` map 按 ID 降序淘汰多余 worker。以下仅展示扩容部分的改动思路：

```go
type WorkerPool struct {
    // ...existing fields...
    nextWorkerID atomic.Int32 // 单调递增 worker ID 分配器
}

// ---- 扩容路径 ----
for n := newSize - currentCount; n > 0; n-- {
    id := p.nextWorkerID.Add(1) - 1
    worker := newWorker(id, p.conf, p.workerEnv(), p.drainPolicy)
    p.workers[id] = worker
    worker.Start()
    p.ring.Add(id)
    if p.statsEnabled {
        p.dispatchCnt[id] = &atomic.Uint64{}
    }
}
p.workerCount.Store(newSize)

// ---- 缩容路径（需同步改造）----
// 不能再用 for i := newSize; i < currentCount; i++ 按区间删除，
// 应改为：收集所有活跃 ID → 按 ID 降序排列 → 取末尾 (currentCount-newSize) 个淘汰。
// 示例伪代码：
//   ids := sortedActiveIDs()           // 升序
//   toRemove := ids[newSize:]          // 保留前 newSize 个
//   for _, id := range toRemove { ... }
```

---

## 5. 审查修订记录

| 日期 | 修订内容 |
|------|--------|
| 2026-04-22 | 初版（13 条问题） |
| 2026-04-22 R1 | ① 移除原 2.8 `SetRWEnabled(true)` 内存可见性问题——Go 1.19+ 内存模型下 atomic Store 被 Load 观察到时保证 happens-before，`EnsureReadResources` 的写按程序序先于 `enabled.Store(true)`，因此其他 goroutine 在 `enabled.Load()==true` 后一定能看到 `readSem`/`readPool` 的最新值，非 race。② 修复 §4.2 示例 `ClearRulesOfResource` 调用：该 API 返回单值 `error`，原示例 `_, err :=` 编译不通过，改为 `err :=`。③ 补全 §4.4 示例缩容路径说明：单调递增 ID 后活跃 ID 不再连续，需同步改造缩容逻辑。问题编号顺延重排（12 条）。 |

---

## 6. 附录：未发现明显问题的子模块

以下子模块经审查未发现 P1/P2 级别问题：

- `engine/pkg/actor/event.go`：仅类型转换包装。
- `engine/pkg/actor/mailbox/queue_manager.go` / `queue_manager_dual.go`：接口与双队列实现简洁正确。
- `engine/pkg/actor/mailbox/scheduler.go`：明确文档"单 Worker 独占"语义；`resetCountersIfNeeded` 防溢出策略合理。
- `engine/pkg/actor/mailbox/strategy.go` / `strategy_factory.go` / `scaler.go`：策略组合 + 注册表 + clamp 实现规范。
- `engine/pkg/actor/mailbox/suspend_policy.go` / `stop_policy.go`：COW + atomic.Pointer 用法标准。
- `engine/pkg/actor/mailbox/format.go`：无分配整数/百分比格式化，性能友好。
- `engine/pkg/actor/mailbox/job/job.go` / `job_factory.go`：sync.Pool + 引用计数 + 冻结注册表，资源契约清晰。
- `engine/pkg/actor/mailbox/rate_limit_middleware.go`：基于 `golang.org/x/time/rate`，简洁正确。

---

**审查结论**：模块整体可投入生产；建议按 P1 → P2 → P3 顺序修复；P1 项不修可能在长期运行 / 反复重启场景下出现 CPU 飙升和内存泄漏。
