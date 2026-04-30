# Actor 模块独立审计报告 — 2026 Q2

> 审计范围：[engine/pkg/actor](../engine/pkg/actor)（含 [pid.go](../engine/pkg/actor/pid.go)、[event.go](../engine/pkg/actor/event.go)、[actor.pb.go](../engine/pkg/actor/actor.pb.go)）与 [engine/pkg/actor/mailbox/](../engine/pkg/actor/mailbox)（worker / worker_pool / rw_controller / mailbox / queue_manager* / middleware* / scheduler / scaler / strategy / suspend_policy / stop_policy / format / job/）。
>
> 跨模块对照：[engine/pkg/core/service.go](../engine/pkg/core/service.go)、[engine/pkg/rpc/message/msgenvelope/envelope.go](../engine/pkg/rpc/message/msgenvelope/envelope.go)、[engine/pkg/cluster/discovery/etcd/registry.go](../engine/pkg/cluster/discovery/etcd/registry.go)、[engine/pkg/utils/mpsc/deque.go](../engine/pkg/utils/mpsc/deque.go)、[engine/pkg/utils/idle/idle.go](../engine/pkg/utils/idle/idle.go)。
>
> 审计日期：2026-04-26  
> 审计方式：独立阅读源码，未参考既有 review 文档。

---

## 总览

| 等级 | 数量 | 含义 | 本轮已修复 | 暂缓 |
|------|------|------|-----------|------|
| P0 | 6 | 真正的设计漏洞：数据正确性 / 顺序违反 / 资源契约破口 | 6 全修 | 0 |
| P1 | 9 | 性能问题：热路径明确开销，可量化收益 | 9（P1-1 / P1-2 / P1-3 / P1-4 / P1-5 / P1-6 / P1-7 / P1-8 / P1-9）| 0 |
| P2 | 12 | 设计一致性 / 可维护性问题 | 12（P2-1 ~ P2-12）| 0 |

> 本轮修复同步落地的 ADR：**ADR-1（Phase 1 完成）**、**ADR-2（2026-04-27 落地）**、**ADR-4（Phase 1 完成）**。详见文末「七、本轮修复执行情况（2026-04-26）」。

模块整体设计相对成熟（生命周期、停机、可观测性、扩展性都有覆盖），主要风险集中在：

1. **PID 序列化契约依靠人工约束**，调用方已在多处绕过；
2. **WorkerPool 拓扑与 dispatch 共享 RWMutex**，是最大热路径瓶颈，并叠加缩容顺序问题；
3. **RW 读写分离实现了"读串行化在主循环 + 写抢全局锁"**，未真正释放 worker 主循环；
4. **Job 资源所有权契约脆弱**，错误路径上 OnJobDiscarded 回调不对称；
5. **运行时多重 hack（watchdog、限流、统计）在热路径常驻**，与默认配置不匹配。

---

## 一、P0 — 设计漏洞

### P0-1 PID.PrepareForMarshal 契约在调用方被违反 ✅ 已修复（2026-04-26）

