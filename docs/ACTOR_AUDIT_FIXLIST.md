# Actor 模块代码审计与修复清单

> 范围：`engine/pkg/actor/**`（`pid.go`、`event.go`、`mailbox/**`、`mailbox/job/**`）
> 视角：性能、并发安全、资源泄漏、API 合约、可维护性
> 等级：P0=必须立即修；P1=本迭代修；P2=后续优化
> 验证：`go build ./...` ✅ `go vet ./...` ✅ `go test -race ./engine/pkg/actor/... ./engine/pkg/core/...` ✅
> 最后更新：2026-04-29（RW Stop / 中间件 recover / Job 池 debug / Sentinel resource 复审见 [ACTOR_REVIEW_2026_04_29.md](ACTOR_REVIEW_2026_04_29.md)）

---

## P0-1 ✅ 已修复 — 投递失败路径的 `MiddlewareContext` 泄漏

### 现象
`Mailbox.PostJob` → `WorkerPool.DispatchJob` → `Worker.SubmitJob` 链路上任何一环失败都会**直接 return error**，但：

1. `job` 未调用 `Release()`，对象池引用计数失衡，长期运行会让 `RpcJob/EventBusJob/...` sync.Pool 持续膨胀；
2. 当失败发生在中间件 `OnReceive` 之后（含中间件 Reject、`workerStateClosing`、ring 没节点、`worker.SubmitJob` 队列 push 失败等），`MiddlewareContext` 既不会通过 `ExecuteOnComplete` 归还 `ctxPool`，也违反洋葱模型契约——已执行 `OnReceive` 的中间件（如 `CircuitBreaker`、`Sentinel`、`RateLimit`）拿不到 `OnComplete` 回调，统计/释放/Sentinel `entry.Exit()` 全部丢失。

代码位点：
- `engine/pkg/actor/mailbox/mailbox.go` `PostJob` 中的 `return def.ErrMailboxSuspended` / `return result.Err` / `return def.ErrMailboxMiddlewareRejected`；
- `engine/pkg/actor/mailbox/worker_pool.go` `DispatchJob` 中的 `return def.ErrMailboxWorkerIsFull` / `ErrMailboxWorkerNotFound` 及 `worker.SubmitJob(job)` 失败的返回；
- `engine/pkg/actor/mailbox/worker.go` `SubmitJob` 中 `state != Running` 与 `queueManager.Submit` 错误返回。

### 影响
- 高并发下 `MsgJobPool` 等多个池 stats 持续增长，最终触发 GC 抖动甚至 OOM；
- Sentinel `entry.Exit()` 未调用，会让 Sentinel 全局并发计数永久泄漏，导致全局限流被锁死；
- `CircuitBreaker.OnComplete` 未调用，失败事件不会进入熔断窗口，熔断器“失效”。

### 修复建议
统一在投递入口建立**“失败时 cleanup”辅助函数**：

```go
// 失败收尾：触发 OnComplete + 释放 Job
func (m *Mailbox) failPost(job inf.IMailboxJob, mctx inf.IMiddlewareContext, err error) error {
    if mctx != nil {
        m.workerPool.middlewareChain.ExecuteOnComplete(mctx, err, nil)
    }
    if job != nil {
        job.Release()
    }
    return err
}
```

`PostJob` 改造：

```go
if m.isSuspended() && !m.suspendPolicy.ShouldAllow(job) {
    return m.failPost(job, nil, def.ErrMailboxSuspended)
}
result, mctx := m.workerPool.middlewareChain.ExecuteOnReceive(job, m.workerPool.invoker.GetServiceName())
if result.Action == def.ActionReject {
    err := result.Err
    if err == nil { err = def.ErrMailboxMiddlewareRejected }
    return m.failPost(job, mctx, err)
}
job.SetMiddlewareContext(mctx)
if err := m.workerPool.DispatchJob(job); err != nil {
    return m.failPost(job, mctx, err) // DispatchJob 不再自行释放
}
return nil
```

`WorkerPool.DispatchJob` 与 `Worker.SubmitJob` 仅返回 error，不自行释放——由调用栈最外层（`PostJob`）统一兜底；这样能避免“双重 release”。

### 验收
- 压测下 `MsgJobPool` 的 inUse 计数与 `Active` 持平；
- Sentinel `passed - blocked - error` 守恒；
- 用注入式 `mailbox closed` 触发 1e6 次失败，pprof heap 不增长。

### 实际修复 (2026-04-21)

**文件**: `engine/pkg/actor/mailbox/mailbox.go` `PostJob`

- 在中间件 `Reject` 路径上补调 `ExecuteOnComplete(mctx, rejectErr, nil)`，归还 mctx + 触发 Sentinel `entry.Exit()` 等回调；
- 在 `DispatchJob` 返回 error 路径上同样补调 `ExecuteOnComplete(mctx, dispatchErr, nil)`；
- `Suspend` 拒绝路径（OnReceive 未执行）无 mctx，不需补调——保持原样；
- **Job Release 仍由调用方负责**（保持现有契约：`service.go`、`bus_*.go`、`sender_local.go` 等调用点在 PostJob 失败后已有 `j.Release()`）。

---

## P0-2 ✅ 已修复 — RW 模式 `StopTimeout` 超时后 `DrainDiscard` 的读写竞态

