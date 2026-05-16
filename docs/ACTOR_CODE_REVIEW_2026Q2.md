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

> **R6 复核提示**：本节为 2026-04-22 初版审查记录。复核 `56879a4c` 后，§2.2、§2.3、§2.4、§2.5、§2.6 中有部分描述已确认不符合该提交真实代码或已属降级路径，具体纠偏见 §11.1；当前后续修复优先级以 §10/§11 为准。

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
| 2026-05-16 R2 | 新增第二轮审查：提交 `0e3e842`（actor 审查改造），详见 §7。 |
| 2026-05-16 R3 | 新增第三轮临时合并审查：提交 `f1c6287`+`d442412`，详见 §8；该节保留历史记录，后续以 §9/§10 的拆分审查与 §11 复核修正为准。 |
| 2026-05-16 R4 | 新增第四轮审查：提交 `f1c6287`（actor 目录优化 part-2），详见 §9。新增 R4-M1（panicRateLimiter 懒初始化竞争）、R4-M2（自定义 JobType Sentinel 规则需配套 job-type resourceFunc）、R4-L1（nil 守护冗余）；确认 §2.1/R3-M1/R2-H1/R2-H2/R3-M3 已修复。 |
| 2026-05-16 R5 | 新增第五轮审查：提交 `d442412`（HEAD，actor 目录优化 part-3），详见 §10。确认 R4-M1/R2-M1 已修复；新增 R5-M1（readPipeline 退出兜底不硬）、R5-M2（Authorizer 指针替换限制）、R5-M3/M4/M5（authz/health/tlsx 遗漏问题）、R5-L1/L2。 |
| 2026-05-16 R6 | 复核同事审查覆盖的四个提交：`56879a4c`、`0e3e8428`、`f1c6287b`、`d4424121`，修正 §2/§7/§8/§9/§10 中的误判、误归属与遗漏，详见 §11。 |
| 2026-05-16 R7 | 安全漏洞复核，新增 §12。发现 R7-H1（WS Origin）、R7-M1~M6 共 7 项安全风险。 |
| 2026-05-16 R8 | 修复 R5-M3/R5-M4/R5-M5/R3-M2/R7-M2/R7-M3/R7-M5 共 7 项问题；`go build`/`go vet`/`go test ./...` 全量通过。 |
| 2026-05-16 R9 | 修复 R5-M1（readPipeline 硬超时）、R7-H1（WSServer Origin 白名单）、R7-M1（Gate WS Origin 白名单）、R7-M4（模板/示例默认密码改占位符）共 4 项；`go build`/`go vet`/`go test ./...` 全量通过。 |
| 2026-05-16 R10 | 修复 R5-M2（`SetAuthorizer` 改 `atomic.Pointer`）、R6-L1~L4（staticcheck 清理）、R7-M5 完善（Gin RawQuery 脱敏）、R7-M6 完善（`insecureSkipVerify` + CA 矛盾拒绝）共 7 项；`go build`/`go vet`/`go test ./...` 全量通过。 |
| 2026-05-16 R11 | 修复 R2-M2（`NewPriorityQueueManager`/`NewPriorityScheduler` 配置深拷贝，不再原地默认化调用方配置）、R4-M2（`WithJobType*Rule` 自动启用 job-type resourceFunc，规则运行时可正确命中）共 2 项；新增 `queue_manager_priority_test.go`、`scheduler_test.go` 回归测试；`go build`/`go vet`/`go test ./...` 全量通过。 |

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

---

## 7. 第二轮审查：`0e3e842`（actor 审查改造）

- **审查时间**：2026-05-16
- **范围**：`0e3e8428~1..0e3e8428`，共 18 个 Go 文件变更
- **工具**：`go build ./engine/...` ✅  `go vet ./engine/...` ✅  `go test ./engine/pkg/actor/... -count=1` ✅

### 7.1 本次提交修复的历史问题

| 历史问题 | 修复情况 |
|---------|---------|
| §2.6 rwUnsafe 中间件 OnComplete race | ✅ 新增 `discardJob()` 集中回调 + 释放，明确了框架内 recover |
| §2.7 resizeWorkers 缩容与 ring 移除顺序错误（可并发执行 per-key Job） | ✅ 改为"先 BeginStop+Wait drain 完 → 再从 ring/workers 移除"，彻底消除同 key 并行执行窗口 |
| §2.9 PID.PrepareForMarshal 跨 goroutine race | ✅ 新增 `SnapshotForWire(pid)` 封装 Clone+PrepareForMarshal，调用方改用独立副本 |
| §2.5 BeginStop 无全局超时（submitters 等待） | ✅ 复核修正：`56879a4c` 基线中已存在 `deadline := time.Now().Add(w.env.rw.stopTimeout)`，该条属于初审误判 |
| §2.4 per-Job AfterFunc watchdog | ✅ 复核修正：`56879a4c` 已优先使用 `watchdogScheduler` 时间轮，`time.AfterFunc` 仅为 scheduler 不可用时的降级路径 |
| §2.1 Sentinel OnStop 未卸载规则 | ⚠️ 本次未改动 sentinel_middleware.go，问题遗留 |
| §2.2 RWController.Disable 忙等 | ⚠️ 未在本次提交修复 |

### 7.2 新发现问题

#### R2-H1 `dispatchRead` 直接调用 `invoker.OnJobDiscarded` 无 panic 保护

- **位置**：[engine/pkg/actor/mailbox/worker.go](engine/pkg/actor/mailbox/worker.go) `dispatchRead`
- **类型**：高 — 健壮性
- **原因**：回压丢弃路径直接调 `w.env.invoker.OnJobDiscarded(job, ...)` 没有 `recover`。若 invoker 实现在 `OnJobDiscarded` 中 panic，将导致 readPipeline goroutine 崩溃，后续所有读 Job 永久阻塞（readCh 不再消费）。
- **状态**：已在后续提交 `d442412` 中通过 `safeNotifyJobDiscarded` 包装修复 ✅

#### R2-H2 `run()` defer 中 `readPipelineWg.Wait()` 无超时

- **位置**：[engine/pkg/actor/mailbox/worker.go](engine/pkg/actor/mailbox/worker.go) `run()` defer 块
- **类型**：高 — 可能阻塞 Stop
- **原因**：`close(w.readCh)` 后直接 `w.readPipelineWg.Wait()`，若 `launchRead` 内 `bo.Backoff()` 死循环（如 `writeRequested` 永不归零）则永久阻塞，导致整个 Stop 路径挂住。
- **状态**：已在后续提交 `d442412` 中添加 `stopDeadlineNS` + `time.After(stopTimeout)` 兜底修复 ✅