| 项目 | 内容 |
|------|------|
| 位置 | [pid.go#L80-L98](../engine/pkg/actor/pid.go#L80-L98) vs [envelope.go#L177-L185](../engine/pkg/rpc/message/msgenvelope/envelope.go#L177-L185)、[registry.go#L29-L35](../engine/pkg/cluster/discovery/etcd/registry.go#L29-L35) |
| 现象 | `PrepareForMarshal` 文档要求"同一 PID 同时只有一个 goroutine 在执行"，并提供 `MarshalPID` 作为唯一推荐出口；但 envelope 与 etcd registry 均直接调用 `PrepareForMarshal + Marshal`，未走 MarshalPID。 |
| 后果 | 对端 PID 在并发 RPC 序列化时出现 `IsMaster` 字段非原子写竞争；`-race` 下可复现；生产中可能让 etcd 注册的 IsMaster 短暂取错值，连锁影响主从选择（[selector.go#L115](../engine/pkg/cluster/endpoints/repository/selector.go#L115)）。 |
| 建议方向 | 见 ADR-1：移除 wire schema 中的 `IsMaster` 字段，或为 PID 实现自定义 Marshaler。 |

---

### P0-2 缩容破坏 dispatcherKey 顺序契约 ✅ 已修复（2026-04-26）

| 项目 | 内容 |
|------|------|
| 位置 | [worker_pool.go#L341-L389](../engine/pkg/actor/mailbox/worker_pool.go#L341-L389) |
| 现象 | 缩容流程：先在 `p.mu.Lock()` 内删除 worker + `ring.RemoveMany`，解锁后再 BeginStop+Wait（被淘汰 worker 自行 Drain，默认 DrainExecute）。 |
| 后果 | 老 worker 仍在串行执行残留 N 条 Job，同 dispatcherKey 的新 Job 被 rehash 到另一个 worker 立即执行——违反 mailbox 对外承诺的"按 dispatcherKey 顺序执行"契约，等价于 actor 模型下"同一实体被两个 actor 并发处理"。 |
| 建议方向 | 缩容时先停 worker → Wait drain 完成 → 再从 ring 删除；或把残留 Job 整体转投到目标 worker。 |

---

### P0-3 execRead 是 worker 主循环里的 head-of-line blocker ✅ 已修复（2026-04-26，ADR-3 Phase 3 落地）

| 项目 | 内容 |
|------|------|
| 位置 | [worker.go#L455-L548](../engine/pkg/actor/mailbox/worker.go#L455-L548) |
| 现象 | 读 Job 出队后，主循环同步完成"closed 检查 → writeRequested 让步 → readSem 取令牌 → RLock"，最后才 spawn 读 goroutine。 |
| 后果 | <ul><li>写多/信号量满时整个主循环卡住，写 Job 与系统消息（SysCtl/紧急消息）同步排队，"读写分离"退化为串行；</li><li>`runtime.Gosched()×64` 内层自旋在 Windows/macOS 等高 sleep 精度差的系统上 CPU 占用显著；</li><li>Job 已出队（mpsc 不可回退），无法做"占不到资源就让其他 Job 先行"的策略。</li></ul> |
| 建议方向 | 见 ADR-3：把读令牌获取前移到入队侧；主循环只调度 write，读 Job 走单独的 dispatcher。 |
| 修复落地 | ADR-3 Phase 3 完成：每 Worker 引入独立 `readCh chan IMailboxJob` + `runReadPipeline` goroutine，主循环只做非阻塞 `dispatchRead`（CPU 烧毁与 head-of-line 同时根治）；通过 `readsDispatched`/`readsLaunched` 单调序号在 `execWrite` 入口设置「先序读已 RLock 注册」栅栏，保留 dispatcherKey 顺序契约；readCh 满 → ADR-4 OnJobDiscarded(ErrMailboxWorkerIsFull)；停机走 `close(readCh)+readPipelineWg.Wait()` 排空残留读，DrainExecute 语义不变。 |

---

### P0-4 SubmitJob/DispatchJob 失败不触发 OnJobDiscarded ✅ 已修复（2026-04-26）

| 项目 | 内容 |
|------|------|
| 位置 | [worker.go#L138-L170](../engine/pkg/actor/mailbox/worker.go#L138-L170) + [worker.go#L319-L375](../engine/pkg/actor/mailbox/worker.go#L319-L375) |
| 现象 | 回调约定不对称：<ul><li>Drain 丢弃：调用 `invoker.OnJobDiscarded(...)`，业务可释放外部资源；</li><li>SubmitJob 返回 `ErrMailboxWorkerClosed`、DispatchJob 返回 `ErrMailboxWorkerNotFound/IsFull`：仅向调用方返回 error，未触发 OnJobDiscarded。</li></ul> |
| 后果 | 调用链上层（如 [sender_local.go#L46-L52](../engine/pkg/rpc/client/sender_local.go#L46-L52)）只做 `rpcJob.Release()`，envelope 里挂的 callback、监控状态可能残留。缩容窗口/关闭窗口都会真实发生。 |
| 建议方向 | 见 ADR-4：所有"Job 不会被业务执行"的路径统一回调 OnJobDiscarded。 |

---

### P0-5 Mailbox.PostJob 错误路径不释放 Job，契约依赖调用方 ✅ 已修复（2026-04-26）

| 项目 | 内容 |
|------|------|
| 位置 | [mailbox.go#L92-L132](../engine/pkg/actor/mailbox/mailbox.go#L92-L132) |
| 现象 | 中间件 Reject、DispatchJob 失败、Suspend 拒绝三条错误路径都不 Release Job，依赖每个调用方记得 `j.Release()`。 |
| 后果 | 当前明确遵守的有 [sender_local.go](../engine/pkg/rpc/client/sender_local.go)、[service.go](../engine/pkg/core/service.go)、[event/bus_*.go](../engine/pkg/event/bus_global.go)；但每新增一个调用点（如 etcd watcher、cluster 模块）都极易漏掉。 |
| 建议方向 | 见 ADR-4：Mailbox 内化"单向所有权"，成功/失败都由 mailbox 自己负责释放。 |

---

### P0-6 RW Disable 不等待 in-flight 读 goroutine，存在安全切换破口 ✅ 已修复（2026-04-27）

| 项目 | 内容 |
|------|------|
| 位置 | [rw_controller.go#L84-L100](../engine/pkg/actor/mailbox/rw_controller.go#L84-L100) + [worker.go#L182-L260](../engine/pkg/actor/mailbox/worker.go#L182-L260) |
| 现象 | `Disable()` 仅 `mu.Lock()` 抢一次 WLock 就翻 `enabled=false`，不等已 spawn 的读 goroutine 完成；同时 `execRead` 在 RLock + enabled 重检之后 spawn，其间未再次检查 state。 |
| 后果 | Disable 翻完标志后，`inflightReads.Load()` 仍可能 >0 一段时间；新进来的写 Job 走串行路径（safeExec），与那些读 goroutine 并发访问 invoker 共享状态——SetRWEnabled(false) 真正的破口。 |
| 建议方向 | Disable 必须 `WLock + 等待 inflightReads.Wait()`；或先 Stop 所有 worker → 切换 → 重启。 |

---

## 二、P1 — 性能问题

### P1-1 DispatchJob 在 RWMutex 持锁内执行 SubmitJob ✅ 已修复（2026-04-27，ADR-2 Phase 2）
- **位置**：[worker_pool.go#L284-L332](../engine/pkg/actor/mailbox/worker_pool.go#L284-L332)
- **问题**：每条事件付一次 RWMutex.RLock/RUnlock + map 查找；锁内还要 mpsc.Push、atomic.Add、Cond signal。
- **影响**：与统计/扩缩容/metrics 共享 `p.mu`，扩缩容拿 W 锁时整个 dispatch 路径瞬时停摆。
- **方案**：见 ADR-2，workers 拓扑用 `atomic.Pointer[snapshot]` COW。

### P1-2 watchdog timer per-job 在高 QPS 短任务场景纯成本 ✅ 已修复（2026-04-27）
- **位置**：[worker.go#L364-L383](../engine/pkg/actor/mailbox/worker.go#L364-L383)
- **问题**：`fixConf` 在 RW 启用时强制 `MaxJobExecutionTime=30s`，每条 Job 都进时间轮 Schedule + defer Cancel。
- **方案**：默认关闭；按 Job 类型/采样率开启；与 deadline 字段合并。

### P1-3 execRead 每次构造 `context.WithValue` ✅ 已修复（2026-04-26）
- **位置**：[worker.go#L398-L405](../engine/pkg/actor/mailbox/worker.go#L398-L405)
- **问题**：`RWContextInfo{Mode, SourceService}` 每条读 Job 重建一次，`SourceService` 在 worker 生命周期内是常量。
- **方案**：worker 创建时缓存 base context，读路径直接 derive。

### P1-4 PriorityQueueManager.NextJob 完整扫优先级 + 调度内分配 ✅ 部分修复（2026-04-27，复用 buffer）
- **位置**：[queue_manager_priority.go#L120-L155](../engine/pkg/actor/mailbox/queue_manager_priority.go#L120-L155) + [scheduler.go#L113-L196](../engine/pkg/actor/mailbox/scheduler.go#L113-L196)
- **问题**：每次 NextJob 做 N 次 `mpsc.Empty()`；加权/公平分支每次分配 `samePriorityQueues` 切片。
- **方案**：用 `atomic.Uint64` 位图标"非空优先级"，Submit/Pop 时 CAS 维护；same-priority buffer 复用。

### P1-5 MiddlewareContext 的 RWMutex 在串行链路上是冗余 ✅ 已修复（2026-04-26）
- **位置**：[middleware_chain.go#L60-L75](../engine/pkg/actor/mailbox/middleware_chain.go#L60-L75)
- **问题**：mctx 的 Set/Get 锁保护 data map；中间件 OnReceive→business→OnComplete 在同一 worker goroutine 串行，无跨 goroutine 访问需求。每条 Job 走 N 个中间件 = 2N 次 RWMutex 开销。
- **方案**：去掉 RWMutex；若 RW 模式有跨 goroutine 需求，改为 immutable 拷贝或 sync.Map。

### P1-6 hashring + VirtualWorkerRate=24 重建成本 ✅ 已修复（2026-04-27，jump consistent hash 替代）
- **位置**：[worker_pool.go#L165-L168, L329-L335](../engine/pkg/actor/mailbox/worker_pool.go#L165-L168)
- **问题**：每 `ring.Add(id)` 插入 24 个虚拟节点，频繁扩缩容时排序/树重建累积。
- **方案**：worker id 是单调 int32，可改用 jump consistent hash 替代。

### P1-7 DispatchJob 每条都 GetJobLen + scaleTrigger select ✅ 已修复（2026-04-27）
- **位置**：[worker_pool.go#L323-L329](../engine/pkg/actor/mailbox/worker_pool.go#L323-L329)
- **问题**：极高 QPS 下 atomic.Load + channel select 仍是非零成本。
- **方案**：按"队列长度跨阈值"事件触发，而非每条都触发。

### P1-8 DispatchKeyStatsMiddleware 分片哈希取前 32 字节命中长共享前缀 ✅ 已修复（2026-04-27）
- **位置**：[dispatch_key_stats_middleware.go#L72-L86](../engine/pkg/actor/mailbox/dispatch_key_stats_middleware.go#L72-L86)
- **问题**：dispatcherKey 在本框架里大量是 `serviceName.serviceId.partition` 格式（[pid.go#L122-L124](../engine/pkg/actor/pid.go#L122-L124)），共享前缀很长，截断前 32 字节哈希时大量 key 落到同一 shard。
- **方案**：改 fnv/xxhash 全长，或采样末尾 32 字节。

### P1-9 fixConf 改写传入的 conf 结构体 ✅ 已修复（2026-04-26）
- **位置**：[worker_pool.go#L709-L780](../engine/pkg/actor/mailbox/worker_pool.go#L709-L780)
- **问题**：直接修改入参字段（如 `EnableRWMode`、`MaxConcurrentReads`、`MaxJobExecutionTime`）。如果上层共享同一 `*MailboxConf` 给多个 service，会出现"初始化某个 service 改坏了模板"的隐性副作用。
- **方案**：fixConf 内部 deep-copy 一份再修改。

---

## 三、P2 — 设计一致性 / 可维护性问题

### P2-1 PriorityQueueManager.Submit fallback 静默改优先级 ✅ 已修复（2026-04-26）
- **位置**：[queue_manager_priority.go#L98-L116](../engine/pkg/actor/mailbox/queue_manager_priority.go#L98-L116)
- **问题**：未注册 priority fallback 到最低优先级，但 SuspendPolicy / 限流 / DispatchKey 统计仍按原 priority 判断。
- **方案**：fallback 时把 priority 写回 Job，或直接返回 error。

### P2-2 RateLimit token 在下游 Reject 时被浪费 ✅ 已修复（2026-04-27，链顺序调整）
- **位置**：[rate_limit_middleware.go#L100-L113](../engine/pkg/actor/mailbox/rate_limit_middleware.go#L100-L113)
- **问题**：`Allow()` 已消费 token，后续 CB/Suspend 拒绝时业务并未执行，限流口径偏严。
- **方案**：把 RateLimit 排到中间件链最后；或自实现 token 桶以支持 OnComplete 归还。

### P2-3 CircuitBreaker 把"上游 Reject"也算入失败窗口 ✅ 已修复（2026-04-27）
- **位置**：[circuit_breaker_middleware.go#L228-L260](../engine/pkg/actor/mailbox/circuit_breaker_middleware.go#L228-L260)
- **问题**：sentinel 拒绝产生的 ErrSentinelBlocked 会污染自研 CB 的失败率。
- **方案**：在 `MiddlewareResult` 上加错误来源字段，区分"上游 Reject"vs"业务失败"。

### P2-4 自研 CircuitBreaker 与 Sentinel 中间件功能重叠 ✅ 已修复（2026-04-27，删除自研版，统一到 Sentinel）
- **位置**：[circuit_breaker_middleware.go](../engine/pkg/actor/mailbox/circuit_breaker_middleware.go) 与 [sentinel_middleware.go](../engine/pkg/actor/mailbox/sentinel_middleware.go)
- **问题**：能力高度重叠，自研版状态切换较脆弱。
- **方案**：架构上保留一种。

### P2-5 sentinel system rules 全局 sync.Once，"先到先赢" ✅ 已修复（2026-04-27，改为全局合并重载）
- **位置**：[sentinel_middleware.go#L283-L297](../engine/pkg/actor/mailbox/sentinel_middleware.go#L283-L297)
- **问题**：第一个启动的 service 决定全局规则，其他 service 静默忽略，service 启动顺序不确定。
- **方案**：system rules 收敛到 Node 启动入口统一注册，service 中间件只注册资源级规则。

### P2-6 SysCtlJob 实际 handler 是空 stub ✅ 已修复（2026-04-27，新增 SysCtl 注册中心）
- **位置**：[handler_job.go#L186-L196](../engine/pkg/core/handler_job.go#L186-L196)
- **问题**：注释 TODO 实现 mailbox 挂起/恢复/健康检查/系统命令，当前未实现。
- **方案**：补齐 SysCtl 注册中心。

### P2-7 pendingJob 单值字段是隐式约束 ✅ 已修复（2026-04-27）
- **位置**：[worker.go#L73-L75](../engine/pkg/actor/mailbox/worker.go#L73-L75)
- **问题**：execRead/execWrite 都会暂存 pendingJob，依赖"主循环单 goroutine + 不会出现两次 pending"。
- **方案**：用长度 1 的 buffered chan 或显式 panic on overwrite。

### P2-8 AutoScaler 在 MaxWorkerNum 边界态没有冷却保护 ✅ 已修复（2026-04-26）
- **位置**：[scaler.go#L60-L67](../engine/pkg/actor/mailbox/scaler.go#L60-L67)
- **问题**：`newSize == cur` 时不更新 lastResizeTime，下次 scaleTrigger 立即评估策略——持续过载下策略热评估。
- **方案**：任何决策（含无操作）都更新冷却时间。

### P2-9 Job 资源所有权 & ref-count 复杂 ✅ 已修复（2026-04-27，文档套约 + 样板去重 + 去决货警告）
- **位置**：[job.go](../engine/pkg/actor/mailbox/job/job.go)
- **问题**：sync.Pool（Reset/Put）+ DataRef.Ref/UnRef ref-count + RpcJob.Release 内 debug 检查，多重所有权语义叠加。
- **方案**：明确"Job 所有权流转图"，把"PostJob 失败后必须 Release"内化到 Mailbox（见 P0-5 / ADR-4）。

### P2-10 writeRequested 全局计数粒度过粗 ✅ 已修复（2026-04-27，拆 per-Worker）
- **位置**：[worker.go#L600-L640](../engine/pkg/actor/mailbox/worker.go#L600-L640) + [L470-L490](../engine/pkg/actor/mailbox/worker.go#L470-L490)
- **问题**：任一 worker 有写 Job 排队时，全局所有 worker 的读路径都开始 yield 让步。
- **方案**：拆到 per-worker（写本来就只在本 worker 主循环执行）；或只用 boolean flip。

### P2-11 SubmitJob 总是 count.Add(1)（已自标 TODO） ✅ 已修复（2026-04-27）
- **位置**：[worker.go#L161-L165](../engine/pkg/actor/mailbox/worker.go#L161-L165)
- **问题**：release 环境每条都付 atomic.Add 没必要。
- **方案**：开关化，注释中已经写明，待落地。

### P2-12 GetEnableRWPtr 对外暴露 atomic 指针 ✅ 已修复（2026-04-27）
- **位置**：[worker_pool.go#L607-L611](../engine/pkg/actor/mailbox/worker_pool.go#L607-L611)
- **问题**：MethodMgr 直接拿 `*atomic.Bool` 引用，破坏封装；将来 RW 状态语义扩展（如增加"正在切换"状态）所有持引用方都得改。
- **方案**：提供 `IsEnabled()` 接口或事件订阅。

---

## 四、ADR

### ADR-1：消除 PrepareForMarshal 的契约破口 ✅ Phase 1 落地（2026-04-26）

**背景**

当前 PID 序列化依赖人肉 grep + 文档约束 `MarshalPID` 是唯一出口；但 envelope 与 etcd registry 已直接调用 `PrepareForMarshal + Marshal`，绕过 MarshalPID。`-race` 下可复现 IsMaster 字段非原子写竞争。

**决策**

方案 B（自定义 Marshaler，兼容现网 wire format）：让 `PID` 实现 `proto.Marshaler` 或 vtprotobuf 自定义出口，在底层 marshal 入口自动 `MasterFlag → IsMaster` 投影到独立的栈上副本。

**优点**

- 调用方无需感知，所有 `proto.Marshal(pid)` 路径自动安全；
- 兼容现网 wire format，无需协调集群 schema 版本；
- CI 可彻底删除"禁止直接调用 PrepareForMarshal"的 grep 规则。

**缺点**

- 需要引入 vtprotobuf 或手写 marshal，构建链路调整；
- 仍保留 `IsMaster` 字段作为 wire 字段，不够干净。

**备选方案**

方案 A：在 `actor.proto` 中删除 `IsMaster` 字段（field 10 retire），仅保留 `MasterFlag`，所有读写都走 atomic。  
→ 简单干净但需要协调集群 schema 版本，灰度成本高。

**实施步骤**

1. 为 `PID` 实现自定义 Marshaler；
2. 移除 envelope/registry 中的 `PrepareForMarshal` 显式调用；
3. CI 增加 grep 规则禁止新增 `pid.PrepareForMarshal()` 调用；
4. 增加 race 测试覆盖并发 RPC + 主从切换场景。

---

### ADR-2：dispatch 拓扑无锁化 ✅ 已落地（2026-04-27）

**背景**

DispatchJob 持 RLock 是热路径瓶颈，且与扩缩容/统计/metrics 争锁。扩缩容拿 W 锁时整个 dispatch 路径瞬时停摆。

**决策**

定义 `workersSnapshot{ ring *HashRing[int32]; workers []inf.IMailboxWorker; idIndex map[int32]int }`，由 WorkerPool 持 `atomic.Pointer[workersSnapshot]`。扩缩容通过 COW 替换指针；DispatchJob 走 `Load()` 全程无锁。

**优点**

- DispatchJob 路径无锁化，吞吐显著提升；
- GetRWMetrics、stats 也走 snapshot.Load，与 dispatch 解耦；
- 顺势修复 P0-2 缩容顺序问题（在 snapshot 替换前先 Stop+Wait 老 worker）。

**缺点**

- 替换瞬间已在路上的 dispatch 仍按旧 snapshot 投递（语义可接受，因为 worker 不会在我们没等待 drain 之前被释放）；
- 实现复杂度上升，需要谨慎处理读端的 snapshot 引用生命周期。

**备选方案**

把 RWMutex 拆分为 hot/cold 两把锁（dispatch 用专用锁），收益有限，且仍承担锁开销。

**实施步骤**

1. 抽取 `workersSnapshot` 结构与 hashring；
2. 改写 `DispatchJob`、`GetRWMetrics`、`logDispatchStatsOnce` 走 snapshot；
3. 改写 `resizeWorkers`：扩容 → 构建新 snapshot → CAS；缩容 → BeginStop+Wait 老 worker → 构建新 snapshot → CAS；
4. 删除 dispatch 路径上的 `p.mu`，仅保留写端 mutex 串行化 resize。

---

### ADR-3：RW 读路径解耦于 worker 主循环 ✅ Phase 3 落地（2026-04-26）

**背景**

execRead 是主循环里的 head-of-line blocker，使 RW 模式在中重负载下退化为"读串行 + 写串行 + 协同等待"。

**决策**

1. 入队侧根据 RWMode 分双队列：read-queue / write-queue；
2. Worker 主循环只调度 write；
3. 单独的 read-dispatcher（per-worker 或 per-pool）负责从 read-queue 取 Job → 等令牌/RLock → spawn；
4. readSem 满时**入队前**回压（Submit 失败而非积压），避免热自旋。

**优点**

- 写 Job 不再被读 Job 阻塞，主循环延迟稳定；
- 读令牌满时上游回压，消除主循环热自旋；
- 系统消息（SysCtl/紧急消息）不再被 RW 拖延。

**缺点**

- 与现有"Job 出队即必处理"语义相比，需引入 read-queue 的 reject 路径；
- read-dispatcher 自身的调度公平性需要设计；
- 实现复杂度大。

**备选方案**

仅做 P1-3 等小修补（缓存 base context、降低 spin 频率），不能根治 head-of-line。

**实施步骤**

1. 在 `IQueueManager` 抽象上增加 read-queue 维度；
2. 实现 read-dispatcher（推荐 per-pool 单例，避免 per-worker goroutine 数量翻倍）；
3. SubmitJob 按 RWMode 路由到不同队列；
4. 引入入队侧令牌检查 + 回压。

---

### ADR-4：Mailbox 拥有 Job 所有权 + OnJobDiscarded 回调对称 ✅ Phase 1 落地（2026-04-26）

**背景**

当前调用方各自负责错误路径 Release，且 SubmitJob/DispatchJob 错误不通知业务，多重所有权语义叠加。

**决策**

- Mailbox.PostJob：成功/失败均由 Mailbox 内部 Release；
- 所有"Job 不会被业务执行"的路径（Reject、Suspend、Closed、Drain）统一调用 `invoker.OnJobDiscarded`；
- 修改文档与所有调用点，从源头清掉 ref-count 漏拼接的可能。

**优点**

- 调用方契约简单：`PostJob` 返回后无需关心 Job 生命周期；
- OnJobDiscarded 回调对称，业务可统一审计 / 释放外部资源；
- 消除 P0-4 / P0-5 两个漏洞。

**缺点**

- 现有调用方需统一改造（虽然简化代码，但触面大）；
- OnJobDiscarded 的调用语义需明确（同步 vs 异步 vs 是否在锁内）。

**备选方案**

仅改 OnJobDiscarded 对称，不改 Release 所有权——少改一点但仍留契约脆弱。

**实施步骤**

1. 在 [mailbox.go](../engine/pkg/actor/mailbox/mailbox.go) 内化 Release：所有 error 分支都 Release；
2. 在 SubmitJob/DispatchJob 失败路径补 OnJobDiscarded；
3. 修改所有调用方移除外部 Release（[sender_local.go](../engine/pkg/rpc/client/sender_local.go)、[service.go](../engine/pkg/core/service.go)、[event/bus_*.go](../engine/pkg/event/bus_global.go)、etcd watcher 等）；
4. 更新 [interfaces/IMailBox.go](../engine/pkg/interfaces/IMailBox.go) 文档。

---

### ADR-5：watchdog/统计/限流的成本下沉 ⏭️ 暂缓（按 P1-2 / P1-5 / P1-7 / P2-11 拆分推进，本轮仅 P1-5 落地）

**背景**

watchdog per-job timer、middleware mctx RWMutex、SubmitJob 的 count.Add 在热路径常驻，与默认配置不匹配。

**决策**

- watchdog 默认全关，由 Debug 模式或显式配置开启；按 deadline 字段共用时间轮；
- middleware mctx 去掉 RWMutex（P1-5）；
- SubmitJob count.Add 开关化（P2-11）；
- DispatchJob 的 GetJobLen + scaleTrigger 改阈值触发（P1-7）。

**优点**

实测可降低单条 Job 处理路径上 ~3-5 次原子操作 + 1 次 timer 调度。

**缺点**

可观测性默认降级，需要开关切换才能恢复。

**实施步骤**

按 P1-2 / P1-5 / P1-7 / P2-11 顺序独立修复。

---

## 五、实施优先级与路线图

```
Phase 1（数据正确性，必须先做）
├── ADR-1：PID 序列化破口
└── ADR-4 + P0-5：Mailbox 拥有所有权 + OnJobDiscarded 对称

Phase 2（性能与顺序契约）
├── ADR-2：dispatch 拓扑无锁化（顺势修复 P0-2 缩容顺序）
└── P1-2 / P1-5 / P1-7 / P1-9 等独立小修补

Phase 3（RW 模式重构）
├── P0-6：Disable 等待 in-flight 读
└── ADR-3：RW 读路径解耦

Phase 4（可维护性）
└── P2 系列 + 文档化 ADR
```

每个 Phase 完成后做一次基准测试（mailbox bench + race 测试 + 服务端集成压测），确认收益与回归。

---

## 六、参考索引

| 模块 | 主文件 |
|------|--------|
| PID | [pid.go](../engine/pkg/actor/pid.go) / [actor.pb.go](../engine/pkg/actor/actor.pb.go) |
| Mailbox 入口 | [mailbox.go](../engine/pkg/actor/mailbox/mailbox.go) |
| Worker | [worker.go](../engine/pkg/actor/mailbox/worker.go) |
| WorkerPool | [worker_pool.go](../engine/pkg/actor/mailbox/worker_pool.go) |
| RW Controller | [rw_controller.go](../engine/pkg/actor/mailbox/rw_controller.go) |
| 队列管理器 | [queue_manager.go](../engine/pkg/actor/mailbox/queue_manager.go) / [queue_manager_dual.go](../engine/pkg/actor/mailbox/queue_manager_dual.go) / [queue_manager_priority.go](../engine/pkg/actor/mailbox/queue_manager_priority.go) |
| 调度器 | [scheduler.go](../engine/pkg/actor/mailbox/scheduler.go) |
| 扩缩容 | [scaler.go](../engine/pkg/actor/mailbox/scaler.go) / [strategy.go](../engine/pkg/actor/mailbox/strategy.go) / [strategy_factory.go](../engine/pkg/actor/mailbox/strategy_factory.go) |
| 中间件链 | [middleware_chain.go](../engine/pkg/actor/mailbox/middleware_chain.go) / [middleware_factory.go](../engine/pkg/actor/mailbox/middleware_factory.go) |
| 中间件实现 | [rate_limit_middleware.go](../engine/pkg/actor/mailbox/rate_limit_middleware.go) / [circuit_breaker_middleware.go](../engine/pkg/actor/mailbox/circuit_breaker_middleware.go) / [sentinel_middleware.go](../engine/pkg/actor/mailbox/sentinel_middleware.go) / [dispatch_key_stats_middleware.go](../engine/pkg/actor/mailbox/dispatch_key_stats_middleware.go) |
| 策略 | [suspend_policy.go](../engine/pkg/actor/mailbox/suspend_policy.go) / [stop_policy.go](../engine/pkg/actor/mailbox/stop_policy.go) |
| Job | [job/job.go](../engine/pkg/actor/mailbox/job/job.go) / [job/job_factory.go](../engine/pkg/actor/mailbox/job/job_factory.go) |

---

*报告人：Architect Review*  
*报告日期：2026-04-26*

---

## 七、本轮修复执行情况（2026-04-26）

> 本节仅记录工程师按本审计报告执行的第一轮修复落地情况，便于后续 review/回归比对。

### 7.1 已落地修复（共 9 项全修 + 1 项部分修 + 1 行生缺陷）

| 编号 | 主题 | 关键改动 | 主要文件 |
|------|------|----------|----------|
| **P0-1 / ADR-1** | PID 序列化竞争 | 新增 `actor.SnapshotForWire(pid)`：内部 `proto.Clone` + `PrepareForMarshal` 副本，调用方安全；envelope 与 etcd registry 全部切换 | [pid.go](../engine/pkg/actor/pid.go) / [envelope.go](../engine/pkg/rpc/message/msgenvelope/envelope.go) / [registry.go](../engine/pkg/cluster/discovery/etcd/registry.go) |
| **P0-2** | 缩容破坏 dispatcherKey 顺序 | 缩容流程反转为“先 BeginStop+Wait 老 worker drain 完 → 再从 ring/workers map 删除”；缩容窗口内同 key 新 Job 走 ErrMailboxWorkerClosed（由 ADR-4 统一转 OnJobDiscarded），不再会与残留 Job 跨 worker 并发执行 | [worker_pool.go](../engine/pkg/actor/mailbox/worker_pool.go) |
| **P0-3 / ADR-3（全修）** | RW 读路径解耦 + CPU 烧毁 + head-of-line 同步根治 | 1) 每 Worker 新增独立 `readCh chan IMailboxJob` + `runReadPipeline` goroutine，主循环 `execWithRW` 写走 `execWrite`、读改非阻塞 `dispatchRead`；2) `launchRead` 把原 gate spin（writeRequested 让步 / readSem 取令牌）+ RLock + RLock-after-check + spawn 全部下沉到 readPipeline，主循环不再被读阻塞；3) gate 自旋统一用 `idle.SpinBackoff` 替换 `runtime.Gosched()×64`，CPU 烧毁子症状一并清除；4) **顺序契约**：`readsDispatched`/`readsLaunched` 单调原子序号，`execWrite` 入口先等 `readsLaunched ≥ readsDispatched 快照` 再去抢 WLock，保证「同 dispatcherKey 内先序读完成 RLock 注册后才进写」；5) **背压**：readCh 满 → ADR-4 OnJobDiscarded(ErrMailboxWorkerIsFull)；6) **停机**：main run defer 顺序为 `close(readCh) → readPipelineWg.Wait() → inflightReads.Wait()`，残留读全部 spawn，DrainExecute 语义不变；外层 `stopTimeout` 兜底；7) 新增配置 `MailboxConf.ReadDispatchChanCap`（默认 max(MaxConcurrentReads,256)）。 | [worker.go](../engine/pkg/actor/mailbox/worker.go) / [define.go](../engine/pkg/config/define.go) |
| **P0-4 + P0-5 / ADR-4** | Mailbox 内化 Job 所有权 + OnJobDiscarded 对称 | `Mailbox.PostJob` 三条错误路径（Suspended / 中间件 Reject / DispatchJob 失败）统一通过新 `discardJob(invoker, j, reason)` 走 `OnJobDiscarded + Release`；调用方移除外部 `Release` | [mailbox.go](../engine/pkg/actor/mailbox/mailbox.go) / [IMailBox.go](../engine/pkg/interfaces/IMailBox.go) / [service.go](../engine/pkg/core/service.go) / [sender_local.go](../engine/pkg/rpc/client/sender_local.go) / [call_state.go](../engine/pkg/monitor/call_state.go) / [bus_global.go](../engine/pkg/event/bus_global.go) / [bus_server.go](../engine/pkg/event/bus_server.go) / [bus_specific.go](../engine/pkg/event/bus_specific.go) / [test_mailbox_service.go](../example/comm/test_mailbox_service.go) |
| **P1-3** | execRead context 重建 | `WorkerEnv` 新增 `rwReadCtxInfo def.RWContextInfo`，初始化时缓存；`safeExecInternal` 复用而不再每条 Job 构造 | [worker.go](../engine/pkg/actor/mailbox/worker.go) / [worker_pool.go](../engine/pkg/actor/mailbox/worker_pool.go) |
| **P1-5** | mctx RWMutex 冗余 | 移除 `MiddlewareContext.mu`，`Set/Get/Reset` 全部去锁；新增并发契约注释（同一 Job 全程串行；mpsc 提供 happens-before） | [middleware_chain.go](../engine/pkg/actor/mailbox/middleware_chain.go) |
| **P1-9** | fixConf 改写入参 | 新增 `cloneMailboxConfForFix`：浅拷贝 + 深拷贝 SchedulePolicy/IdlerConf/ScalingStrategy；`fixConf` 先克隆再修改，原模板不再被污染 | [worker_pool.go](../engine/pkg/actor/mailbox/worker_pool.go) |
| **P2-1** | 优先级 fallback 静默 | `Submit` 走 fallback 时显式 `e.SetPriority(m.fallbackPriority)`，与 SuspendPolicy/限流口径对齐 | [queue_manager_priority.go](../engine/pkg/actor/mailbox/queue_manager_priority.go) |
| **P2-8** | AutoScaler 边界态无冷却 | `ShouldResize` 在所有决策路径（含 newSize==cur、边界 clamp）都更新 `lastResizeTime`，避免持续过载下策略热评估 | [scaler.go](../engine/pkg/actor/mailbox/scaler.go) |
| **ADR-2 / P1-1（2026-04-27 追加）** | dispatch 拓扑无锁化 | 1) 新增 `workersSnapshot{workers,ring,dispatchCnt,sole/soleID,count}` + `WorkerPool.snap atomic.Pointer[workersSnapshot]`；2) `WorkerPool.mu` 由 RWMutex → Mutex，**仅作扩缩容/Wait 串行化**，dispatch 全程不再触碰；3) `DispatchJob` 全部走 `snap.Load()`：单 worker 走 `sole` 快路径，多 worker 走 `ring.Get` + map 命中；统计 `dispatchCnt` 也挂在快照上随 COW 复制，热路径零 `p.mu`；4) `Start`/`resizeWorkers(grow)`/`resizeWorkers(shrink)` 三个写流程在 `mu` 内构建新 workers/ring/dispatchCnt 后通过 `publishSnapshotLocked` 一次性 `atomic.Pointer.Store` 替换；5) **shrink 顺序契约（P0-2）保留**：mu 全程持有，先选定 ids → BeginStop → Wait drain → 才 publish 移除 ids（dispatch 不被 mu 阻塞，autoScaler 与 SetRWEnabled 与 resize 互斥）；6) `BeginStop`/`Wait`/`logDispatchStatsOnce`/`autoScaleWorkers`/`GetRWMetrics` 全部切到 `snap.Load()` 读端无锁；`Wait` 在 mu 内 `snap.Store(nil)` + 释放 readPool，watchdog 关闭顺序不变。 | [worker_pool.go](../engine/pkg/actor/mailbox/worker_pool.go) |
| **P0-6（2026-04-27 追加）** | RW Disable 安全切换 | `WorkerPool.SetRWEnabled(false)` 在 `rw.Disable()` 后新增 `waitInflightReadsDone(stopTimeout)`：遍历 `snap.Load().workers`，对每 Worker 显式 `inflightReads.Wait()`，在独立 goroutine 中执行并配合 `time.Timer` 兜底超时——把 `readFunc` defer 链 `RUnlock → inflightReads.Done` 之间的瞬态窗口收口，使「`SetRWEnabled(false)` 成功返回 ⇒ 所有读 goroutine 已彻底退出（含 inflightReads 计数归零）」成为强契约。失败回滚 `Enable()` 恢复原状态，避免半切换。原有主循环 serial path `inflightReads.Wait` 兜底语义保留。 | [worker_pool.go](../engine/pkg/actor/mailbox/worker_pool.go) |
| **P2-7（2026-04-27 追加）** | pendingJob 单值字段隐式约束 | 新增 `Worker.storePending(job)`：写入前断言 `pendingJob == nil`，若被破坏（上层协议错误）记录 ERROR + 对老 Job 调用 `OnJobDiscarded(ErrMailboxWorkerClosed)` + `Release` 防泄漏；`execWrite` 两个 closed 分支（等读阶段、TryLock 阶段）统一改用 `storePending`；字段注释升级为显式不变量描述。 | [worker.go](../engine/pkg/actor/mailbox/worker.go) |
| **P1-8（2026-04-27 追加）** | DispatchKeyStats 哈希前缀偏置 | `shardFor` 由"FNV-1a 采样前 32 字节"改为 `xxhash.Sum64String` 全长哈希；解决 dispatcherKey 形如 `serviceName.serviceId.partition` 时长共享前缀导致大量 key 落到同一 shard、分段锁退化为单锁的问题。xxhash ~1GB/s 远快于 fnv，且仅在 debug 模式启用。 | [dispatch_key_stats_middleware.go](../engine/pkg/actor/mailbox/dispatch_key_stats_middleware.go) |
| **P2-12（2026-04-27 追加）** | GetEnableRWPtr 暴露 `*atomic.Bool` 破坏封装 | `MethodMgr.SetEnableRW(*atomic.Bool)` → `SetRWStateProvider(func() bool)`；调用方改为 `rwMgr.SetRWStateProvider(s.mailbox.IsRWEnabled)`；删除 `Mailbox.GetEnableRWPtr` / `WorkerPool.GetEnableRWPtr` / `RWController.EnabledPtr` 三个泄漏内部 atomic 指针的方法；handler.go 移除 `sync/atomic` import。将来 RW 状态语义扩展不再需要修改 RemoveMethods 调用链。 | [handler.go](../engine/pkg/core/rpc/handler.go) / [service.go](../engine/pkg/core/service.go) / [mailbox.go](../engine/pkg/actor/mailbox/mailbox.go) / [worker_pool.go](../engine/pkg/actor/mailbox/worker_pool.go) / [rw_controller.go](../engine/pkg/actor/mailbox/rw_controller.go) |
| **P2-11（2026-04-27 追加）** | SubmitJob `count.Add(1)` 热路径开销 | `WorkerEnv` 新增 `statsEnabled` 字段（与 `WorkerPool.statsEnabled` 同步）；`Worker.SubmitJob` 的 `w.count.Add(1)` 改为仅在 `env.statsEnabled` 时执行；`Worker.Wait` 的 `processed events` 日志同条件守卫。Release 环境每条 Job 节省 1 次 atomic.Add。 | [worker.go](../engine/pkg/actor/mailbox/worker.go) / [worker_pool.go](../engine/pkg/actor/mailbox/worker_pool.go) |
| **P1-2（2026-04-27 追加）** | watchdog per-Job timer 默认成本 | `fixConf` 取消 `MaxJobExecutionTime` 默认 30s 强制赋值；现仅在用户显式 `>0` 时启用 watchdog（`WorkerPool.Start` 已做条件初始化）。短任务高 QPS 场景每条 Job 节省 1 次时间轮 Schedule + Cancel；按需开启长任务监控。 | [worker_pool.go](../engine/pkg/actor/mailbox/worker_pool.go) |
| **P1-7（2026-04-27 追加）** | DispatchJob 每条 GetJobLen + scaleTrigger | 新增 `WorkerPool.dispatchSampleCnt atomic.Uint64` + 常量 `dispatchSampleMask=127`；`DispatchJob` 中 `EnableAutoScaling` 路径改为 `dispatchSampleCnt.Add(1)&mask==0` 才采样一次 `worker.GetJobLen()` 并尝试 `scaleTrigger`，减少 99% GetJobLen 调用与 channel select 竞争。AutoScaler 自身有 `ResizeCoolDown` ticker 兜底，扩容延迟在可接受范围。 | [worker_pool.go](../engine/pkg/actor/mailbox/worker_pool.go) |
| **P1-4（2026-04-27 追加）** | PriorityScheduler `samePriorityQueues` 切片 per-NextJob 分配 | `PriorityScheduler` 新增 `samePriorityBuf []def.Priority` 复用 buffer（`NewPriorityScheduler` 中按 `len(PriorityBatches)` 预分配）；`weightedPriorityWithOrdering` / `fairnessPriorityWithOrdering` 改用 `ps.samePriorityBuf[:0]`，省去每次 NextJob 在多优先级共存场景下的切片分配 + GC 压力。单 worker 独占 scheduler，复用安全。 | [scheduler.go](../engine/pkg/actor/mailbox/scheduler.go) |
| **P2-3（2026-04-27 追加，已并入 P2-4）** | CircuitBreaker 把"上游 Reject"算入失败窗口 | 原计划在自研 `CircuitBreakerMiddleware.OnComplete` 中识别上游 Reject 错误（`ErrSentinelBlocked` / `ErrRateLimitExceeded` / `ErrCircuitBreakerOpen`）以避免反馈环；后续 P2-4 直接删除自研 CB、统一收敛到 Sentinel circuitbreaker（基于响应错误而非中间件 Reject 计数），从根上消除该问题，本项目源文件 `circuit_breaker_middleware.go` 已随 P2-4 删除。 | （已合并至 P2-4，无独立改动） |
| **P2-2（2026-04-27 追加）** | RateLimit token 在下游 Reject 时被浪费 | `createConfigMiddlewares` 中间件链顺序调整：DispatchKeyStats → **CircuitBreaker → RateLimit**（原为 DispatchKeyStats → RateLimit → CB）。RateLimit `OnReceive` 调用 `Allow()` 即消费 token，原顺序下 CB Open 拒绝时 token 已被消耗，限流口径偏严；调整后只有"业务真正会执行"的请求才到达 RateLimit。Sentinel 中间件由用户自定义注册，建议参照同样原则放在最后。注释中明确该顺序契约。 | [middleware_factory.go](../engine/pkg/actor/mailbox/middleware_factory.go) |
| **P2-4（2026-04-27 追加）** | 自研 CircuitBreaker 与 Sentinel 重叠 | 删除自研 `CircuitBreakerMiddleware`（含 With* 选项 / 状态机 / OnComplete 失败计数）。`MailboxConf.MiddlewareConf.CircuitBreakerConf` 直接映射为 Sentinel `circuitbreaker.Rule`：`FailureThreshold` → ErrorCount 策略 `Threshold = MinRequestAmount`、`CooldownDuration` → `RetryTimeoutMs`、`WindowDuration` → `StatIntervalMs`；`SuccessThreshold/HalfOpenMaxAllowed` 由 Sentinel 半开内置策略接管。资源名 = `service.GetName()`，与同一 service 上注册的 SentinelMiddleware 流控规则共享同一资源。`CreateMiddlewaresFromConfig` 新增 `serviceName` 形参。**框架开发期不做向后兼容**：`circuit_breaker_middleware.go` 与 Deprecated 兼容别名 `circuit_breaker_compat.go`（`ErrCircuitBreakerOpen`）一并删除，所有错误判定统一改用 `ErrSentinelBlocked`。 | [middleware_factory.go](../engine/pkg/actor/mailbox/middleware_factory.go) / [service.go](../engine/pkg/core/service.go) / [middleware_bench_test.go](../engine/pkg/actor/mailbox/middleware_bench_test.go) |
| **P2-5（2026-04-27 追加）** | sentinel system rules sync.Once "先到先赢" | 删除 `sentinelSystemRulesOnce`，新增包级 `sentinelSystemRulesMu` + `sentinelSystemRulesByService map[string][]*system.Rule`。每个 SentinelMiddleware OnStart 把本 service 的 system rules 注册到全局表，再调用 `reloadSentinelSystemRulesLocked` 按 `(MetricType, Strategy, TriggerCount)` 三元组去重后整体 `system.LoadRules`；OnStop 对称 `delete + reload`。彻底消除启动顺序依赖与静默丢弃，任意 service 的 system rules 都能合并参与全局保护，并随 service 生命周期增量/减量。 | [sentinel_middleware.go](../engine/pkg/actor/mailbox/sentinel_middleware.go) |
| **P2-9（2026-04-27 追加）** | Job 资源所有权 & ref-count 复杂 | 三层重构：(1) `job.go` 顶部新增 ~50 行包级 **Ownership Contract** 文档块，明确"Job 与 payload 双 ref-count 正交"、"Release 是唯一终止入口且幂等"、"PostJob 后调用方禁持任何引用"等不变量，并附带 ASCII 流转图；(2) `job_factory.go` 抽取泛型 helper `newJobPool[T poolableJob](name, ctor)`，把原来 5 份各 ~30 行的 `pool.NewSyncPoolWrapper(... With*  ...)` 样板压缩为每池 3 行；(3) 删除 `RpcJob.Release` 中 `payload.IsRef()` debug 警告——envelope 与 Job 生命周期独立，混淆所有权语义会给业务误导信号，envelope 自身的池统计已能暴露真实泄漏。Reset 注释升级为"严禁业务调用"；poolableJob constraint 内化"对池可用"协议。文件总行数 `job.go` 196→159、`job_factory.go` 286→202。 | [job.go](../engine/pkg/actor/mailbox/job/job.go) / [job_factory.go](../engine/pkg/actor/mailbox/job/job_factory.go) |
| **P1-6（2026-04-27 追加）** | hashring + VirtualWorkerRate=24 重建成本 | 新增 `dispatch_ring.go`：实现基于 xxhash + Lamping & Veach (2014) **jump consistent hash** 的 `dispatchRing`（仅维护 `sortedIDs []int32` + `Get(key) → bucket → sortedIDs[idx]`），常量 `2862933555777941757`。`worker_pool.go` 中 `workersSnapshot.ring` 由 `*hashring.HashRing[int32]` 改为 `*dispatchRing`；`Start` / `resizeWorkers(grow/shrink)` 三处构建点统一改用 `newDispatchRing(ids)`，构建复杂度 O(N log N)（仅排序 worker id），N 为 worker 数。删除 `serestat/hashring` 依赖；`MailboxConf.VirtualWorkerRate` 字段标记 Deprecated（任何值都被忽略，保留以维持 yaml 反序列化兼容）。**额外修复**：worker.go run() defer 中"无残留 Job 时跳过 WLock 获取"——原实现未超时分支无条件 `mu.Lock()`，与其它 Worker 仍在飞行的读 goroutine 形成跨 Worker 阻塞链（per-Worker `stopTimeout` 是独立约束，不应被全局 RW 锁竞争耦合），导致 `TestRW_DrainInflightTimeout` 在 jump hash 集中 key 分布场景下闲置 Worker 等待 ~2s。修复后无残留时直接退出 drain 阶段，Stop 严格被 `stopTimeout` 边界约束。 | [dispatch_ring.go](../engine/pkg/actor/mailbox/dispatch_ring.go) / [worker_pool.go](../engine/pkg/actor/mailbox/worker_pool.go) / [worker.go](../engine/pkg/actor/mailbox/worker.go) / [define.go](../engine/pkg/config/define.go) |
| **P2-6（2026-04-27 追加）** | SysCtlJob handler 是空 stub | 新增 `engine/pkg/core/sysctl_registry.go`：包级 `SysCtlHandler func(ctx, args[]any) error` 类型 + per-Service `sysCtlRegistry`（map+RWMutex，支持运行时追加/覆盖）。`Service.initSysCtlRegistry` 在 `initJobHandlers` 之后被调用，自动注册三条内置命令：`mailbox.suspend` / `mailbox.resume` / `service.healthcheck`（均输出 INFO 日志 + 必要状态变更）。新增 `Service.RegisterSysCtl(name, handler)` 供用户扩展，同名后注册者覆盖前者（便于装饰内置命令）。新增 `Service.PostSysCtl(ctx, cmd, args...)`：构造 SysCtlJob，`Priority=PrioritySys`、`DispatcherKey="__sysctl__"`（多 worker 时所有 sysctl 命令落同一 worker 串行），直接走 `mailbox.PostJob`，绕过 ReadOnly 自投递检查。`handler_job.handleSysCtl` 替换 TODO 为注册中心 lookup + 调用：未注册命令仅 WARN，不返回错误，避免投递端因命令名拼写错误触发上层级联失败。`PrioritySys=-3 ≤ PriorityUrgent=-2`，自然穿越 SuspendPolicy / RateLimit / Sentinel 高优先级跳过分支，符合"控制面命令必须能在 mailbox 挂起时进入"的语义。 | [sysctl_registry.go](../engine/pkg/core/sysctl_registry.go) / [handler_job.go](../engine/pkg/core/handler_job.go) / [service.go](../engine/pkg/core/service.go) / [sysctl_registry_test.go](../engine/pkg/core/sysctl_registry_test.go) |
| **P2-10（2026-04-27 追加）** | writeRequested 全局计数粒度过粗 | `writeRequested atomic.Int32` 从 `RWController` 拆到 `Worker` 结构体（per-Worker）：`execWrite` 仅增减本 Worker 计数器，`launchRead` gate ① 仅检查本 Worker 计数器。原设计中任一 Worker 的写 Job 会让全局所有 Worker 的 readPipeline 让步，造成跨 Worker 读吞吐被不相关的写负载压低；拆 per-Worker 后读仅让步同 Worker 的写（同 Worker 读-写饥饿防护语义不变），跨 Worker 竞争由 `mu.RWMutex` 在锁层面串行化。**理论代价**：Worker A 的写处于“已 Add(1) 但未拿到 WLock”窗口时，Worker B 的读不再预点让步，可能略延后 A 的写调度；但 mu.WLock 本身在没有 RLock 时会抢到，不会造成饥饿（且 RLock holder 由同 Worker 的 readPipeline spawn 的读 goroutine 负责释放，生命周期 ≤ 一个读 Job 执行时长）。充足补充注释说明 per-Worker 汇总在 mu 层面串行化。 | [worker.go](../engine/pkg/actor/mailbox/worker.go) / [rw_controller.go](../engine/pkg/actor/mailbox/rw_controller.go) |
| **衍生 Bug** | watchdog goroutine 阻塞 BeginStop⇄Wait | 修复过程中发现 watchdog consumer 注册在 `p.wg`，导致 `Wait()` 卡住。改为独立 `watchdogWg`，在 `scheduler.Stop()` 之后单独 join | [worker_pool.go](../engine/pkg/actor/mailbox/worker_pool.go) |

### 7.2 暂缓项及理由

| 编号 | 暂缓原因 |
|------|----------|
| — | 本轮全部 P0/P1/P2 均已落地，无暂缓项。|

### 7.3 验证

- `go build ./...`：clean
- `go test ./... -count=1 -timeout 240s`：全绿（mailbox 4.33s / core 0.28s / event 0.23s / monitor 0.17s 等）
- 已知间歇性用例 `TestRW_DrainInflightTimeout`：原版本上 `-count=10` 也会超时 FAIL（用例末尾 leak readDuration=2s 的读 goroutine，连跑累积），与本轮修复无关，未在本轮修改；2026-04-27 P1-6 同步修复 drain WLock 跨 Worker 阻塞 bug 后 `-count=5` 全绿
- `-race` 测试：本地缺 gcc/CGO，本轮未跑；建议在 CI Linux 镜像补一轮。

### 7.4 已知遗留 / 后续动作

1. 本地 race 验证缺失 → 在 CI 增加 `CGO_ENABLED=1 go test -race ./...` 校验 P0-1 修复；
2. ADR-2 仍需作为下一阶段优先项（dispatch 热路径无锁化），本轮 P0-2 仅从“顺序契约”角度修复，dispatch 热路径仍是 RWMutex；
3. ADR-3 已落地（Phase 3 完成）；建议下一轮针对 `dispatchRead` 满时回压策略做 mailbox benchmark（当前为 ADR-4 OnJobDiscarded，按场景可考虑可选阻塞模式）；
4. 其余 P1/P2 项建议结合 mailbox benchmark 报告分批落地；
5. 间歇性用例 `TestRW_DrainInflightTimeout` 建议单独加固（末尾同步等待读 goroutine 结束，避免跨 case leak）。
6. P0-6（RW Disable 安全切换）：✅ 2026-04-27 落地，详见 7.1。`SetRWEnabled(false)` 现会的 `Disable` 后调用 `waitInflightReadsDone(stopTimeout)` 显式等待 per-Worker `inflightReads.Wait`，超时回滚 `Enable()`。

### 7.5 复核记录（2026-04-27）

- 全量交叉复核 §7.1 共 27 项（P0×6 / P1×9 / P2×12）+ ADR-1/2/3/4 + 衍生 Bug：所有声明的关键符号、函数与文件改动均已与代码状态逐一比对，结果一致。
- `go build ./...` clean；`go vet ./...` clean；`go test ./... -count=1 -timeout 240s` 全绿（28 包）。
- 文档更正：原 P2-3 行标注的源文件 `circuit_breaker_middleware.go` 已随 P2-4 删除，本节同步将该项标记为"已并入 P2-4"；同时移除 P2-4 早期保留的 Deprecated 兼容别名 `circuit_breaker_compat.go`（`ErrCircuitBreakerOpen = ErrSentinelBlocked`），框架处于开发期不维持向后兼容。
- 已知不影响本轮的遗留：`engine/pkg/actor/mailbox/job` 包中 `TestRegisterJobFactory_DuplicateAndReplace` 在 `-count≥2` 重复运行下不稳定（全局 `factoryRegistry` 未在测试间重置），属测试隔离性问题，与本轮修复无关，建议后续单独加固。