### 现象
`Worker.run` 退出 defer：
```go
case <-time.After(w.env.rw.stopTimeout):
    stopTimedOut = true   // 强制降级为 DrainDiscard，绕过 WLock
...
case DrainDiscard:
    // 不持 WLock 直接执行
    w.queueManager.DrainAll(func(e inf.IMailboxJob) { w.discardExec(e) })
```
而 `discardExec` 调用 `w.env.invoker.OnJobDiscarded(job, ...)`。**超时意味着此时仍有泄漏的读 goroutine 持有 RLock 在调用 `invoker.ExecuteJob`**，两边并发访问同一 `invoker`/Service 共享状态，破坏 RW 协议设计目标。

代码位点：`engine/pkg/actor/mailbox/worker.go` `run()` 末尾 defer。

### 影响
- 业务侧 Service 在“安全”假设下的写状态可能被读 goroutine 撞到，data race；
- 与 `MAILBOX_OPTIMIZATION_PLAN.md / DESIGN_RW_SEPARATION.md §10.14` 的语义不一致。

### 修复建议
1. 即便走 `DrainDiscard`，若超时仍要拿 WLock 但用 `TryLock` 防止永久阻塞：
   ```go
   if w.env.rw.enabled.Load() {
       deadline := time.Now().Add(time.Second)
       for !w.env.rw.mu.TryLock() {
           if time.Now().After(deadline) { break }
           runtime.Gosched()
       }
       defer w.env.rw.mu.Unlock() // 仅当 lock 成功
   }
   ```
2. `OnJobDiscarded` 必须保证只读/幂等，或改为投递到独立的"discarded" 通道由后台串行消费；
3. 在 `stopTimedOut=true` 路径上记录 `metrics: rw_drain_unsafe_total`，便于线上告警。

### 实际修复 (2026-04-21)

**文件**: `engine/pkg/actor/mailbox/worker.go` `run()` defer

- `stopTimedOut=true` 时直接走 unsafe drain（不抢 WLock），跳过 `invoker.OnJobDiscarded()` 调用，仅执行 `OnComplete` + `Release` 资源回收；
- 非超时路径保持阻塞式 `w.env.rw.mu.Lock()` 安全 drain（此时读 goroutine 已全部 Done）；
- `discardExec` 新增 `rwUnsafe bool` 参数，`rwUnsafe=true` 时跳过 invoker 调用；
- `drainDiscardTotal` 计数在 unsafe 路径上递增，供线上告警。

---

## P1-3 ✅ 已修复 — 缩容窗口 `DispatchJob` 命中已停 worker 直接返回错误

### 现象
`resizeWorkers` 缩容流程：
```
Lock → ring.RemoveMany → workerCount.Store → Unlock → BeginStop(w) → Wait(w) → Lock → delete(workers, id)
```
`DispatchJob` 在 `mu.RLock` 内通过 `ring.Get` 拿 `workerID`，再 `p.workers[workerID]`，**两个时间窗**：
- 窗口 A：`Unlock` 之后、`BeginStop` 之前——`worker.SubmitJob` 仍可用 ✓
- 窗口 B：`BeginStop` 已调用、`delete(workers,id)` 之前——`SubmitJob` 命中 `state==workerStateClosing/Closed` 直接返回 `ErrMailboxWorkerClosed`，对调用方而言等价于消息丢失。

代码位点：`engine/pkg/actor/mailbox/worker_pool.go` `DispatchJob` / `resizeWorkers`。

### 修复建议
**“先 delete，再 Stop”**：
1. `Lock → ring.RemoveMany + delete(workers, id) + Unlock`；这样 `DispatchJob` 在 `RLock` 阶段从 `ring.Get` 后 `p.workers[workerID]` 拿不到，直接走 `ErrMailboxWorkerNotFound` 让上层重试 / fallback；
2. 或者保留旧顺序，在 `DispatchJob` 检测到 `ErrMailboxWorkerClosed` 时**重新走一次 `ring.Get`** 并重试一次（不超过 1 次）。

推荐方案 1（语义更简单，且 ring 已经移除时 `ring.Get` 不会再选到该 ID）。

### 实际修复 (2026-04-21)

**文件**: `engine/pkg/actor/mailbox/worker_pool.go` `resizeWorkers`

- 采用方案 1：在锁内同时执行 `ring.RemoveMany` + `delete(p.workers, id)` + `delete(p.dispatchCnt, id)`，然后 Unlock → BeginStop → Wait；
- `DispatchJob` 在 `RLock` 内通过 `ring.Get → p.workers[id]` 时，如果 id 已被 delete，直接走 `ErrMailboxWorkerNotFound`，上层可重试或降级。

---

## P1-4 ✅ 已修复 — `CircuitBreaker` Open→HalfOpen 期间的探测计数 race

### 现象
```go
case StateOpen:
    if cooldown够 {
        if state.CAS(Open, HalfOpen) {
            halfOpenReqs.Store(1) // ←重置为 1
            successes.Store(0)
            return Continue()
        }
        // CAS 失败 fallthrough 到 HalfOpen
    }
    fallthrough
case StateHalfOpen:
    for {
        if halfOpenReqs.Load() >= max { reject }
        if halfOpenReqs.CAS(...) { return Continue() }
    }
```