#### R2-M1 `discardJob` 的 `defer job.Release()` 在 panic 路径下可能双释放

- **位置**：[engine/pkg/actor/mailbox/mailbox.go](engine/pkg/actor/mailbox/mailbox.go) `discardJob`
- **类型**：中 — 资源契约
- **原因**：
  ```go
  defer func() {
      if r := recover(); r != nil { ... }
      job.Release()  // ← 无论如何都执行
  }()
  invoker.OnJobDiscarded(job, reason)
  ```
  若 `OnJobDiscarded` 内部已经调用 `job.Release()`（业务方误实现），`defer` 中的 `Release()` 将造成二次释放。`dto.DataRef` 的 CAS 可以防止实际双 Put，但会打印警告、使统计失真。
- **建议**：在 `IMessageInvoker.OnJobDiscarded` 接口注释中明确"**禁止**在此方法内调用 `job.Release()`"（已在接口文档中有所说明，但可加强为编译期检测或 debug-only 断言）。
- **状态**：接口文档在本次提交已更新声明；业务层无法强约束，属合理折中。

#### R2-M2 `MultiLevelQueueConf` 原地默认化可能污染共享配置模板

- **位置**：[engine/pkg/actor/mailbox/worker_pool.go](engine/pkg/actor/mailbox/worker_pool.go) `cloneMailboxConfForFix`
- **类型**：中 — 潜在配置污染
- **原因**：复核后修正原描述：`fixConf` 本身不直接写 `SchedulePolicy.MultiLevelQueueConf`；真正风险在 `NewPriorityQueueManager` 对传入的 `MultiLevelQueueConf` 做默认化时，可能原地补充 `PriorityBatches`。若多个 Service 共享同一个 `MultiLevelQueueConf` 指针，仍可能污染共享配置模板。
- **建议**：在 `cloneMailboxConfForFix` 中追加对 `MultiLevelQueueConf` 及其 `PriorityBatches` map 的深拷贝，或让 `NewPriorityQueueManager` 先复制配置再做默认化。

#### R2-P1 `AutoScaler.ShouldResize` 冷却更新位置修复但边界仍有死角

- **位置**：[engine/pkg/actor/mailbox/scaler.go](engine/pkg/actor/mailbox/scaler.go)
- **类型**：低 — 可观测性
- **原因**：本次修复将 `s.lastResizeTime = now` 移至策略结果判断之外，确保"未触发任何动作"的路径也推进冷却。但 `clamp` 命中边界（`newSize == cur`）时冷却仍然推进，此场景下"没有实际 resize"但使用了冷却 token，下一次有效负载变化可能因冷却而推迟响应。
- **影响**：轻微，可接受；若需精确语义可在 `newSize != cur` 时才推进 `lastResizeTime`。

### 7.3 第二轮审查结论

**状态：✅ 可合并（高危问题均在后续提交修复）**

本次提交是对第一轮审查的集中响应，核心修复包括：
- ADR-4 所有权契约统一（`discardJob`、`PostJob` 移除外部 Release）
- ADR-3 readPipeline 架构（主循环解耦读操作）
- 缩容顺序修正（P0-2）
- `SnapshotForWire` 消除 PID marshal race（ADR-1）
- `cloneMailboxConfForFix` 防配置模板污染（P1-9）

R2-H1/H2 已在后续 `d442412` 提交中修复；R2-M1/M2 建议在后续迭代处理。

---

## 8. 第三轮临时合并审查：`f1c6287`+`d442412`（actor 目录优化）

- **审查时间**：2026-05-16
- **范围**：`0e3e8428..HEAD`（两个连续提交），170+ 文件，核心 Go 变更集中于 `engine/pkg/actor/mailbox/`、`engine/pkg/utils/pool/`、`engine/pkg/utils/tlsx/`、`engine/pkg/utils/errorx/`
- **工具**：`go build ./engine/...` ✅  `go vet ./engine/...` ✅  `go test ./engine/pkg/actor/... -count=1` ✅
- **复核说明**：本节是对 `f1c6287` 与 `d442412` 的早期合并审查，后续已在 §9/§10 拆分到单提交粒度。经 R6 复核，`R3-M1`（Sentinel map 泄漏）对 `f1c6287` 后代码不成立，`R3-M3` 标题与原因表述不准确；最终结论以 §9/§10/§11 为准。

### 8.1 修复的历史/上轮问题

| 问题 | 修复情况 |
|-----|---------|
| R2-H1 `dispatchRead` 无 panic 保护 | ✅ 新增 `safeNotifyJobDiscarded` 包装，全链路 recover |
| R2-H2 `readPipelineWg.Wait()` 无超时 | ✅ 新增 `stopDeadlineNS`、`pipelineDone` channel + `time.After(stopTimeout)` 兜底 |
| §2.7 worker ID 监控差分 | ✅ 引入 `nextWorkerID atomic.Int32` 单调递增分配，配合 COW snapshot 实现 |
| §2.9 PID marshal race | ✅ 调用方改用 `SnapshotForWire`（已在 R2 引入） |

### 8.2 新发现问题

#### R3-H1 `dispatchRead` 回压路径仍有潜在双释放

- **位置**：[engine/pkg/actor/mailbox/worker.go](engine/pkg/actor/mailbox/worker.go) `dispatchRead`
- **类型**：高 — 资源契约
- **原因**：
  ```go
  w.safeNotifyJobDiscarded(job, def.ErrMailboxWorkerIsFull)  // ① 通知业务
  if mctx != nil {
      w.env.middlewareChain.ExecuteOnComplete(mctx, ...)     // ② 中间件回调
  }
  job.Release()  // ③ 释放
  ```
  `safeNotifyJobDiscarded` 调用 `invoker.OnJobDiscarded(job, reason)`。若业务实现在 `OnJobDiscarded` 内调 `job.Release()`（违反接口契约），③ 处将产生双释放。`dto.DataRef` CAS 可防双 Put，但统计与日志失真。
- **建议**：在接口注释中强化"禁止在 `OnJobDiscarded` 中调用 `job.Release()`"，debug 模式可加 `DataRef.IsRef()` 断言；或参考 `discardJob` 模式，将 `Release()` 纳入 defer 并仅执行一次。

#### R3-M1 全局 Sentinel 规则 map 无清理（遗留自初始审查 §2.1）

