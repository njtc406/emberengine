# Mailbox 读写分离设计方案

> 状态: **待实现** | 创建: 2026-02-07

## 背景与动机

当前 mailbox 的并发模型基于 `DispatcherKey` 一致性哈希路由：相同 Key 的 Job 串行执行，不同 Key 可分发到不同 Worker 并行执行。

**关于并发安全的澄清**：
- **单 Worker 模式**：所有消息完全串行，**真正的并发安全**，业务层无需加锁
- **多 Worker 模式**：DispatcherKey 只保证同 Key 消息到同一 Worker 串行执行。但不同 Key 的消息在不同 Worker 上并行执行时，如果业务代码访问了**服务级共享状态**（如全局 map、计数器等），那是**不安全的**，是否并发安全**完全由业务代码决定**

也就是说：多 Worker 模式下无锁并行的前提是业务状态按 Key 完全隔离。如果有跨 Key 共享状态，业务自身就需要加锁。

在此基础上的局限：

- **同一 Key 内**：所有操作（包括只读查询）仍串行排队，读多写少时浪费吞吐
- **单 Worker 模式**：完全串行，安全但效率不足
- **实际业务特征**：很多服务写入不频繁，但有大量读取需求（如用户数据查询、状态查询等）

**核心想法**：从机制上实现读写分离——读操作共享并发，写操作独占串行。在单 Worker 和 per-Key 隔离的多 Worker 场景下，业务层仍不需要关心并发。

## 现状分析

### 当前 Job 流水线

```
PostJob → 中间件链 → DispatchJob(一致性哈希 by DispatcherKey) → Worker队列 → ExecuteJob
```

### 当前并无读写区分

- `IMailboxJob` 接口没有读/写标记
- `MailboxJobType` 按来源分类（RPC/Event/Timer/Callback/SysCtl），不按读写分类
- `DispatcherKey` 路由基于实体亲和性，不考虑操作是读还是写
- Worker 内部按优先级调度，不区分读写

### 当前并发对比表

| 维度   | 当前行为       |
| ------ | -------------- |
| 写-写  | 同 Key 串行    |
| 读-读  | 同 Key 串行 ✗  |
| 读-写  | 无区分，全串行 |

## 设计目标

| 维度   | 读写分离后              |
| ------ | ----------------------- |
| 写-写  | 同 Key 串行 ✓（WLock）  |
| 读-读  | 可并发 ✓（RLock）       |
| 读-写  | 互斥 ✓（RWMutex 语义） |

**关键约束**：
- **单 Worker 模式下**：业务层完全不需要加锁，RW 模式提供读并发 + 写独占
- **多 Worker 模式下**：RW 模式在 per-Worker 粒度内提供读写分离；但跨 Key 共享状态的并发安全仍由业务自行保证（这与当前多 Worker 模式的要求一致，RW 模式不会使情况变差）
- 向后完全兼容：不启用 RW 模式时行为与当前一致

## 为什么是框架层而非业务层

1. Mailbox **本身就是框架的并发控制机制**。业务层的"不需要锁"是因为 mailbox 做了串行保证
2. 读写分离后，业务层**仍然不需要锁**——框架负责 RW 协调，业务只需要**声明**某个方法是"读"还是"写"
3. 如果放在业务层，每个服务都要自己实现 RWMutex 管理，违背了框架存在的意义

## 架构方案

### 整体架构

```
┌─────────────────────────────────────────┐
│              Mailbox (RW Mode)           │
│                                         │
│  PostJob → 中间件链 → RW Dispatcher      │
│                                         │
│  ┌────────────────┐  ┌──────────────┐  │
│  │  Reader Workers  │  │ Writer Workers│  │
│  │  (N 个, 并发)    │  │ (Y 个, 串行)  │  │
│  │                 │  │              │  │
│  │  RLock →执行→   │  │ WLock →执行→ │  │
│  │  RUnlock        │  │ Unlock       │  │
│  └────────────────┘  └──────────────┘  │
│                                         │
│  ┌─────────────────────────────────────┐│
│  │ sync.RWMutex (per-service)          ││
│  │ 读Job → RLock (可并发)              ││
│  │ 写Job → WLock (独占)               ││
│  └─────────────────────────────────────┘│
└─────────────────────────────────────────┘
```