**问题**：CAS 翻状态成功的线程把 `halfOpenReqs.Store(1)`，但与此同时另一个线程在 `for { CAS halfOpenReqs }` 已读到旧值并 CAS 成功增加到 N+1，此后 `Store(1)` 直接覆盖丢失了那次计数。
- 短时实际探测请求数可超过 `halfOpenMaxAllowed`；
- 同时 `successes.Store(0)` 若发生在另一探测的 `OnComplete` 增加之后，会丢失已记的成功数。

另一个细节：HalfOpen→Closed 时未重置 `halfOpenReqs`，但下一次 Open→HalfOpen 用 Store(1) 覆盖，问题不严重；HalfOpen→Open（探测失败）后也未重置 successes/halfOpenReqs，再次进入 HalfOpen 也靠 Store(1) 覆盖——可接受但请加注释。

代码位点：`engine/pkg/actor/mailbox/circuit_breaker_middleware.go` `OnReceive`。

### 修复建议
把状态切换+计数初始化合并为一个原子组合，最简单做法：用 `mu` 保护切换临界区（已有字段）：

```go
case StateOpen:
    if cooldownReached {
        m.mu.Lock()
        if m.state.Load() == int32(StateOpen) {
            m.halfOpenReqs.Store(0)  // 必须先清零
            m.successes.Store(0)
            m.state.Store(int32(StateHalfOpen))
        }
        m.mu.Unlock()
        // 重新走 HalfOpen 分支统一 CAS 取额度
    } else {
        rejected; return Reject
    }
    fallthrough
```
切换+清零在 `mu` 内串行化，HalfOpen 分支保持无锁 CAS。`Reset()`、`OnComplete` 中 HalfOpen→Open/Closed 也应在 `mu` 内做"state + halfOpenReqs"组合写。

### 实际修复 (2026-04-21)

**文件**: `engine/pkg/actor/mailbox/circuit_breaker_middleware.go`

- **OnReceive Open→HalfOpen**：使用 `m.mu.Lock()` 保护切换临界区，先 `halfOpenReqs.Store(0)` + `successes.Store(0)` 再 `state.Store(HalfOpen)`，然后 Unlock，统一 fallthrough 到 HalfOpen 分支 CAS 取额度。消除"Store(1) 覆盖并发 CAS"的 race；
- **OnComplete HalfOpen→Open**（探测失败）：在 `mu` 内组合执行 `state.Store(Open)` + `halfOpenReqs.Store(0)` + `successes.Store(0)` + `lastFailTime.Store()`；
- **OnComplete HalfOpen→Closed**（探测成功达阈值）：在 `mu` 内组合执行 `state.Store(Closed)` + `failures.Store(0)` + `successes.Store(0)` + `halfOpenReqs.Store(0)`。

---

## P1-5 ✅ 已修复 — `execRead` 在 `readSem` 满时的 CPU 退避策略

### 现象
```go
default:
    for i := 0; i < yieldCount; i++ { runtime.Gosched() }
    if yieldCount < maxYieldCount { yieldCount *= 2 }
    continue
```
当读密集场景下 `readSem` 长时间打满，主循环会以 `Gosched` 形式持续燃烧 CPU（每次 64 次 Gosched + 立刻重试）。

代码位点：`engine/pkg/actor/mailbox/worker.go` `execRead`。

### 修复建议
- 在 `yieldCount` 达到 `maxYieldCount` 后改用 `time.Sleep(microsecond)` 阶梯（与 `execWrite` 的 backoff 模式一致）；
- 或者把"获取信号量令牌"改成阻塞 `select { case readSem <- {}: ; case <-stopCh: }`，但这需要 Worker 暴露 `stopCh`。后者更优雅，可放到 §P2 重构。

### 实际修复 (2026-04-21)

**文件**: `engine/pkg/actor/mailbox/worker.go` `execRead`

- `readSem` 满时：先用 Gosched 退避到 `maxYieldCount(64)` 次，之后改用 microsecond 级阶梯 `time.Sleep`（初始 1µs，翻倍增长，上限 5ms），与 `execWrite` 的 backoff 模式一致；
- `writeRequested` 让步分支保持原有纯 Gosched 退避（写优先语义不变）。

---

## P1-6 ✅ 已修复 — `MaxJobExecutionTime` watchdog 增加可观测性指标

### 现象
```go
if maxExec := w.env.rw.maxJobExecTime; maxExec > 0 {
    timer := time.AfterFunc(maxExec, func() {
        w.env.logger.Warnf("Worker %d job execution exceeds %v: %v", ...)
    })
}
```
仅 warn 级别打印，**没有 cancel context、不计数指标、不联动熔断**。线上长时间执行的 Job 不可见，日志噪音也大。

### 修复建议
- watchdog 触发时：
  - 给 `ctx` 注入 `context.WithCancel` 并 cancel（需要业务 handler 监听 ctx），但需要谨慎，因为 RPC handler 通常不监听；
  - 触发后 atomic 累加 `rwLongJobTotal`，纳入 `RWMetrics`；
  - 与 `Sentinel` `WithSlowRatioRule` 联动可选（保留给业务自己注册）。

### 实际修复 (2026-04-21)