- **位置**：[engine/pkg/actor/mailbox/sentinel_middleware.go](engine/pkg/actor/mailbox/sentinel_middleware.go)
- **类型**：中 — 内存泄漏 / 规则污染
- **原因**：本轮重构新增 `sentinelFlowRules`/`sentinelBreakerRules`/`sentinelSystemRules` 三个全局 `map[string]...`，按 owner（service name）聚合规则。`OnStart` 注册，但 `OnStop` 没有对应 `delete(sentinelFlowRules, owner)` 逻辑。动态创建/销毁 Service 时全局 map 持续增长，且 merge 后的 Sentinel 规则中保留已死 Service 的限流策略。
- **建议**：`SentinelMiddleware.OnStop` 中加入：
  ```go
  sentinelRulesMu.Lock()
  delete(sentinelFlowRules, m.owner)
  delete(sentinelBreakerRules, m.owner)
  delete(sentinelSystemRules, m.owner)
  reloadSentinelFlowRulesLocked(resource, m.logger)          // 各 resource
  reloadSentinelBreakerRulesLocked(resource, m.logger)
  reloadSentinelSystemRulesLocked(m.logger)
  sentinelRulesMu.Unlock()
  ```

#### R3-M2 `panicRateLimiter.Allow()` CAS 自旋无退避

- **位置**：[engine/pkg/actor/mailbox/worker_pool.go](engine/pkg/actor/mailbox/worker_pool.go) `panicRateLimiter.Allow`
- **类型**：中 — 性能（panic storm 场景）
- **原因**：
  ```go
  for {
      cur := l.tokens.Load()
      if cur <= 0 { return false }
      if l.tokens.CompareAndSwap(cur, cur-1) { return true }
      // CAS 失败：无退避，直接重试
  }
  ```
  panic storm 时大量并发 goroutine 在此自旋，CPU 热点与引入 limiter 的初衷相悖。
- **建议**：加 `runtime.Gosched()` 或改用 `atomic.Int32.Add(-1)` + 事后修正令牌上限，消除 CAS 自旋。

#### R3-M3 `resizeWorkers` 缩容全程持 `p.mu`，`BeginStop`+`Wait` 阻塞 dispatch

- **位置**：[engine/pkg/actor/mailbox/worker_pool.go](engine/pkg/actor/mailbox/worker_pool.go) `resizeWorkers` 缩容分支
- **类型**：中 — 延迟/吞吐
- **原因**：当前缩容注释说"dispatch 全程无锁"，但实际 `BeginStop`+`Wait` 都在 `p.mu` 外执行（先 Unlock 再 BeginStop，再 Lock 移除 ring）；但扩容分支 `p.mu.Lock()` + `defer p.mu.Unlock()` 包住了整个扩容过程（包括 `worker.Start()`），高 QPS 下扩容期间 dispatch 会退化为串行等待。
- **建议**：扩容路径也改为"先构建新 workers → Start → 再持锁一次性 publish snapshot"，缩短锁持有时间。

#### R3-P1 `MiddlewareContext.data` 去锁的不变量未在代码层面强制

- **位置**：[engine/pkg/actor/mailbox/middleware_chain.go](engine/pkg/actor/mailbox/middleware_chain.go)
- **类型**：低 — 可维护性
- **原因**：移除 `sync.RWMutex` 后，正确性依赖"OnReceive 在 producer goroutine、OnComplete 在 worker goroutine、happens-before 由 mpsc queue 建立"的隐式不变量。RW 模式下 `dispatchRead` 将 mctx 转移到 readPipeline goroutine，若业务中间件错误地在 `OnComplete` 回调之后还持有 mctx 引用并访问 `data`，会出现 race。
- **建议**：在 `MiddlewareContext` 注释中补充"mctx 所有权转移时序：PostJob 成功后 producer **不得**再访问 mctx"；考虑 debug 模式下在 `ctxPool.Put` 时将 `data` 置为 nil 以使越界访问 panic 快速暴露。

### 8.3 亮点

| 改动 | 评价 |
|---|---|
| `workersSnapshot` COW + `atomic.Pointer` | 彻底消除 dispatch 热路径 RWMutex，正确 |
| `readPipeline` goroutine | 主循环让出 CPU，写 Job 与系统消息优先级提升，并发模型清晰 |
| `dispatchRing`（jump consistent hash） | O(log N) 无虚节点，内存与 CPU 均优于原哈希环 |
| `safeNotifyJobDiscarded` + 全族 `safe*` | panic 隔离完整，所有中间件生命周期回调受保护 |
| `ExecuteFrameworkCleanup` | 框架资源清理（Sentinel entry）与业务 OnComplete 分离，unsafe drain 语义正确 |
| `SnapshotForWire(pid)` | 消除 PID marshal 并发写 race |
| `MethodMgr.isRWEnabled func() bool` | 封装优于 `*atomic.Bool` 暴露，未来可扩展多状态 |
| `rwReadCtxInfo` 预构造 | 热路径 struct + 字符串拷贝归零 |
| `statsEnabled` 门控 `count.Add(1)` | release 构建零开销 |
| `switchableStatsRecorder` | 动态开关统计，pool.GetStats() 跳过空 entry，清晰 |

### 8.4 第三轮审查结论

**状态：⚠️ 警告（可合并，建议跟进 R3-M1 和 R3-H1）**

- 无关键安全或并发正确性问题
- **R3-H1**（`dispatchRead` 双释放风险）：建议补充接口层 debug 断言，防止 invoker 实现误用
- **R3-M1**（Sentinel 全局 map 泄漏）：`OnStop` 未清理，动态 Service 场景内存持续增长，建议本轮修复
- **R3-M2**（panicRateLimiter CAS 自旋）：低频路径，可加 `runtime.Gosched()` 简单规避
- **R3-M3**（扩容持锁时间长）：吞吐优化，可作为后续 backlog 跟踪

**审查结论**：模块整体可投入生产；建议按 P1 → P2 → P3 顺序修复；P1 项不修可能在长期运行 / 反复重启场景下出现 CPU 飙升和内存泄漏。


---

## 9. 第四轮审查（2026-05-16） commit `f1c6287b`（actor 目录优化）

- **commit**：`f1c6287b`
- **父 commit**：`0e3e8428`（R2 审查改造）
- **文件变更**：46 文件，约 +3200 / -1200 行
- **构建/vet/测试**：`go build ./engine/...`   `go vet ./engine/...`   `go test ./engine/pkg/actor/... -count=1 -timeout 30s` 

### 9.1 本轮主要改动