### 核心改动点

#### 1. Job 层：新增 RW 标记

`IMailboxJob` 新增 `GetRWMode()` 方法，返回读/写标记。

```go
type RWMode int

const (
    RWModeWrite RWMode = iota // 默认：写操作（独占）
    RWModeRead                // 读操作（可共享并发）
)
```

#### 2. 方法注册层：ReadOnly 自动识别

当前 RPC 注册是**基于方法名前缀的自动反射注册**（`Rpc`/`RPC` → 对外方法，`Api`/`API` → 内部方法），
没有手动注册环节，因此不适合用 `rpc.ReadOnly()` 参数式声明。

**方案 A：前缀约定（推荐，与现有架构一致）**

新增 `RpcR`/`RPCR`/`ApiR`/`APIR` 前缀，表示只读方法。`suitableMethods()` 扫描时
检测到该前缀，自动标记对应方法的 `RWMode = RWModeRead`。

```go
// 写方法（现有前缀，默认行为）
func (s *UserService) RpcUpdateUser(ctx context.Context, uid int64, data *UserData) error
func (s *UserService) ApiReloadCache(ctx context.Context) error

// 只读方法（新增前缀，自动标记为 ReadOnly）
func (s *UserService) RpcRGetUser(ctx context.Context, uid int64) (*User, error)
func (s *UserService) RpcRGetRanking(ctx context.Context) ([]RankEntry, error)
func (s *UserService) ApiRGetStats(ctx context.Context) (*Stats, error)
```

实现改动点（`engine/pkg/core/rpc/prefix.go`）：
```go
var (
    apiPrefixIndex    = newPrefixBucketIndex([]string{"Api", "API"})
    rpcPrefixIndex    = newPrefixBucketIndex([]string{"Rpc", "RPC"})
    // 新增只读前缀索引
    apiRoPrefixIndex  = newPrefixBucketIndex([]string{"ApiR", "APIR"})
    rpcRoPrefixIndex  = newPrefixBucketIndex([]string{"RpcR", "RPCR"})
)
```

在 `suitableMethods()` 中优先匹配 `RpcR`/`ApiR`（因为它们是 `Rpc`/`Api` 的超集前缀），
匹配成功则标记该方法为 ReadOnly。

**方案 B：接口声明（补充，不改名场景）**

对于已有大量 `RpcXxx` 方法不便改名的服务，可额外实现一个接口手动声明：

```go
type IReadOnlyDeclarer interface {
    ReadOnlyMethods() []string
}

// 服务实现
func (s *UserService) ReadOnlyMethods() []string {
    return []string{"RpcGetUser", "RpcGetRanking"}
}
```

`registerMethod()` 时先检查服务是否实现了 `IReadOnlyDeclarer`，
如果实现了，用返回的列表标记对应方法。

**两种方案可共存**：前缀优先自动识别，接口声明作为手动覆盖/补充。
未标记的方法默认为"写"（WriteMode），保证向后兼容。

#### 3. Worker Pool 层：RW Dispatch 模式

新增 RW dispatch 逻辑（可以在现有 WorkerPool 中加分支，或新增 `RWWorkerPool`）：

- 读 Job → 获取 RLock → 分发到任意空闲 Reader Worker → 执行 → RUnlock
- 写 Job → 获取 WLock → 按 DispatcherKey 分发到 Writer Worker → 执行 → Unlock

#### 4. 配置层

```yaml
mailbox:
  rwMode: true       # 是否启用读写分离（默认 false）
  readerNum: 8       # 读 Worker 数量
  writerNum: 2       # 写 Worker 数量
```

## 粒度选择

### Per-Service RWMutex（推荐先做）

- 一个服务一把 `sync.RWMutex`
- 写操作阻塞整个服务的读
- 实现简单，适合"读多写少"的典型场景
- 足以覆盖绝大多数业务需求

### Per-Entity RWMutex（进阶，可后续扩展）