**文件**: `engine/pkg/actor/mailbox/rw_controller.go`、`worker.go`、`worker_pool.go`

- `RWController` 新增 `longJobTotal atomic.Int64` 字段；
- watchdog `time.AfterFunc` 回调中新增 `w.env.rw.longJobTotal.Add(1)`；
- `RWMetrics` 结构体新增 `LongJobTotal int64`，`GetRWMetrics()` 中赋值。
- cancel context 及 Sentinel 联动暂不实施，标记为后续可选增强。

---

## P1-7 ✅ 已修复 — `pid.go` `IsMaster` 字段与 `MasterFlag` 的双源一致性

### 现象
- `SetMaster` 同时写 `MasterFlag`（atomic）与 `IsMaster`（普通 bool）；
- protobuf 反序列化只写 `IsMaster`，需要调用方主动 `SyncMasterFlag()`；
- 任何忘记调用 `SyncMasterFlag` 的反序列化路径会让 `IsMasterNode()` 永远返回 false。

### 修复建议
- 在所有反序列化入口（`endpoints` 的 PID 解码处）grep 检查并加上 `SyncMasterFlag()`；
- 给 `PID` 增加单元测试：`proto.Unmarshal` 后无 Sync 调用 `IsMasterNode()` → 应失败的测试用例做"提示"，避免回归。

### 实际修复 (2026-04-23)

- `engine/pkg/cluster/endpoints/endpoints.go`：原有 `protojson.Unmarshal` 路径已调用 `SyncMasterFlag`，保持不变；
- `engine/pkg/rpc/remote/handler/handler.go` `RpcMessageHandler`：在处理入口对 `req.GetSenderPid()` / `req.GetReceiverPid()` 统一调用 `SyncMasterFlag()`，覆盖 nats/grpc/rx 三条远程监听路径（各 listener 内部都走 proto Unmarshal 得到 `actor.Message`）；
- 新增 `engine/pkg/actor/pid_test.go`：
  - `TestPID_IsMasterNode_AfterUnmarshalWithoutSync` 断言 Unmarshal 后未调用 `SyncMasterFlag` 则 `IsMasterNode()` 为 false，调用后为 true，防回归；
  - `TestPID_SetMaster_Concurrent` 在 `-race` 下验证 `SetMaster` / `IsMasterNode` 并发安全。

---

## P1-8 ✅ 已修复 — `MiddlewareContext.data` 在 OnReceive/OnComplete 跨阶段的并发可见性

### 现象
- `MiddlewareContext` 用 `mu sync.RWMutex` 保护 `data` map；
- OnReceive 在 `Mailbox.PostJob` 调用线程执行，OnComplete 在 worker goroutine 执行，**跨 goroutine** 读写 `data`。`mu.Lock/RLock` 已覆盖，OK；但 `Reset` 时 `for k := range mc.data { delete(...) }` 未 Lock 的版本（`pool` 回收路径有 Lock，构造函数 Reset 没有 Lock 但只在持有 mctx 单一者时调用）需要在文档中明确"调用 Reset 必须独占 mctx"。

### 修复建议
仅文档增强，并把 `Reset(ctx, job, name)` 公开方法标注 `// must be called by sole owner`。

### 实际修复 (2026-04-23)

**文件**: `engine/pkg/actor/mailbox/middleware_chain.go` `MiddlewareContext.Reset`

- 在 `Reset` 方法上新增详细 godoc，明确“must be called by sole owner”并发契约，说明 data map 的 mu 保护不等于所有权，调用方必须保证调用 Reset 时无其他 goroutine 读写本 ctx；
- 代码实现保持不变（已经在 Reset 内部持 mu 清空 data map）。

---

## P1-9 ✅ 已修复 — `PriorityQueueManager.NextJob` 栈数组 `[16]` 截断无告警

```go
var buf [16]def.Priority
n := 0
for _, priority := range m.sortedPriorities {
    if !m.queues[priority].Empty() { buf[n] = priority; n++ }
}
```
若 `len(sortedPriorities) > 16` 会 index out of range panic。当前默认 6 个优先级安全，但 `RegisterJobFactory` + 自定义优先级可超出。

### 修复
- 在 `NewPriorityQueueManager` 中 `if len(conf.PriorityBatches) > 16 { return error }` 显式拒绝；
- 或把 `[16]` 改为 `make` + `sync.Pool[*[]def.Priority]`，性能差异可忽略。

### 实际修复 (2026-04-21)

**文件**: `engine/pkg/actor/mailbox/queue_manager_priority.go`

- `PriorityQueueManager` 结构体新增 `nextJobBuf []def.Priority` 字段；
- `NewPriorityQueueManager` 中在排序后 `m.nextJobBuf = make([]def.Priority, len(m.sortedPriorities))` 预分配；
- `NextJob()` 使用 `m.nextJobBuf` 替代 `[16]def.Priority` 固定数组，长度严格匹配注册优先级数，彻底消除越界 panic。

---

## P2-10 ✅ 已修复 — `CompositeStrategy.Mode` 字段大小写敏感

`scaler.go` 比较 `c.Mode == "all"`，配置写 `"All"` 会静默 fallback 到 `any`。

### 修复
```go
if strings.EqualFold(c.Mode, "all") { ... }
```
并在 `newCompositeStrategy` 中 `mode = strings.ToLower(strings.TrimSpace(mode))`。