| 改动 | 文件 | 说明 |
|---|---|---|
| Sentinel 多实例 owner 聚合 | `sentinel_middleware.go` | 用 `owner` key 替代全局 `sync.Once`，`OnStop` 删除并重载，彻底修复 2.1/R3-M1 |
| `circuit_breaker_middleware.go` 删除 |  | CB 功能迁移至 `SentinelMiddleware`，通过新 `WithCircuitBreakerRule` 系列 option 配置 |
| `dispatchRing`（新文件） | `dispatch_ring.go` | jump consistent hash（Lamping & Veach 2014），O(log N)，无虚节点 |
| `workersSnapshot` COW | `worker_pool.go` | `sync.Mutex` 替代 `sync.RWMutex`，`DispatchJob` 完全无锁 |
| `writeRequested` 移入 Worker | `worker.go` | 每个 Worker 独立门控，消除跨 Worker 竞争 |
| `stopDeadlineNS` + `time.After` 超时 | `worker.go` | 修复 R2-H2 readPipelineWg 无超时 |
| `safeNotifyJobDiscarded` | `worker.go` | 修复 R2-H1 dispatchRead 无 panic 保护 |
| `frameworkCleanupMiddleware` + `ExecuteFrameworkCleanup` | `middleware_chain.go` | 框架清理与业务 OnComplete 分离，unsafe drain 语义正确 |
| `SetPanicHandler` 后置注入 | `middleware_chain.go` / `worker_pool.go` | 构造后设置，避免循环依赖 |
| `samePriorityBuf` 复用 | `scheduler.go` | 消除 `PriorityScheduler.NextJob` 每次调用的切片分配 |
| `SysCtlRegistry`（新文件） | `core/sysctl_registry.go` | 框架内部命令注册中心，内置 suspend/resume/healthcheck，消除 `handleSysCtl` TODO 桩 |
| `MethodMgr.isRWEnabled func() bool` | `core/rpc/handler.go` | 封装 `*atomic.Bool` 为闭包，隔离内部表示 |
| `xxhash` 替换截断 FNV | `dispatch_key_stats_middleware.go` | 全字节哈希，避免长公共前缀导致的分片偏置 |
| `switchableStatsRecorder` | `utils/pool/stats_recorder.go` | 动态开关统计，`pool.GetStats()` 跳过空 entry |
| `VirtualWorkerRate` 标记 Deprecated | `config/define.go` | jump hash 无需虚节点，保留字段兼容已有 yaml 配置 |

### 9.2 发现的问题

#### R4-M1 `panicRateLimiter.Allow()` 延迟初始化存在数据竞争

- **位置**：`engine/pkg/actor/mailbox/worker_pool.go`  `panicRateLimiter.Allow()`
- **类型**：中  数据竞争（`go test -race` 可复现）
- **原因**：`panicRateLimiter` 作为值类型嵌入 `WorkerPool`，其 `Allow()` 对普通 `int64` 字段 `burstCap` 做 `if l.burstCap <= 0` 检查并原地写入。多个 Worker goroutine 在第一次 panic 时并发调用 `Allow()`，会产生非原子读写虽然幂等（值始终为 5），仍属 Go 内存模型中的数据竞争，`-race` 会报告。

```go
// 当前实现（存在竞争）
func (l *panicRateLimiter) Allow() bool {
    if l.burstCap <= 0 {     //  非原子读
        l.burstCap = 5       //  非原子写
        l.tokens.Store(l.burstCap)
        ...
    }
}
```

- **建议**：在 `NewWorkerPool` 中显式初始化，消除运行时懒加载：

```go
pool.panicStackLimiter = panicRateLimiter{burstCap: 5, intervalNS: int64(time.Second)}
```

#### R4-M2 自定义 JobType Sentinel 规则需配套 job-type resourceFunc

- **位置**：`engine/pkg/actor/mailbox/sentinel_middleware.go`  `sentinelJobTypeResources()`
- **类型**：中  功能完整性 / 易用性陷阱
- **复核修正**：原先认为 `WithJobTypeFlowRule` / `WithJobTypeCircuitBreakerRule` 不会把自定义 resource 加入 `ruleResources`，这是不准确的；实际二者会经 `addFlowRulesForResource` / `addCircuitBreakerRulesForResource` 自动追加 resource。
- **真正风险**：普通 `NewSentinelMiddleware` 默认 `resourceFunc == nil`，运行时 `Sentinel.Entry` 使用的仍是 `serviceName`；即使自定义 JobType 规则已加载到 `serviceName:jobType`，也不会命中，除非使用 `NewSentinelMiddlewareWithJobType` 或显式 `WithResourceFunc` 返回对应 job-type resource。
- **建议**：在 `WithJobTypeFlowRule` / `WithJobTypeCircuitBreakerRule` 注释中明确“规则加载不等于运行时命中；必须配套 job-type resourceFunc”。也可在构造期检测到 job-type 规则但 `resourceFunc == nil` 时输出 warn。

#### R4-L1 `handleSysCtl` 中的 `sysCtlRegistry == nil` 守护冗余

- **位置**：`engine/pkg/core/handler_job.go`  `handleSysCtl`
- **类型**：低  代码质量
- **原因**：`initSysCtlRegistry()` 在 `Init` 中必然执行，`sysCtlRegistry` 不可能为 nil；nil 守护分支会误导读者认为存在未初始化路径。
- **建议**：删除 nil 守护，或将 `sysCtlRegistry` 改为接口类型以便测试注入 stub。

### 9.3 已修复问题汇总

| 历史问题 | 状态 |
|---|---|
| 2.1 / R3-M1 Sentinel `OnStop` 全局 map 泄漏 |  已修复（owner 聚合 + OnStop 删除重载） |
| R2-H1 `dispatchRead` 无 panic 保护 |  已修复（`safeNotifyJobDiscarded`） |
| R2-H2 `readPipelineWg` 无超时 |  已修复（`stopDeadlineNS` + `time.After`） |
| R3-M3 扩容全程持 `p.mu` |  部分修复（COW 使 dispatch 无锁；`worker.Start()` 仍在锁内，但影响窗口大幅缩短） |
| `writeRequested` 跨 Worker 竞争 |  已修复（移入 Worker 值内） |

### 9.4 仍未修复的历史问题

| 历史问题 | 说明 |
|---|---|
| 2.2 `RWController.Disable` busy-wait | 未在本轮改动 |
| R2-M2 `MultiLevelQueueConf` 原地默认化可能污染共享配置模板 | ✅ R11 已修复 |

### 9.5 亮点

| 改动 | 评价 |
|---|---|
| Sentinel owner 聚合 + `OnStop` 清理 | 多实例并存语义正确，彻底修复长期泄漏问题 |
| `circuit_breaker_middleware.go` 删除 | 消除重复实现，统一由 Sentinel 管理 CB 逻辑 |
| `dispatchRing` jump consistent hash | 算法简洁，无虚节点，扩缩容影响最小 |
| `frameworkCleanupMiddleware` / `ExecuteFrameworkCleanup` | 框架清理与业务 OnComplete 彻底分离，unsafe drain 不遗漏 Sentinel entry 归还 |
| `SysCtlRegistry` + 内置命令 | 消除 `handleSysCtl` TODO 桩，suspend/resume/healthcheck 可测试 |
| `isRWEnabled func() bool` 闭包 | 隔离内部原子细节，未来扩展多状态无需改调用方 |
| `xxhash` 替换截断 FNV | 全字节哈希，长公共前缀场景分布更均匀 |
| `switchableStatsRecorder` | 统计门控与业务逻辑解耦，零开销 release 构建 |