- 每个 `DispatcherKey` 一把锁
- 不同实体的读写完全不互相阻塞
- 更强大但实现复杂（需要管理锁的生命周期、防止锁泄漏）
- 适合实体级高并发场景

## 需要注意的细节

### 因果一致性

读请求如果在写请求之后到达，必须看到最新的写结果。使用 `sync.RWMutex` 天然保证——写持有 WLock 时读会被阻塞，写完之后的读自然看到新状态。

### 向后兼容

- 默认 `rwMode=false`，所有 Job 当作"写"处理 → 行为完全等同于当前的串行模型
- 业务不标记 RW 的旧代码零改动即可运行
- 仅当显式开启 `rwMode=true` 且 handler 标记了 `ReadOnly()` 时才启用读写分离

### DispatcherKey 与 RW 的关系

两者是正交维度，不矛盾：

- **DispatcherKey**：实体亲和性路由（同实体到同 Worker）
- **RWMode**：操作类型分流（读并发/写独占）

在 RW 模式下：
- 写操作仍遵守 DispatcherKey 路由，保证同实体写串行
- 读操作可分散到多个 Reader Worker 并行处理

## 实现步骤（建议顺序）

1. `def` 包新增 `RWMode` 常量定义
2. `IMailboxJob` 接口新增 `GetRWMode() / SetRWMode()` 方法
3. `Job[T]` 泛型结构体新增 `rwMode` 字段
4. RPC/Event handler 注册层增加 `ReadOnly()` 选项
5. 方法调用链路中为 Job 自动设置 RWMode
6. `MailboxConf` 增加 `RWMode`、`ReaderNum`、`WriterNum` 配置
7. WorkerPool 新增 RW dispatch 分支（或新建 RWWorkerPool）
8. 集成测试：验证读并发、写独占、读写互斥
9. 压测对比：RW 模式 vs 普通模式的吞吐差异

## 风险与开放问题

- **读操作的定义边界**：某些操作虽然不修改持久状态，但可能修改缓存/统计量，是否算"读"？建议严格定义：`ReadOnly` 意味着不修改服务任何可观测状态
- **锁竞争**：Per-Service 粒度下，如果写操作较频繁，RLock/WLock 竞争可能成为瓶颈。需要通过 benchmark 验证
- **定时器和回调 Job** 的读写分类：Timer/ConcurrentCallback 通常涉及状态变更，默认应为写
- **事件 Job** 的读写分类：EventBus 事件可能是读也可能是写，需要在事件注册时声明

---

## 补充分析：初始方案 Per-Service RWMutex 的局限性

> 以下是对初始方案更深入的审视，**初始方案在特定场景下存在退化和正确性风险**。

### 并发能力是否真正提升？

当前多 Worker 模式下，不同 DispatcherKey 分发到不同 Worker 并行执行。但需注意：**这种并行只在业务状态按 Key 完全隔离时才是安全的**。如果存在服务级共享状态，多 Worker 模式本身就需要业务自行加锁。

引入 Per-Service RWMutex 后的对比：

| 场景 | 当前模型 | Per-Service RW 模式 | 效果 |
|---|---|---|---|
| 同一 Key 连续 100 个读 | 串行排队 | 可并发 | **✅ 提升** |
| 不同 Key 各自读写（状态隔离） | 各 Worker 并行，业务保证安全 | 所有操作共享一把 RWMutex | **⚠️ 可能退步** |
| 不同 Key 各自读写（有共享状态） | 各 Worker 并行，但本身就不安全 | 全局 RWMutex 反而提供了某种协调 | **↔ 持平或改善** |
| 全局读多写极少 | 读也需要串行 | 读并发，写偶尔独占 | **✅ 提升** |
| 写操作较频繁 | 不同 Key 的写可并行（业务保证安全） | WLock 阻塞所有读和写 | **❌ 退步** |

**核心问题**：Per-Service RWMutex 引入了一把全局锁。对于状态按 Key 隔离的服务，这比当前模型粒度更粗；但对于有全局共享状态的服务，全局锁反而可能是正确的选择。

### 因果一致性风险