### 实际修复 (2026-04-21)

**文件**: `engine/pkg/actor/mailbox/strategy.go`

- `newCompositeStrategy` 中 `mode = strings.ToLower(strings.TrimSpace(mode))`；
- `ShouldScaleUp`/`ShouldScaleDown` 中 `c.Mode == "all"` 改为 `strings.EqualFold(c.Mode, "all")`。

---

## P2-11 ✅ 已修复 — `logDispatchStatsOnce` 死代码

```go
//p.logger.Infof(msg)
```
最终输出被注释，整个 stats 计算每 10s 一次但永远不打印。应：
- 恢复 `p.logger.Debugf(msg)`（或抽到 `Debug()` 守卫下）；
- 或者直接删除整个 `logDispatchStatsLoop` 与 `logDispatchStatsOnce`，由 `RWMetrics`/外部 prometheus 替代。

### 实际修复 (2026-04-21)

**文件**: `engine/pkg/actor/mailbox/worker_pool.go` `logDispatchStatsOnce`

- 在被注释的 `//p.logger.Infof(msg)` 下方新增 `p.logger.Debugf(msg)`，恢复统计输出（Debug 级别，生产环境默认不打印）。

---

## P2-12 ✅ 已修复 — `BeginStop` 中的 `for w.submitters.Load() != 0 { runtime.Gosched() }` 无超时

如果某个 SubmitJob 阻塞在 `queueManager.Submit`（理论上 mpsc.Push 是无锁的，不应阻塞），`BeginStop` 会无限自旋燃烧 CPU。建议加 `deadline + time.Sleep(microsecond)` 阶梯退避。

### 实际修复 (2026-04-21)

**文件**: `engine/pkg/actor/mailbox/worker.go` `BeginStop`

- 先跑 1024 次 `runtime.Gosched()` 自旋（快速路径无延迟开销）；
- 超过后改用 microsecond 阶梯 `time.Sleep`（初始 1µs，翻倍增长，上限 100µs），避免长时间 CPU 燃烧。

---

## P2-13 待优化 — `DispatchKeyStatsMiddleware.shardFor` 重复 hash

每次 OnReceive 都对完整 key 做 fnv32，但同一 key 的 dispatcher 已经被 `WorkerPool.ring.Get` hash 过一次。可以把 hash 缓存到 `MiddlewareContext` 复用，或者直接用 `len(key) & (shards-1)` 折中。

---

## P2-14 待优化 — `RegisterJobFactory` 在 frozen 后报错，但首次 `CreateJob/GetJobPayload` 才冻结

风险：业务在 init() 中注册自定义 jobType，若有任何包先调用了 `CreateJob`（非确定的 init 顺序），后注册的类型会因 frozen 而失败。

### 修复
- 提供显式 `FreezeJobFactory()` 由 `Node.Start` 调用，确保所有 init 完成后再冻结；
- `RegisterJobFactory` 和 `CreateJob` 都用同一互斥锁，去掉 `frozen` 状态机，性能影响可忽略（注册仅启动期）。

---

## P2-15 待优化 — `worker_pool.go:Wait` 在 `mu.Lock` 内 `readPool.Release()` + `nil`

```go
p.mu.Lock()
defer p.mu.Unlock()
...
if p.rw.readPool != nil {
    p.rw.readPool.Release()
    p.rw.readPool = nil
}
```
`ants.Release()` 是非阻塞的（标记 closed），后续仍 in-flight 的任务能跑完，但 `readPool = nil` 后任何并发的 `SetRWEnabled(true)` 会再次创建。`SetRWEnabled` 也持 `mu.Lock`，OK。但建议把 `readPool` 释放放到 `BeginStop` 之后、`Wait` 末尾（逻辑上"运行结束才释放资源池"），避免与 `EnsureReadResources` 在动态切换路径中潜在的语义混淆。

---

# 第二轮审计（2026-04-22）

> 第二轮聚焦审查 P0/P1/P2 全部修复后未覆盖的遗漏。下列项目均经过交叉验证（grep 调用点 + 阅读上下文）确认为真问题。

## P1-16 ✅ 已修复 — `pid.SetMaster` 对 `IsMaster` bool 字段的非原子写

### 现象
```go
func (pid *PID) SetMaster(master bool) {
    ...
    atomic.StoreInt32(&pid.MasterFlag, v)
    pid.IsMaster = master // 非原子写
}
```
`pid.IsMaster` 是 protobuf 生成的 `bool` 字段，无任何同步保护。`watcher.go` 中 `electMaster` / `KeepAliveLoop` 失败回调路径会调用 `pid.SetMaster(...)`，而 `registry.go:RegisterService` 紧接着会 `protojson.Marshal(pid)` 读取该字段（虽然目前在同 goroutine 顺序调用，无即时 race），但任何后续新增 goroutine 调用 `pid.GetIsMaster()` / `protojson.Marshal(pid)` 都会立即触发 race。

`pid_test.go` 中的 `TestPID_SetMaster_Concurrent` 仅校验了 `IsMasterNode()`（atomic 读），未覆盖 `IsMaster` bool 字段的并发读，掩盖了风险。