### 9.6 第四轮审查结论

**状态： 批准（建议同步修复 R4-M1）**

- 无关键安全或并发正确性 blockers
- **R4-M1**（`panicRateLimiter` 延迟初始化竞争）：构造时显式初始化即可修复，消除 `-race` 告警
- **R4-M2**（自定义 JobType Sentinel 规则需配套 job-type resourceFunc）：建议补充构造期 warn 或文档警告
- **R4-L1**（nil 守护冗余）：可后续清理顺手移除

---

## 10. 第五轮审查（2026-05-16） commit `d442412`（actor 目录优化 part-2 / HEAD）

- **commit**：`d442412`（HEAD）
- **父 commit**：`f1c6287`（R4 审查对象）
- **文件变更**：130+ 文件；Go 源码改动集中在 `engine/` 目录，其余为 `.github/` agent 配置、文档、示例
- **构建/vet/测试**：`go build ./engine/...`   `go vet ./engine/...`   `go test ./engine/pkg/actor/... ./engine/pkg/authz/... ./engine/pkg/metrics/... ./engine/pkg/core/... -count=1 -timeout 60s` 

### 10.1 本轮主要改动

| 改动 | 文件 | 说明 |
|---|---|---|
| **R4-M1 修复**：`panicRateLimiter` 构造时初始化 | `worker_pool.go` | 新增 `newPanicRateLimiter(burstCap)`，字段改为 `*panicRateLimiter`，`NewWorkerPool` 赋值，消除懒初始化竞争 |
| `NewMiddlewareChain` 签名改为 `([]IMailboxMiddleware, ...Option)` | `middleware_chain.go` | 参数由 variadic 改为 slice + opts，`WithPanicHandler` 改为构造期 `MiddlewareChainOption`，移除运行时 `SetPanicHandler` |
| `safeLifecycle` 拆分为 `safeOnStart` / `safeOnStop` | `middleware_chain.go` | 消除字符串比较分支，语义更清晰 |
| `ReturnContext` 删除 | `middleware_chain.go` | 已被 `ExecuteOnComplete` 内联归还取代，消除死代码 |
| `discardJob` / `notifyJobDiscarded` 重构 | `mailbox.go` | 拆分为"通知业务"与"Release"两步，PostJob 错误路径统一为 `notifyJobDiscarded  ExecuteOnComplete  Release` 顺序，消除 R2-M1 双释放风险 |
| `mailboxMetricsCollector`（新文件） | `mailbox_metrics.go` | 嵌入 Mailbox，四个原子计数器：postTotal / suspendedTotal / rejectedTotal / dispatchFailedTotal |
| `MailboxMetrics` 快照类型 | `def/mailbox_metrics.go` | 纯值类型，供上层聚合使用 |
| `PriorityQueueManager` 接入 `log.ILoggerX` | `queue_manager_priority.go` | `slog.Warn` 替换为框架 logger，fallback 警告纳入统一日志系统 |
| `worker.go` readPipeline 超时等待改用轮询 | `worker.go` | `inflightReads.Wait()` 前改为 `inflightReadCnt` + `time.Sleep(1ms)` 轮询等待，配合 `stopDeadlineNS` 提前退出 |
| `execWithRW` RW Disabling 路径修正 | `worker.go` | `IsDisabling()` 时强制走写路径（串行），避免 disable 窗口内新 Job 仍被 dispatchRead |
| `dispatchRead` 计数器顺序修正 | `worker.go` | `readsDispatched.Add(1)` 移到非阻塞 send 前；回压路径改为先 `readsLaunched.Add(1)`（本条 pending 已算入）再丢弃 |
| `launchRead` 超时后走 `execWrite` | `worker.go` | 超时等 readSem 后改为 `execWrite` 而非 `safeExec`，保持 panic 保护一致 |
| `sentinelEntryKey` 常量化 | `sentinel_middleware.go` | `"sentinel_entry"`  `const sentinelEntryKey`，避免字符串字面量散落两处 |
| `CompositeSuspendPolicy.AddPolicy` CAS 加 `runtime.Gosched()` | `suspend_policy.go` | 消除 CAS 自旋饥饿 |
| RBAC `Authorizer` 注入 `Handler` | `core/rpc/handler.go` + `core/service.go` | `SetAuthorizer(*authz.Authorizer)` 注入，`HandleRequest` 前置 RBAC 鉴权检查（nil = 不鉴权） |
| `def/error.go` 注释规范化 + 修复错误字符串 | `def/error.go` | `ErrEventChannelIsFull` 清除调试占位符 `"111111111..."`，补充错误码分段规范注释 |
| 大量新增测试 | `mailbox_lifecycle_test.go` / `postjob_ownership_test.go` / `job_release_test.go` / `mailbox_metrics_test.go` / 各包 `*_test.go` | 覆盖 PostJob 所有权、生命周期、指标、RPC 鉴权、RW 切换等关键路径 |
| 新增 `authz` 包 | `engine/pkg/authz/authz.go` | RBAC 引擎，Role-Permission 矩阵，`Authorize(caller, service, method)` |
| 新增 `metrics` 包 | `engine/pkg/metrics/` | mailbox / event / rpc / pool / node 各维度指标，`snapshot` + `sample` 架构 |
| 新增 `sysService/healthservice` | `engine/pkg/sysService/healthservice/` | HTTP health endpoint，依赖 `INodeContext.IsReady()` |
| 新增 `utils/tlsx` | `engine/pkg/utils/tlsx/tls.go` | TLS 配置构建工具，覆盖 mTLS / 单向 TLS |

### 10.2 发现的问题

#### R5-M1 `readPipelineWg` 超时分支仍等待 `pipelineDone`，兜底不硬

- **位置**：`engine/pkg/actor/mailbox/worker.go`  `run()` stop 分支
- **类型**：中  停机可靠性
- **原因**：复核后修正原描述：1ms 轮询发生在 `inflightReadCnt` 等待阶段，而 `readPipelineWg.Wait()` 仍使用 `pipelineDone + time.After`。真正问题是超时分支记录错误后仍执行 `<-pipelineDone`，如果 readPipeline 卡在自定义中间件/回调或异常路径，Stop 仍可能继续挂住，不能称为硬超时兜底。