将读和写分到不同的 Worker 队列后，同一客户端的连续操作可能乱序：

```
1. Client 发起 WriteUserData(key="player_123", gold=100)  → 进入 Writer Worker 队列
2. Client 紧接着发起 ReadUserData(key="player_123")       → 进入 Reader Worker 队列
```

**当前模型**：同一 DispatcherKey → 同一 Worker → FIFO → 先写后读 → 读到 gold=100 ✓

**RW 模式**：写进 Writer 队列，读进 Reader 队列，是两条独立路径。Reader Worker 可能**先获取 RLock 执行读操作** → 读到旧数据 ✗

`sync.RWMutex` 保证读写互斥，但**不保证跨队列的 FIFO 顺序**。

---

## 补充方案（推荐）：Worker 内 RW 增强

不拆分 Reader/Writer Worker，而是在**现有 Worker 内部**引入 RWMutex 语义。

### 核心思路

```
PostJob → DispatcherKey 路由到 Worker（保持不变）
    → Worker 内部判断 RWMode：
        - Read Job:  不独占 Worker，允许多个 Read Job 并发执行（spawn goroutine + RLock）
        - Write Job: 等待所有并发 Read 完成，独占执行（WLock）
```

### 架构图

```
┌─────────────────────────────────────────────────────────────┐
│                     WorkerPool                              │
│                                                             │
│  PostJob → DispatcherKey 一致性哈希 → 选择 Worker           │
│                                                             │
│  ┌─────────────────────────────────────────────────────────┐│
│  │              Worker (RW Enhanced)                       ││
│  │                                                         ││
│  │  run() 主循环:                                          ││
│  │    NextJob() → 判断 RWMode:                             ││
│  │                                                         ││
│  │    Read:  rwMu.RLock() → spawn goroutine 执行           ││
│  │           可多个 Read 同时 in-flight                     ││
│  │                                                         ││
│  │    Write: 等待所有 in-flight Read 完成                   ││
│  │           rwMu.Lock() → 在 Worker goroutine 内执行       ││
│  │           rwMu.Unlock()                                 ││
│  │                                                         ││
│  │  ┌────────────────────────────────────┐                 ││
│  │  │ sync.RWMutex (per-Worker)          │                 ││
│  │  │ 读: RLock → goroutine → RUnlock   │                 ││
│  │  │ 写: Lock  → 串行执行  → Unlock    │                 ││
│  │  └────────────────────────────────────┘                 ││
│  └─────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────┘
```

### 优势对比

| 维度 | Per-Service RW | Worker 内 RW |
|---|---|---|
| 同 Key 读并发 | ✅ | ✅ |
| 异 Key 独立性 | ❌ 全局锁互相阻塞 | ✅ 各 Worker 独立锁 |
| 因果一致性 | ⚠️ 跨队列乱序 | ✅ FIFO 入队天然保证 |
| 实现复杂度 | 低 | 中 |
| 锁粒度 | 全局一把 | per-Worker 一把 |
| 向后兼容 | ✅ | ✅ |

**关于多 Worker 模式下的并发安全**：Worker 内 RW 方案的 RWMutex 粒度是 per-Worker，
这意味着它保护的是**同一 DispatcherKey 路由到的同一 Worker 内**的读写互斥。
对于跨 Key 的服务级共享状态，多 Worker 模式下的并发安全仍然取决于业务代码（与当前模型一致，RW 模式不会使情况变差）。对于**单 Worker 模式 + RW 增强**，
则能提供完整的服务级读写并发安全。

### Worker run() 伪代码

```go
func (w *Worker) run() {
    for !w.closed.Load() {
        job, ok := w.queueManager.NextJob()
        if !ok {
            w.idler.Idle()
            continue
        }

        if job.GetRWMode() == def.RWModeRead {
            // 读操作：spawn goroutine 并发执行
            w.rwMu.RLock()
            w.inflightReads.Add(1)
            go func() {
                defer w.rwMu.RUnlock()
                defer w.inflightReads.Add(-1)
                w.safeExec(job)
            }()
        } else {
            // 写操作：等待所有 in-flight 读完成，然后独占执行
            w.rwMu.Lock()
            w.safeExec(job)
            w.rwMu.Unlock()
        }
    }
}
```