### 影响
- 当前路径无即时 race，但属于 latent bug：任何"序列化 PID"或"读取 pid.IsMaster"的新增 goroutine 都会触发 `-race` 报错；
- 违反 "MasterFlag/IsMaster 双源一致性" 的并发契约。

### 修复建议
- `SetMaster` 仅原子写 `MasterFlag`，删掉对 `pid.IsMaster` 的同步赋值；
- 在所有 PID 序列化出口（`registry.go:RegisterService` / `endpoints` 内部）调用统一辅助函数 `pid.IsMaster = pid.IsMasterNode()` 或包装成 `MarshalPIDForWire(pid)`；
- `pid_test.go` 增加并发 race 用例：一个 goroutine 反复 `SetMaster`，另一个 `protojson.Marshal(pid)`，`-race` 下应通过。

### 实际修复 (2026-04-22)

**文件**: `engine/pkg/actor/pid.go`、`engine/pkg/rpc/message/msgenvelope/envelope.go`、`engine/pkg/cluster/discovery/etcd/registry.go`、`engine/pkg/actor/pid_test.go`

设计原则：运行时唯一权威字段是 `MasterFlag`（atomic int32），`IsMaster` bool 仅作为 protobuf 序列化的传输载体，在序列化出口由 `MasterFlag` 投影。运行时所有读取走 `IsMasterNode()`。

- `pid.go`：
  - `SetMaster` 删除 `pid.IsMaster = master` 行，仅保留 `atomic.StoreInt32(&pid.MasterFlag, v)`；
  - 新增 `PrepareForMarshal()` 辅助函数：`pid.IsMaster = pid.IsMasterNode()`，仅在序列化出口调用；
  - 在 godoc 中明确并发契约：调用方需保证同一 PID 同时只有一个 goroutine 在 PrepareForMarshal+Marshal 的窗口内。
- `envelope.go.ToProtoMsg`：在 `msg.SenderPid = senderPid` / `msg.ReceiverPid = receiverPid` 赋值前调用 `senderPid.PrepareForMarshal()` / `receiverPid.PrepareForMarshal()`。
- `registry.go.RegisterService`：在 `protojson.Marshal(pid)` 之前调用 `pid.PrepareForMarshal()`。
- `pid_test.go` 新增两个 `-race` 测试：
  - `TestPID_SetMaster_DoesNotTouchIsMasterBool`：断言 `SetMaster(true)` 后 `IsMaster` 仍为 false（未被触碰），仅 `IsMasterNode()` 反映新值；调用 `PrepareForMarshal()` 后 `IsMaster` 才与 `MasterFlag` 一致；
  - `TestPID_PrepareForMarshal_ConcurrentWithSetMaster`：并发调用 `SetMaster`（写 MasterFlag）与 `PrepareForMarshal`（写 IsMaster），两者操作不同字段，`-race` 下无报错。

验证：`go build ./...` ✅ `go vet ./...` ✅ `go test -race -count=1 ./engine/pkg/actor/... ./engine/pkg/core/... ./engine/pkg/cluster/...` ✅

---

## P1-17 ✅ 已修复 — `CircuitBreaker.Reset()` 未持 `mu` 锁，与状态机切换竞态

### 现象
P1-4 修复时已统一 `OnReceive` Open→HalfOpen 与 `OnComplete` HalfOpen→Open/Closed 的 `mu` 内组合写，但 `Reset()` 仍是裸 atomic Store：

```go
func (m *CircuitBreakerMiddleware) Reset() {
    m.state.Store(int32(StateClosed))
    m.failures.Store(0)
    m.successes.Store(0)
    m.halfOpenReqs.Store(0)
    now := time.Now().UnixNano()
    m.windowStart.Store(now)
    m.lastFailTime.Store(now)
    ...
}
```

并发并发流量同时跑 OnComplete 的 HalfOpen→Closed/Open 组合写时，`Reset()` 的多个 `Store` 之间可被插入，导致部分字段被覆盖回旧值（典型场景：state=Closed 但 failures 被并发的探测失败 OnComplete 改为非零）。

### 修复建议
`Reset()` 内套 `m.mu.Lock()/Unlock()`，与 OnReceive/OnComplete 的状态机切换串行化。

### 实际修复 (2026-04-22)

**文件**: `engine/pkg/actor/mailbox/circuit_breaker_middleware.go` `Reset()`

- 在 `Reset()` 中三定义中的 6 个字段写入（state/failures/successes/halfOpenReqs/windowStart/lastFailTime）外套 `m.mu.Lock()/Unlock()`；logger 调用移出锁以避免持锁 I/O；
- godoc 明确说明与 OnReceive Open→HalfOpen / OnComplete HalfOpen→Open/Closed 串行化的契约。

---

## P1-18 ✅ 已修复 — `DispatchKeyStatsMiddleware.OnStop` 不等待后台 goroutine 退出

### 现象
```go
func (m *DispatchKeyStatsMiddleware) OnStart() {
    ...
    go func() {
        defer ticker.Stop()
        for {
            select {
            case <-ticker.C:
                m.reportAndReset()
            case <-m.stopCh:
                m.reportAndReset()
                return
            }
        }
    }()
}

func (m *DispatchKeyStatsMiddleware) OnStop() {
    m.stopOnce.Do(func() { close(m.stopCh) })
}
```