```go
select {
case <-pipelineDone:
case <-time.After(w.env.rw.stopTimeout):
    stopTimedOut = true
    w.env.logger.Errorf("...")
    <-pipelineDone // 超时后仍阻塞等待
}
```

- **建议**：超时后不要再无条件等待 `pipelineDone`；应进入 unsafe drain 并允许 `run()` 退出，或在注释中明确该 timeout 只是“告警与推进 deadline”，不是硬兜底。

#### R5-M2 `Handler.SetAuthorizer` 指针替换不适合作为运行期热更新机制

- **位置**：`engine/pkg/core/service.go` `SetAuthorizer` + `engine/pkg/core/rpc/handler.go` `HandleRequest`
- **类型**：中  功能完整性
- **复核修正**：原先认为 `Authorizer` 策略无法热更新不准确；`Authorizer` 自身有 `AddRole` / `RemoveRole` / `BindRole` / `UnbindRole`，并用 `RWMutex` 保护，已注入 `Handler` 的同一个 `Authorizer` 指针可以原地更新策略。
- **真正限制**：运行期替换整个 `*authz.Authorizer` 指针没有同步保护，`HandleRequest` 读取 `h.authorizer` 与外部并发 `SetAuthorizer` 会形成数据竞争；当前 `SetAuthorizer` 只适合 `Init` 前注入。
- **建议**：文档明确“运行期更新策略应修改同一个 `Authorizer` 实例，不应替换指针”；若确需替换指针，`Handler.authorizer` 应改为 `atomic.Pointer[authz.Authorizer]` 或接口快照。

#### R5-M3 `Authorizer.AddRole` 未拷贝 `permissions` 切片

- **位置**：`engine/pkg/authz/authz.go`  `AddRole`
- **类型**：中  数据竞争 / 权限绕过
- **原因**：`AddRole(name, permissions)` 直接保存调用方传入的 slice。调用方如果在 `AddRole` 返回后继续修改该 slice，会绕过 `Authorizer` 的锁；与并发 `Authorize` 读权限列表时可能产生 data race，也可能导致非预期授权变化。
- **建议**：在 `AddRole` 内复制切片：`perms := append([]string(nil), permissions...)`，再保存到 `Role.Permissions`。

#### R5-M4 `HealthService.OnStart` 异步 `ListenAndServe`，监听失败无法返回生命周期

- **位置**：`engine/pkg/sysService/healthservice/healthservice.go`  `OnStart`
- **类型**：中  可用性 / 运维可观测性
- **原因**：`OnStart` 中直接 goroutine 调用 `hs.server.ListenAndServe()`，端口占用、权限不足等监听失败只会异步打日志，`OnStart` 仍返回 nil，Node/Service 生命周期会误认为健康服务已成功启动。
- **建议**：使用 `net.Listen("tcp", addr)` 在 `OnStart` 同步绑定；绑定失败直接返回错误，成功后再 goroutine `hs.server.Serve(listener)`。

#### R5-M5 `LoadClientTLS` cert/key 半配置时静默降级

- **位置**：`engine/pkg/utils/tlsx/tls.go`  `LoadClientTLS`
- **类型**：中  配置安全 / 易排障性
- **原因**：当前只有 `certFile != "" && keyFile != ""` 时才加载客户端证书；若只配置 cert 或只配置 key，会静默跳过客户端证书加载，导致 mTLS 退化为单向 TLS 或在服务端握手失败，错误定位困难。
- **建议**：校验 `certFile == ""` 与 `keyFile == ""` 必须同时成立或同时不成立；半配置时返回明确错误。

#### R5-L1 `def/error.go` 错误码分段注释与现有 `errors.New` 风格不一致

- **位置**：`engine/pkg/def/error.go`
- **类型**：低  文档/规范
- **原因**：新增注释推荐"后续新增 sentinel 使用 `errorx.New(code, msg)`"，但同文件内所有现有错误仍是 `errors.New`，与规范注释不一致，容易造成新贡献者混淆。
- **建议**：在注释中明确区分"存量错误暂不迁移"与"新增错误规范"，或提供迁移 TODO 追踪条目。

#### R5-L2 `mailboxMetricsCollector` 未暴露给 `WorkerPool` 内部路径

- **位置**：`engine/pkg/actor/mailbox/mailbox.go` / `mailbox_metrics.go`
- **类型**：低  功能完整性
- **原因**：`dispatchFailedTotal` 在 `Mailbox.PostJob` 的 `DispatchJob` 失败路径计数，但 Worker 内部的 `discardExec`（drain 阶段丢弃）、`drainDiscardTotal` 等路径不在 `mailboxMetricsCollector` 统计范围内，上层无法从单一 `GetMailboxMetrics()` 获得完整丢弃全貌。
- **建议**：后续可将 Worker 级别的 `drainDiscardTotal` 汇总到 `mailboxMetricsCollector`，或通过 `WorkerPool.GetStats()` 与 `GetMailboxMetrics()` 联合查询并在文档中注明。

### 10.3 已修复问题汇总

| 历史问题 | 状态 |
|---|---|
| R4-M1 `panicRateLimiter` 懒初始化数据竞争 |  已修复（`newPanicRateLimiter` 构造时初始化） |
| R2-M1 `discardJob` 双释放风险 |  已修复（`notifyJobDiscarded` 拆分，`ExecuteOnComplete  Release` 顺序固定） |
| `safeLifecycle` 字符串比较分支 |  已修复（拆分为 `safeOnStart` / `safeOnStop`） |
| `ErrEventChannelIsFull` 调试占位符 |  已修复（恢复正确字符串） |
| `CompositeSuspendPolicy.AddPolicy` CAS 无退避 |  已修复（加 `runtime.Gosched()`） |

### 10.4 仍未修复的历史问题

| 历史问题 | 说明 |
|---|---|
| 2.2 `RWController.Disable` busy-wait | 未在本轮改动 |
| R2-M2 `MultiLevelQueueConf` 原地默认化可能污染共享配置模板 | ✅ R11 已修复：`NewPriorityQueueManager`/`NewPriorityScheduler` 内部深拷贝配置 |
| R4-M2 自定义 JobType Sentinel 规则需配套 job-type resourceFunc | ✅ R11 已修复：`WithJobType*Rule` 自动启用 job-type resourceFunc |
| R3-M2 `panicRateLimiter.Allow()` CAS 自旋无退避 | ✅ R8 已修复：CAS 失败加 `runtime.Gosched()` 退避 |

### 10.5 亮点