### 实现步骤

1. `def` 包新增 `RWMode` 常量
2. `IMailboxJob` 接口新增 `GetRWMode() / SetRWMode()`
3. RPC handler 注册时声明 `ReadOnly()`
4. Worker 结构体新增 `rwMu sync.RWMutex` 和 `inflightReads atomic.Int64`
5. Worker.run() 中增加 RW 判断分支
6. `MailboxConf` 新增 `EnableRWMode bool` 开关
7. 集成测试 + 压测

---

## 业界参考：谁在做类似的事？

### Microsoft Orleans — `[ReadOnly]` 属性（✅ 最直接的先例）

Orleans 是 Microsoft 推出的分布式 Virtual Actor 框架（.NET 平台），其 Grain（等价于 Actor）默认是**单线程串行**处理请求——与当前 emberengine mailbox 的模型完全一致。

Orleans 提供了 **`[ReadOnly]` 属性标记**，这是目前业界**最接近**我们想法的框架级实现：

```csharp
public interface IMyGrain : IGrainWithIntegerKey
{
    Task<int> IncrementCount(int incrementBy);  // 写方法，串行执行

    [ReadOnly]
    Task<int> GetCount();  // 读方法，可与其他 ReadOnly 请求并发执行
}
```

行为语义：
- **未标记 `[ReadOnly]`** 的方法：独占执行，等同于当前 mailbox 串行模型
- **标记 `[ReadOnly]`** 的方法：可与其他 `[ReadOnly]` 方法**并发交错执行**，但与写方法互斥
- 执行仍然是**单线程**的，但 ReadOnly 请求可以在 await 点交错（cooperative concurrency）

**与我们方案的区别**：Orleans 的 ReadOnly 是在 `await` 点交错（协程级），不是真多线程并发。我们的方案是真正的多 goroutine 并发读，读吞吐更强，但对 handler 有**线程安全只读**的要求。

### Orleans — `[AlwaysInterleave]` 属性

更宽松的形式，标记后可以和**任何**请求并发交错，不限于 ReadOnly：

```csharp
[AlwaysInterleave]
Task GoFast();  // 可与任何方法（包括写方法）并发交错
```

这是 Orleans 为极度追求吞吐量且开发者自行管理状态一致性的场景提供的"逃生舱"。

### Akka (JVM/Scala) — 严格单线程 Actor

Akka 采用经典 Actor 模型，**不提供**方法级读写分离。每个 Actor 严格单线程处理所有消息。并发能力完全依赖 Actor 拆分——将同一实体的读请求路由到只读副本 Actor。

对于需要并发读的场景，Akka 的惯用模式是：
- 派生多个 Read-Replica Actor，共享不可变快照
- 主 Actor 负责写，广播状态变更到副本
- 这本质上是**应用层**手动读写分离，而不是框架内置

### Erlang/OTP — 严格串行 gen_server

Erlang 的 `gen_server` 严格单进程单信箱串行处理。OTP 不提供任何消息级读写标记。类似的读优化也是通过 ETS 表（进程外共享内存，并发安全读）来实现，属于业务层模式。

### Proto.Actor (Go) — 类似 Akka，严格串行

Go 平台的 Actor 框架，行为与 Akka 类似：每个 Actor 串行处理所有消息，不区分读写。

### 总结对比

| 框架 | 平台 | Actor 串行模型 | 读写分离 | 级别 |
|---|---|---|---|---|
| **Orleans** | .NET | ✅ 默认串行 | ✅ `[ReadOnly]` 属性 | **框架内置** |
| **Akka** | JVM | ✅ 严格串行 | ❌ 需手动拆 Actor | 应用层 |
| **Erlang/OTP** | BEAM | ✅ 严格串行 | ❌ 使用 ETS 等外部方案 | 应用层 |
| **Proto.Actor** | Go | ✅ 严格串行 | ❌ | 无 |
| **emberengine** | Go | ✅ 默认串行 | 🔜 **本设计方案** | **框架内置** |