`OnStop` 仅 close stopCh 即返回，`MiddlewareChain.Stop()` 之后后台 goroutine 仍可能正在 `reportAndReset` 内访问 `m.shards`/`m.logger`，与外部"中间件已停止"的语义不一致。

### 影响
- 测试场景下偶发 `-race` 抖动；
- 极端情况下 logger 在 Service 销毁后被关闭，goroutine 触发 nil 写或 panic。

### 修复建议
- 增加 `done chan struct{}`（或 `sync.WaitGroup`），goroutine 退出前 close（或 Done）；
- `OnStop` 在 close stopCh 后阻塞等待 done close，确保返回时后台 goroutine 已彻底退出。

### 实际修复 (2026-04-22)

**文件**: `engine/pkg/actor/mailbox/dispatch_key_stats_middleware.go`

- `DispatchKeyStatsMiddleware` 新增 `doneCh chan struct{}` 字段，构造函数中初始化；
- `OnStart` 启动的后台 goroutine 采用 `defer close(m.doneCh)` 在退出前通知；
- `OnStop` 在 `close(m.stopCh)` 后检查 `m.running.Load()`：仅当后台 goroutine 已被 `OnStart` 启动过才 `<-m.doneCh` 阻塞等待，避免未启动场景下永远阻塞。

---

## P2-19 ✅ 已优化 — `drainDiscardTotal` 计数语义重复

### 现象
- `worker.go:run()` 末尾 unsafe drain 入口 `w.env.rw.drainDiscardTotal.Add(1)`（事件级，标记"发生过 unsafe drain"）；
- `discardExec` 内对**每个 job** 又 `w.env.rw.drainDiscardTotal.Add(1)`（per-job 计数）。

两者使用同一原子变量，最终 `RWMetrics.DrainDiscardTotal` = 1（事件标记）+ N（job 数），含义混乱。

### 修复建议
- 拆出独立 `unsafeDrainEvents atomic.Int64` 字段，事件级标记单独累加；
- 或直接移除 unsafe drain 入口的 Add（unsafe drain 通过日志 + per-job 计数推断）。
- `RWMetrics` 同步暴露新指标。

### 实际修复 (2026-04-22)

**文件**: `engine/pkg/actor/mailbox/rw_controller.go`、`engine/pkg/actor/mailbox/worker.go`、`engine/pkg/actor/mailbox/worker_pool.go`

- `RWController` 新增 `unsafeDrainEvents atomic.Int64` 字段，语义为"进入 unsafe drain 路径的事件计数"；`drainDiscardTotal` 语义明确为"per-job 丢弃计数"；
- `worker.go:run()` defer 内 unsafe drain 分支中的 `drainDiscardTotal.Add(1)` 改为 `unsafeDrainEvents.Add(1)`；`discardExec` 内 per-job `drainDiscardTotal.Add(1)` 保留；
- `RWMetrics` 新增 `UnsafeDrainEvents int64` 字段，`GetRWMetrics()` 中赋值。应用侧可区分“unsafe drain 事件次数”与“被丢弃的 Job 总数”。

---

## P2-20 ✅ 已清理 — `WorkerPool.profiler` 字段从未赋值，profiler 链路死代码

### 现象
- `worker_pool.go` 声明 `profiler *profiler.Profiler`，`workerEnv()` 透传给 worker；
- 全代码库 grep `WorkerPool.profiler` / `pool.profiler =` 均无任何赋值点；
- `worker.go:safeExecInternal` 中 `if w.env.profiler != nil { ... }` 分支永远走不到，profiler.Push/Pop 全部死代码。

### 决策
profiler 付是还在规划中的特性，当前的集成逻辑全部为死代码，先清理接入点以避免误导。`engine/pkg/profiler` 包代码保留，待后续重新规划后再接入。

### 实际修复 (2026-04-22)

**文件**: `engine/pkg/actor/mailbox/worker.go`、`engine/pkg/actor/mailbox/worker_pool.go`

- `worker.go`：
  - 移除 `"github.com/njtc406/emberengine/engine/pkg/profiler"` import 与 `"strconv"` import（后者仅为 profiler 调用服务）；
  - `WorkerEnv` 删除 `profiler *profiler.Profiler` 字段；
  - `safeExecInternal` 删除 `var analyzer *profiler.Analyzer` 与 `if w.env.profiler != nil { analyzer = w.env.profiler.Push(...) }` 分支以及末尾 `analyzer.Pop()` 逻辑；
  - 参数名 `skipProfiler` 重命名为 `skipShared`（表达语义从"跳过 profiler"迁移为"跳过同步运行路径上的共享状态"）；`safeExecSkipProfiler` 函数名和外部调用点保持不变，避免调用点迁移。
- `worker_pool.go`：
  - 移除 `profiler` import；
  - `WorkerPool` 删除 `profiler *profiler.Profiler` 字段；
  - `workerEnv()` 移除 `profiler: p.profiler` 赋值。

`engine/pkg/profiler/` 目录代码保持原状，待后续 profiler 重新规划时从 WorkerPool 构造函数重新接入。

---

## 第二轮已交叉验证为"非真问题"的疑点（不入修复清单）

记录在此供后续审计参考，避免重复研判。