| 改动 | 评价 |
|---|---|
| `discardJob` 重构 + `notifyJobDiscarded` | PostJob 所有权语义彻底清晰，双释放风险从根本上消除 |
| `mailboxMetricsCollector` | 四路原子计数器零竞争，PostJob 热路径无额外分配 |
| RBAC `Authorizer` 注入 | nil-safe 设计，无 RBAC 时零开销，接口侵入最小 |
| `NewMiddlewareChain` 签名改为 slice + opts | 参数语义更明确，Option 模式方便后续扩展 |
| `sentinelEntryKey` 常量化 | 消除散落字符串字面量，编译期检查 key 一致性 |
| 大量补充测试 | `postjob_ownership_test.go`（291 行）、`mailbox_lifecycle_test.go`（353 行）等全面覆盖关键契约 |
| `authz` 包独立 | RBAC 逻辑与框架解耦，可独立测试，`authz_test.go` 197 行覆盖率高 |
| `PriorityQueueManager` 接入框架 logger | 消除 `log/slog` 依赖，日志统一管理 |

### 10.6 第五轮审查结论

**状态： 批准**

- 无关键安全或并发正确性问题
- **R5-M1**（readPipelineWg 超时后仍等待 pipelineDone）：建议修复为真正硬兜底，否则 Stop 在异常路径仍可能挂住
- **R5-M2**（Authorizer 指针替换限制）：运行期应更新同一实例策略，不应无锁替换指针
- **R5-M3**（AddRole 未拷贝 permissions）：建议优先修复，避免调用方修改 slice 绕过锁
- **R5-M4/M5**（health/tlsx）：影响启动可观测性与 TLS 配置排障，建议纳入近期修复
- **R5-L1/L2**：代码质量/规范问题，后续清理

---

## 11. R6 复核修正（2026-05-16）— 四提交审查纠偏

- **复核范围**：`56879a4c`、`0e3e8428`、`f1c6287b`、`d4424121`（实际 HEAD 前缀为 `d442412`）
- **复核命令**：`git show` / `git diff` 对比单提交 Go 变更；`go vet ./...` 无输出；`staticcheck ./...` 有全仓既有输出，相关条目见 §11.4
- **工作区提示**：复核时工作区已有后续未提交改动（`authz` policy 分发、配置文档等），本文结论仅针对上述四个提交本身

### 11.1 初审 §2 中的过时/误判条目

| 原条目 | 复核结论 |
|---|---|
| §2.2 `RWController.Disable` 纯 `runtime.Gosched()` 忙等 | `56879a4c` 中已使用 `idle.NewSpinBackoff(5 * time.Millisecond)`，不是纯忙等；仍可优化但不应按“满核自旋 10s”定性 |
| §2.3 `PriorityQueueManager fallback` 队列可能不存在 | `NewPriorityQueueManager` 在空配置时会注入 `PriorityNormal` 默认队列，fallback 队列不存在的描述基本不成立 |
| §2.4 watchdog 每个 Job 都创建 `time.AfterFunc` | `56879a4c` 已优先使用 `watchdogScheduler` 时间轮；`time.AfterFunc` 是降级路径 |
| §2.5 `Worker.BeginStop` 缺总体超时 | `56879a4c` 已有 submitters 等待 deadline，属于误判 |
| §2.6 rwUnsafe drain 仍执行中间件 `OnComplete` | `56879a4c` 中 `rwUnsafe` 分支跳过 `ExecuteOnComplete`，只归还 `mctx`，该条不成立 |

### 11.2 §8 临时合并审查的纠偏

| 原条目 | 复核结论 |
|---|---|
| R3-M1 Sentinel 全局 map 无清理 | 对 `f1c6287` 之后代码不成立；`OnStop` 已按 owner 删除并 reload flow / breaker / system rules |
| R3-H1 `dispatchRead` 双释放风险 | 更准确地说是“业务违反 `OnJobDiscarded` 禁止 Release 契约时的 debug 断言建议”，框架自身释放顺序没有发现双释放 |
| R3-M3 缩容全程持 `p.mu` 阻塞 dispatch | 标题不准确；缩容 `BeginStop`/`Wait` 在 `p.mu` 外，COW snapshot 下 dispatch 不持 `p.mu`。实际可优化点是扩容期间 `worker.Start()` 仍在锁内，会阻塞其他 resize / `SetRWEnabled`，但不阻塞普通 dispatch |

### 11.3 本次复核新增/修正的问题清单

| 编号 | 严重度 | 问题 | 建议 |
|---|---|---|---|
| R5-M1 | 中 | `readPipelineWg` 超时后仍 `<-pipelineDone`，Stop 异常路径仍可能挂住 | ✅ R9 已修复：超时后不再阻塞等待 pipelineDone，直接继续后续清理 |
| R5-M2 | 中 | `Handler.SetAuthorizer` 只适合 Init 前注入，运行期替换指针无同步保护 | ✅ R10 已修复：改用 `atomic.Pointer[authz.Authorizer]`，运行期可安全替换 |
| R5-M3 | 中 | `Authorizer.AddRole` 未拷贝 `permissions` slice | ✅ R8 已修复：`AddRole` 内 `copy` 后存入 |
| R5-M4 | 中 | `HealthService.OnStart` 异步监听，端口失败无法返回生命周期 | ✅ R8 已修复：`net.Listen` 同步绑定，`server.Serve` 异步 |
| R5-M5 | 中 | `LoadClientTLS` cert/key 半配置静默降级 | ✅ R8 已修复：半配置直接返回配置错误 |
| R3-M2 | 中 | `panicRateLimiter.Allow()` CAS 扣 token 仍无退避 | ✅ R8 已修复：CAS 失败加 `runtime.Gosched()` |

### 11.4 staticcheck 复核补充

`staticcheck ./...` 当前会输出较多全仓既有问题（unused、style、nil context、deprecated errorlib 等），不全部归入本轮四提交结论。与本次提交范围相邻、值得跟踪的低优先级项如下：

| 编号 | 严重度 | staticcheck | 位置 | 说明 |
| --- | --- | --- | --- | --- |
| R6-L1 | 低 | S1039 | `engine/pkg/actor/mailbox/scaler.go` | `fmt.Sprintf` 用法可简化，纯风格问题 |
| R6-L2 | 低 | U1000 | `engine/pkg/actor/mailbox/scheduler.go` | `newDefaultMultiLevelConfig` 未使用；若确为遗留 helper，建议删除或补测试覆盖 |
| R6-L3 | 低 | S1011 | `engine/pkg/actor/mailbox/sentinel_middleware.go` | `addFlowRulesForResource` / `addCircuitBreakerRulesForResource` 可用 `append(dst, src...)` 简化循环 |
| R6-L4 | 低 | SA5011 | `engine/pkg/rpc/client/pool/metrics_test.go` | 测试中 `m == nil` 后未 `return`/`Fatal`，后续访问 `m.TotalConnections` 被 staticcheck 判定为潜在 nil deref；生产代码无影响 |