**结论**：在 Actor/Mailbox 级别内置读写分离，Orleans 是最直接的先例。但 Orleans 的 ReadOnly 是协程交错（仍然单线程），我们的方案走得更远——**真正的多 goroutine 并发读**，更充分利用 Go 的多核优势。在 Go Actor 框架生态中，这将是一个差异化特性。

---

## 使用者收益综合分析

### 核心收益

**1. 读多写少服务的"免费"吞吐提升**

用户只需要把 `RpcGetUser` 改成 `RpcRGetUser`，配置里开启 `rwMode: true`，读请求就自动并发了。不需要改任何业务逻辑，不需要加锁，不需要理解并发模型。

典型受益服务：排行榜查询、用户信息查询、配置读取、状态查看——这类服务在游戏/分布式系统里**非常多**。

**2. 不用在"单线程安全"和"多线程性能"之间做痛苦选择**

当前用户面临的选择：
- 单 Worker：完全安全，但读请求也得排队，QPS 上不去
- 多 Worker（不同 Key）：Key 间并发了，但同 Key 的读仍然串行

RW 模式提供了第三条路：**同 Key 读并发 + 写独占**，用户不再需要纠结。

**3. 框架层保证，业务层零心智负担**

用户不需要知道 `sync.RWMutex` 是什么。只需知道：
- 加了 `RpcR` 前缀 → "我保证这个方法不改状态"
- 框架自动处理并发安全

对比用户自己做：每个服务自己管 RWMutex、每个读方法记得加 RLock、忘了就出 bug。框架做这件事的价值就是**业务层零成本**。

### 适用范围（非银弹）

| 服务特征 | 收益 |
|---|---|
| 读 90%、写 10% | **显著提升** |
| 读写各半 | 有提升，写频繁时 WLock 等待变多 |
| 写为主 | **几乎无收益**，写仍然是独占串行 |
| 纯计算无状态 | 不需要，多 Worker 已经够了 |

**不是所有服务都需要开启**。RW 模式是一个**可选增强**，不是默认模式。

### 使用者需要额外承担的决策

用户需要判断方法是"读"还是"写"。大多数情况很明确（查询 vs 修改），但存在边界 case：
- 查询同时更新了"最后访问时间" → 算写
- 查询触发了缓存预热 → 严格说改了状态，算写

**标错的风险是不对称的**：
- 把写标成读 → **⚠️ 并发安全问题**，可能数据竞争
- 把读标成写 → ✅ 无害，只是没享受到并发收益

因此默认"不标 = 写"是安全的兜底策略。

### 与"不做这个特性"的对比

如果不提供框架级 RW 分离，用户在读多写少场景下的替代方案：

| 替代方案 | 可行性 | 问题 |
|---|---|---|
| 加更多 Worker | 只解决异 Key 并发，同 Key 读仍串行 | 根本没解决 |
| 用户自己加 RWMutex | 可以 | 重复劳动 + 容易出错 |
| 拆服务：读服务 + 写服务 | 架构膨胀 | 运维成本翻倍，状态同步复杂 |
| 用缓存/Redis 分流读 | 引入外部依赖 | 一致性窗口、网络开销 |

替代方案要么**没解决问题**要么**成本更高**。框架内置 RW 分离是**性价比最高**的方案。

### 接入成本 vs 收益

| 维度 | 评价 |
|---|---|
| 接入成本 | **极低**（改前缀 + 开配置，或实现 `IReadOnlyDeclarer` 接口） |
| 使用者收益 | 特定场景（读多写少）显著，通用场景适中 |
| 风险 | 标错读写有安全隐患，但默认安全（不标 = 写） |
| 适用面 | 读多写少服务（游戏里非常常见） |
| 框架差异化 | Go Actor 生态里独有，对标 Orleans `[ReadOnly]` |

### 一句话定位

> **RW 模式是一个"可选的、低成本的、框架级并发增强"——它不改变现有模型，但给读多写少的服务提供了一条不用改业务逻辑就能提升吞吐的路径。**