| 疑点 | 验证结论 |
|------|---------|
| `handler.go` 失败路径 meta/data 泄漏 | `data.go:NewData()` 注释明确"不入池"（直接 `return &Data{}`），`envelope.Release()` 已 putMeta；无泄漏 |
| `handler.go` `sf.GetDispatcher(...).DeliverRequest()` nil panic | `EndpointManager.GetDispatcher` 已对 nil fallback 到 `AddTmp`，永不返回 nil |
| `AutoScaler.lastResizeTime` 非原子 | 仅由 `autoScaleWorkers` 单 goroutine 调用，无并发；非真 race |
| `scheduler.go:resetThreshold = 1e10` | 无类型浮点常量，64-bit 平台编译为 int 合法；纯风格问题 |
| `execRead.sleepBackoff` writeRequested 让步后未重置 | 实测路径下进入 sleep 退避前必经 `writeRequested.Load()==0` 校验，sleepBackoff 为 0；极端边界仍可优化但非 bug |

---

## 第二轮新增项优先级与状态总览

| 等级 | 项 | 状态 | 影响面 | 修复成本 |
|------|----|------|--------|---------|
| P1-16 | `pid.SetMaster` 写 IsMaster bool race | ✅ 已修复 | 序列化路径 | 小 |
| P1-17 | `CircuitBreaker.Reset()` 缺 mu | ✅ 已修复 | 启用熔断的 Service | 极小 |
| P1-18 | DispatchKeyStats OnStop 不等 goroutine | ✅ 已修复 | 启用 debug 统计的 Service | 小 |
| P2-19 | drainDiscardTotal 语义重复 | ✅ 已优化 | 可观测性 | 极小 |
| P2-20 | WorkerPool.profiler 死代码 | ✅ 已清理 | 可维护性 | 小 |

---

## 不属于 bug 但建议增强测试的点

1. `Mailbox.PostJob` 在 Suspend / 中间件 Reject / Worker Closed 三种失败路径下，`MsgJobPool` `Active` 计数守恒（验证 P0-1 修复）；
2. `RW` 模式下注入"读 goroutine 阻塞 > stopTimeout"用例，验证 P0-2 不再 race（`go test -race`）；
3. `resizeWorkers` 高并发缩容 + Dispatch 丢消息率 = 0（验证 P1-3）；
4. `CircuitBreaker` HalfOpen 探测并发上限严格 ≤ `halfOpenMaxAllowed`（验证 P1-4）；
5. `PriorityQueueManager` 注册 17 个优先级时返回 error（验证 P1-9）。

---

## 修复优先级与状态总览

| 等级 | 项 | 状态 | 影响面 | 修复成本 |
|------|----|------|--------|---------|
| P0-1 | Job + mctx 投递失败泄漏 | ✅ 已修复 | 全链路 | 小 |
| P0-2 | RW StopTimeout 后 DrainDiscard race | ✅ 已修复 | RW Service | 中 |
| P1-3 | 缩容窗口丢消息 | ✅ 已修复 | 自动扩缩容 Service | 小 |
| P1-4 | CircuitBreaker HalfOpen race | ✅ 已修复 | 启用熔断的 Service | 小 |
| P1-5 | execRead Gosched 燃烧 CPU | ✅ 已修复 | RW 高读负载 | 小 |
| P1-6 | watchdog 仅日志 | ✅ 已修复 | 可观测性 | 小 |
| P1-7 | PID Sync 检查 | ✅ 已修复 | 网络入口 | 小 |
| P1-8 | MiddlewareContext Reset 文档 | ✅ 已修复 | 文档增强 | 极小 |
| P1-9 | PriorityQueue 优先级越界 | ✅ 已修复 | 自定义优先级 | 小 |
| P2-10 | CompositeStrategy 大小写 | ✅ 已修复 | 可维护性 | 极小 |
| P2-11 | logDispatchStatsOnce 死代码 | ✅ 已修复 | 可维护性 | 极小 |
| P2-12 | BeginStop 自旋退避 | ✅ 已修复 | 可维护性 | 极小 |
| P2-13 | shardFor 重复 hash | ✅ 已优化 | 性能微调 | 极小 |
| P2-14 | JobFactory frozen 时序 | ✅ 已优化 | 扩展性 | 小 |
| P2-15 | readPool 释放位置 | ✅ 已优化 | 语义清晰度 | 极小 |
| P1-16 | `pid.SetMaster` 写 IsMaster bool race | ✅ 已修复 | 序列化路径 | 小 |
| P1-17 | `CircuitBreaker.Reset()` 缺 mu | ✅ 已修复 | 启用熔断的 Service | 极小 |
| P1-18 | DispatchKeyStats OnStop 不等 goroutine | ✅ 已修复 | 启用 debug 统计的 Service | 小 |
| P2-19 | drainDiscardTotal 语义重复 | ✅ 已优化 | 可观测性 | 极小 |
| P2-20 | WorkerPool.profiler 死代码 | ✅ 已清理 | 可维护性 | 小 |

> 已修复项均通过 `go build ./...` + `go vet ./...` + `go test -race ./engine/pkg/actor/... ./engine/pkg/core/... ./engine/pkg/cluster/...` 验证。
> 第二轮新增项 P1-16/P1-17/P1-18/P2-19/P2-20 全部完成（2026-04-22）。