### 11.5 复核后最终结论

**状态：✅ 可合并（关键问题均已修复）**

- 未发现新的关键安全问题或必须阻塞合并的问题。
- 文档中部分早期审查条目为误判或已被后续单提交审查取代，已在本节明确纠偏。
- ✅ R8 已修复：R5-M3（`AddRole` 切片拷贝）、R5-M4（`HealthService.OnStart` 同步监听）、R5-M5（`LoadClientTLS` 半配置报错）、R3-M2（CAS 退避）。
- ✅ R9 已修复：R5-M1（`readPipelineWg` 硬超时）。
- ✅ R10 已修复：R5-M2（`SetAuthorizer` 改 `atomic.Pointer`）；R6-L1~L4（staticcheck 清理）。

## 12. R7 安全漏洞复核（2026-05-16）

- **复核目标**：在 R6 基础上补充 OWASP / secrets / authn-authz / TLS / 运维端点暴露面检查。
- **复核命令**：`go vet ./...` 无输出；`staticcheck ./...` 仍为 §11.4 所述既有输出；本地 `gosec`、`govulncheck` 未安装，无法完成对应自动化扫描。
- **敏感信息扫描**：未发现生产 Go 代码中硬编码云密钥或私钥；发现模板/示例配置含默认密码，详见 R7-M4。

### 12.1 安全发现清单

| 编号 | 严重度 | 类型 | 位置 | 问题 | 建议 |
| --- | --- | --- | --- | --- | --- |
| R7-H1 | 高 | WebSocket 跨站劫持 / 未鉴权入口 | `engine/pkg/utils/network/ws_server.go` | 通用 `WSServer` 的 `CheckOrigin` 固定返回 `true`，且连接建立处仍有 `TODO 验签`；若业务直接暴露该组件，任意 Origin 可发起连接，且缺少内建认证钩子 | ✅ R9 已修复：增加 `AllowedOrigins` 配置，空列表默认同源策略，支持 `"*"` 允许所有来源 |
| R7-M1 | 中 | Gate WebSocket Origin 放开 | `engine/pkg/sysModule/gate/protocol_adapter/ws/websocket.go` | Gate WS 同样 `CheckOrigin: true`。当前依赖 Bearer JWT 中间件有一定缓解，但服务端不校验 Origin，后续若改用 Cookie 或浏览器可携带凭证，容易退化为 CSWSH | ✅ R9 已修复：配置增加 `AllowedOrigins` 字段，空列表默认同源策略 |
| R7-M2 | 中 | 运维端点信息暴露 | `engine/pkg/sysService/healthservice/config/config.go`、`engine/pkg/sysService/healthservice/healthservice.go` | `/metrics` 默认监听 `0.0.0.0:9090` 且无认证/TLS/IP allowlist，可能泄露服务名、节点状态、吞吐、错误率等运行时信息 | ✅ R8 已修复：默认改为 `127.0.0.1:9090`；仍建议后续支持 token / mTLS / IP allowlist |
| R7-M3 | 中 | JWT 校验不够严格 | `engine/pkg/utils/jwtx/jwt.go` | `ParseJwtToken` 未显式校验签名算法、issuer、audience，也未限制最短 secret 强度；当前 HS256 生成路径正常，但解析侧缺少防御式约束 | ✅ R8 已修复：keyfunc 中校验 `token.Method == jwt.SigningMethodHS256`；issuer/audience 校验可后续补充 |
| R7-M4 | 中 | 默认密码传播风险 | `template/config/db.yaml`、`template/config/node.yaml`、`template/docker/*.yaml`、`example/configs/**` | 模板和示例中存在 `123456`、root 等默认账号密码。虽属示例，但容易被复制进测试/生产环境 | ✅ R9 已修复：模板改为 `${MYSQL_PASSWORD}` / `${ETCD_PASSWORD}` 占位符；docker compose 使用环境变量；示例配置添加警告注释 |
| R7-M5 | 中 | 日志泄露敏感查询参数 | `engine/pkg/utils/httpx/gin.go`、`engine/pkg/utils/httplib/http.go` | Gin 中间件记录完整 `RawQuery`，`httplib.Request` 打印完整 URL 和响应对象；若 URL 中含 `token`、`secret`、`password`，会落盘或输出到控制台 | ✅ R8+R10 已修复：`httplib` 移除 `fmt.Println`；Gin `RawQuery` 增加 `sanitizeQuery` 脱敏 token/secret/password 等参数 |
| R7-M6 | 中 | TLS 降级/误配置风险 | `engine/pkg/utils/tlsx/tls.go`、`engine/pkg/event/bus_nats.go`、`engine/pkg/rpc/client/sender_remote_nats.go` | `LoadClientTLS` 与 NATS 连接允许配置 `insecureSkipVerify=true`，且 cert/key 半配置静默降级，R5-M5 已记录半配置问题；生产配置缺少强制 guard | ✅ R8+R10 已修复：半配置直接报错；`insecureSkipVerify=true` 与 `caFile` 同时配置时拒绝矛盾配置 |

### 12.2 未发现或暂未确认的问题

- **命令注入**：未发现 `exec.Command` / `os/exec` 拼接用户输入的生产路径。
- **SQL 注入**：抽样检查到的 GORM SQL 使用参数化占位符，未发现明显字符串拼接 SQL。
- **XXE**：未发现 XML 解析入口。
- **不安全反序列化**：主要为 JSON / Protobuf 反序列化，未发现 Go `gob` 或任意类型反序列化；仍建议对外部消息保持最大长度限制。
- **硬编码生产密钥**：未发现私钥、云访问密钥或生产 token；示例/模板默认密码按 R7-M4 跟踪。

### 12.3 安全结论

#### 状态：✅ 安全问题均已修复（未发现 CRITICAL；R8 修复 R7-M2/M3，R9 修复 R7-H1/M1/M4，R10 完善 R7-M5/M6）

- ✅ R8 已修复：R7-M2（默认地址改 `127.0.0.1`）、R7-M3（JWT 算法校验）。
- ✅ R9 已修复：R7-H1（WSServer `AllowedOrigins` 配置，默认同源策略）、R7-M1（Gate WS `AllowedOrigins`）、R7-M4（模板/示例默认密码改占位符）。
- ✅ R10 已完善：R7-M5（Gin `RawQuery` 敏感参数脱敏）、R7-M6（`insecureSkipVerify` + CA 矛盾配置拒绝）。
- 当前安全发现均已得到修复或缓解，无阻塞合并项。