## 框架定位与差异化分析

### EmberEngine 的核心定位

EmberEngine 不是 Actor 框架，不是游戏网关，也不是微服务框架。它是一个**服务容器（Service Container）**——核心抽象是 Service，框架统一管理所有 Service 的通信方式和部署方式。

### Go 生态中的框架定位对比

| 框架 | 定位 | 核心抽象 | Stars |
|---|---|---|---|
| Proto.Actor | Actor 系统 | Actor/PID | 5.4k |
| Ergo | Erlang OTP 复刻 | Process/Application | 4.4k |
| Nano / Pitaya | 游戏网关框架 | Session + Handler | 3.2k / 2.7k |
| Hollywood | 轻量 Actor 引擎 | Receiver/PID | 2.2k |
| go-kratos / go-zero | 微服务框架 | HTTP/gRPC Service | - |
| Cherry | Actor 游戏服务器 | Actor + NATS | - |
| Goakt | Actor/Grain (类 Orleans) | Actor | - |
| **EmberEngine** | **服务容器** | **Service** | - |

### 为什么"服务容器"在 Go 生态中是空缺的

**Actor 框架**让用户写 Actor，关心的是：消息传递、Actor 生命周期、Supervision 树。用户需要理解 Actor 模型。

**微服务框架**让用户写 Controller/Handler，关心的是：HTTP/gRPC endpoint、服务注册发现、API 网关。用户需要理解微服务架构。

**EmberEngine 作为服务容器**让用户只写 Service 业务逻辑，框架自动接管：
- **通信方式**：本地调用 vs 远程 RPC，对 Service 完全透明
- **部署方式**：同节点 vs 跨节点，自动路由，无需业务感知
- **并发模型**：Mailbox Worker 池全框架托管，Service 不关心线程安全
- **RPC 注册**：反射前缀扫描自动完成，无需手动注册

这更接近于：
- **Erlang/OTP 的 Application** 概念（一组 Process 的容器，而非单个 Process）
- **Orleans 的 Silo**（Service/Grain 的宿主容器）
- **Service Fabric** 的理念（服务作为部署和管理单元）

但在 Go 生态里，**没有人在做这件事**。Ergo 最接近（有 Application 的概念），但它本质在复刻 Erlang/OTP，不是面向 Go 服务场景设计的。

### RW 分离在"服务容器"定位下的意义

在 Actor 框架的语境下，RW 分离是"锦上添花"的优化。

但在**服务容器**的语境下，它是**并发治理能力的自然延伸**：

> 服务容器的核心价值主张是**"你只管写业务逻辑，通信/部署/并发全归框架"**。
>
> RW 分离是在并发这条线上的进一步承诺：你不仅不用管怎么通信、怎么部署，你甚至不用管读写并发——只要给方法加个 `RpcR` 前缀，框架自动帮你做读并行、写串行。

这与服务容器的叙事完全一致，不是硬加的特性，是自然延伸。

### EmberEngine 真正的差异化

不是某个单一特性，而是一套针对"服务容器"定位的完整能力组合：

```
Service 为一等公民
  ├── 通信透明化：本地/远程自动路由
  ├── 部署透明化：单节点/集群自动切换
  └── 并发精细化管理
       ├── 多 Worker 池 + 一致性哈希路由
       ├── MPSC 无锁队列 + 多策略优先级调度
       ├── Onion 中间件链
       ├── 动态 Worker 伸缩（AutoScaler）
       └── (未来) 框架级 RW 读写分离
```

这套组合在 Go 生态中没有第二个。其他框架要么走极简 Actor 路线（Hollywood、go-actor），要么照搬 Erlang（Ergo），要么专注游戏网关（Nano/Pitaya），要么是通用微服务（kratos/go-zero）。没有人在"服务容器 + Worker 级精细化调度"这个方向上做文章。

### 核心叙事

> **其他框架让你"用 Actor/Handler/Controller 写代码"，EmberEngine 让你"只写 Service 业务逻辑"——通信方式（本地/远程）、部署方式（单节点/集群）、并发模型（串行/读写分离）全部由框架透明管理。**
