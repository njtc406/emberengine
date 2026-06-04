# Mailbox 读写分离设计方案（Mailbox 级 RW 增强）

> 状态: **待实现** | 创建: 2026-02-07 | 最后更新: 2026-02-13

---

## 1. 背景与动机

### 1.1 当前并发模型

当前 mailbox 的并发模型基于 `DispatcherKey` 一致性哈希路由：相同 Key 的 Job 串行执行，不同 Key 可分发到不同 Worker 并行执行。

**并发安全特征**：

| 模式 | 行为 | 并发安全 |
|---|---|---|
| **单 Worker** | 所有消息完全串行 | ✅ 真正的并发安全，业务层无需加锁 |
| **多 Worker** | 同 Key 串行、异 Key 并行 | ⚠️ 取决于业务：状态按 Key 隔离则安全，有共享状态则需自行加锁 |

**核心流水线**：

```
PostJob → SuspendPolicy → Middleware Chain → DispatchJob(HashRing by DispatcherKey) → Worker Queue → ExecuteJob
```

### 1.2 现有局限

- **同一 Key 内**：所有操作（包括只读查询）串行排队，读多写少时浪费吞吐
- **单 Worker 模式**：完全串行，安全但效率不足
- **无读写区分**：
  - `IMailboxJob` 接口没有读/写标记
  - `MailboxJobType` 按来源分类（Rpc/Event/Timer/Callback/SysCtl），不按读写分类
  - `DispatcherKey` 路由基于实体亲和性，不考虑操作是读还是写
  - Worker 内部按优先级调度，不区分读写

### 1.3 业务特征驱动

典型服务（用户数据查询、排行榜查询、配置读取、状态查看）**写入不频繁，但有大量读取需求**。这类读多写少的服务在游戏/分布式系统中非常普遍。

### 1.4 核心想法

从框架层面实现 **Mailbox 级读写分离**：在不改变 DispatcherKey 路由和队列 FIFO 语义的前提下，在 WorkerPool 层面引入共享的 `sync.RWMutex`，让所有 Worker 的读操作可跨 Worker 并发执行，写操作跨全 Service 独占串行。

---

## 2. 设计目标

### 2.1 并发语义对比

| 维度 | 当前模型 | RW 增强后 |
|---|---|---|
| 写-写 | 同 Key 串行、异 Key 并行 | **全 Service 串行（Mailbox 级 WLock）** |
| 读-读 | 同 Key 串行 ✗ | **全 Service 可并发 ✓（跨 Worker、跨 Key，RLock）** |
| 读-写 | 无区分，全串行 | **全 Service 互斥 ✓（Mailbox 级 RWMutex）** |

> **❗ 写序列化影响警告**：`写-写` 从「异 Key 并行」退化为「全 Service 串行」，意味着多 Worker
> 的写吞吐退化为 1 个 Worker 水平。需确保写 QPS 在可接受范围内，详见 §10.8 的量化分析。

### 2.2 关键约束

1. **单 Worker + RW**：业务层完全不需要加锁，RW 提供读并发 + 写独占
2. **多 Worker + RW**：Mailbox 级 RWMutex 保证写操作跨全 Service 独占，读操作跨所有 Worker 并发。Service 级共享状态（Module 树、MethodMgr 等）的并发安全由框架保证，业务层无需加锁
3. **向后完全兼容**：`EnableRWMode=false`（默认）时行为与当前完全一致
4. **因果一致性**：同 Key 的读写在同一 Worker 队列中 FIFO 入队，不存在跨队列乱序

### 2.3 为什么是框架层而非业务层

1. Mailbox **本身就是框架并发控制机制**。业务层的"不需要锁"是因为 mailbox 做了串行保证
2. 读写分离后，业务层**仍然不需要锁**——框架负责 RW 协调，业务只需声明方法是"读"还是"写"
3. 如果放在业务层，每个服务自己实现 RWMutex 管理 → 重复劳动 + 容易出错 + 违背框架存在的意义

---

## 3. 架构设计

### 3.1 方案选型：为什么选择"Mailbox 级 RW 增强"

在设计过程中对比了两种方案：

| 方案 | 思路 | 核心问题 |
|---|---|---|
| ❌ Per-Service RW（独立 Reader/Writer Pool） | 拆分读写到不同 Worker 队列 + 全局 RWMutex | 因果一致性风险（跨队列乱序）；全局锁阻塞异 Key 独立性 |
| ✅ **Mailbox 级 RW 增强（本方案）** | 保持 DispatcherKey 路由不变，在 WorkerPool 层引入共享 RWMutex | FIFO 保证因果一致性；WLock 跨全 Service 独占，保护共享状态 |

**详细对比**：

| 维度 | Per-Service RW（独立 Reader/Writer Pool） | Mailbox 级 RW（本方案） |
|---|---|---|
| 同 Key 读并发 | ✅ | ✅ |
| 跨 Key 读并发 | ✅ | ✅ 所有 Worker 的 Read 共享 RLock |
| 异 Key 写并行 | ❌ 全局锁阻塞 | ❌ 全 Service WLock 串行（可接受的 trade-off） |
| 因果一致性 | ⚠️ 跨队列乱序风险 | ✅ FIFO 确保顺序 |
| Service 级共享状态安全 | ✅ | ✅ WLock 保证全 Service 独占 |
| 实现侵入性 | 高（需新增 Reader/Writer Pool） | 低（仅改 Worker.run() + WorkerPool 加共享锁） |
| 锁粒度 | 全局一把 | Mailbox/WorkerPool 级一把 |
| 向后兼容 | ✅ | ✅ |

**Per-Service RW 的因果一致性风险示例**：

```
1. Client → WriteUserData(key="player_123", gold=100)  → Writer Worker 队列
2. Client → ReadUserData(key="player_123")              → Reader Worker 队列
```

在 Per-Service 方案中，读写分到不同队列，Reader Worker 可能先获取 RLock 执行 → 读到旧数据。
而 Mailbox 级 RW 方案中，两个 Job 进入同一 Worker 的同一队列，FIFO 保证先写后读。

### 3.2 整体架构

```
┌──────────────────────────────────────────────────────────────────┐
│                         Mailbox                                  │
│                                                                  │
│  PostJob → SuspendPolicy → Middleware Chain → WorkerPool         │
│                                                                  │
│  ┌────────────────────────────────────────────────────────────┐  │
│  │           WorkerPool (共享 RW 状态)                        │  │
│  │                                                            │  │
│  │  ┌────────────────────────────────────────────────────┐  │  │
│  │  │ sync.RWMutex        (全 Service 共享，所有 Worker 引用)  │  │  │
│  │  │ writeRequested      (原子计数器，防止写饥饿)         │  │  │
│  │  │ readSem (chan)      (全 Service 读并发上限)          │  │  │
│  │  └────────────────────────────────────────────────────┘  │  │
│  │                                                            │  │
│  │  DispatchJob → HashRing(DispatcherKey) → 选中 Worker       │  │
│  │                                                            │  │
│  │  ┌──────────────────────────┐ ┌─────────────────────────┐  │  │
│  │  │ Worker A                  │ │ Worker B                 │  │  │
│  │  │ Queue: [R][R][W]...       │ │ Queue: [R][W][R]...      │  │  │
│  │  │ inflightReads (per-W WG) │ │ inflightReads (per-W WG)│  │  │
│  │  │ run() → 引用共享 rwMu    │ │ run() → 引用共享 rwMu   │  │  │
│  │  │                          │ │                         │  │  │
│  │  │ Read  → pool.rwMu.RLock  │ │ Read → pool.rwMu.RLock  │  │  │
│  │  │ Write → pool.rwMu.Lock   │ │ Write→ pool.rwMu.Lock   │  │  │
│  │  └──────────────────────────┘ └─────────────────────────┘  │  │
│  └────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────┘
```

> **为什么锁在 Mailbox/WorkerPool 级而非 per-Worker 级？**
>
> 当前模型中，handler 在 Worker 内串行执行，可以安全访问 Service 级共享状态
> （Module 树、MethodMgr、事件处理器等）而无需加锁。RW 模式下 Read goroutine 并发执行，
> 如果锁在 per-Worker 级，则：
> - Worker A 的 Read goroutine 读 `children` map
> - Worker B 的 Write handler 调用 `ReleaseModule()` 删除 `children` 条目
> - **map 并发读写 → fatal**
>
> per-Worker WLock 只保护本 Worker 的串行性，无法阻止其他 Worker 的 Read goroutine。
> 因此锁必须提升到 Mailbox/WorkerPool 级，写操作跨全 Service 独占。
>
> **Trade-off**：多 Worker 场景下，异 Key 的写操作从并行退化为串行（N → 1）。
> 但对于本方案的主要目标场景（读多写少），写频率低、影响可忽略；
> 而读并发反而从 per-Worker 扩展为跨全部 Worker 并发，吐吐量可能更高。
> 对于单 Worker 场景（最常见、最安全），无任何差异。
> **写序列化量化分析见 §10.8**。
>
> **ℹ️ 跨 Worker RLock 阻塞（已知耦合）**：
>
> RW 模式下，一个 Worker 的写操作持有 WLock 期间，**其他所有 Worker 的读路径主循环**
> 会阻塞在 `rwMu.RLock()` 上。这打破了原有模型的关键性质：「不同 Worker 独立执行」。
>
> 具体影响：
> - Worker A 持有 WLock 执行写操作（耗时 T_w）
> - Worker B/C/D 的主循环阻塞在 RLock 上，无法出队后续 Job（包括高优先级 Job）
> - 阻塞时间 = 写操作执行耗时 T_w（通常毫秒级）
>
> **缓解措施**：
> - 写操作应尽可能快（毫秒级），避免慢 IO
> - 单 Worker + RW 场景无此问题（仅一个 Worker，无跨 Worker 阻塞）
> - 读多写少场景影响极小（写频率低，阻塞窗口稀疏）
> - 若写延迟敏感，可考虑拆分服务或保持异 Key 并行的纯串行模式（不开 RW）

### 3.3 执行时序：读批次与写独占交替

```
时间轴 →
Queue:  [R1] [R2] [R3] [W1] [R4] [R5] [W2]

执行:
  R1 ──────→  (goroutine, RLock)
  R2 ────────→  (goroutine, RLock, 与 R1 并发)
  R3 ──────────→  (goroutine, RLock, 与 R1/R2 并发)
               │ W1 等待 R1/R2/R3 全部 RUnlock
               └───── W1 ─→  (主 goroutine, WLock, 独占)
                          │ R4/R5 可开始
                          └── R4 ──→  (goroutine, RLock)
                              R5 ────→  (goroutine, RLock, 与 R4 并发)
                                    │ W2 等待 R4/R5 全部 RUnlock
                                    └── W2 ─→  (独占)
```

**关键语义**：
- 连续的 Read Job 被 spawn 为并发 goroutine（多个 RLock 共存）
- 遇到 Write Job 时，主循环调用 `rwMu.Lock()` 自然等待所有 in-flight Read goroutine 完成 `RUnlock()`
- Write 独占执行完毕后，后续 Read 可继续并发
- `sync.RWMutex` 天然保证读写互斥和 happens-before 内存可见性

---

## 4. Job 层改动

### 4.1 RWMode 常量定义

新增于 `engine/pkg/def/mailbox.go`：

```go
// RWMode 读写模式标记
type RWMode int32

const (
    // RWModeWrite 写操作（默认值）——独占执行，与其他任何操作互斥
    RWModeWrite RWMode = iota
    // RWModeRead 读操作——可与其他 Read 操作并发执行，但与 Write 互斥
    RWModeRead
)
```

零值 `RWModeWrite` 是刻意设计：**未标记的 Job 默认为写操作**，保证向后兼容和安全兜底。

### 4.2 RW 模式接口设计

> **⚠️ 接口兼容性说明**：与 §5.3 对 `IMethodMgr` 的处理一致，**不修改 `IMailboxJob` 接口**。
> 在 Go 中向已有 exported 接口添加方法是 Breaking Change，所有已有实现都必须补充新方法
> 否则编译失败。虽然当前 `Job[T]` 是唯一的内部实现，但 `IMailboxJob` 是 exported 接口，
> 不能排除外部使用者实现了它。
>
> 因此采用**新增独立接口 `IRWModeJob` + 类型断言**的方式，完全不修改 `IMailboxJob`：

在 `engine/pkg/interfaces/IMailBox.go` 中新增**独立接口**：

```go
// IMailboxJob 保持不变（零修改）
type IMailboxJob interface {
    // ... 所有现有方法完全不变 ...
}

// IRWModeJob 新增独立接口，Job[T] 同时实现 IMailboxJob 和 IRWModeJob
// 调用方通过类型断言按需使用，不强制所有 IMailboxJob 实现者补充方法
type IRWModeJob interface {
    // SetRWMode 设置读写模式
    SetRWMode(mode def.RWMode)
    // GetRWMode 获取读写模式（零值 RWModeWrite 保证未设置时默认为写）
    GetRWMode() def.RWMode
}
```

调用方通过类型断言安全使用：

```go
// 在 buildRpcJob() 中设置 RWMode
if rwJob, ok := inf.IMailboxJob(j).(inf.IRWModeJob); ok {
    if roMgr, ok := methodMgr.(inf.IReadOnlyMethodMgr); ok && roMgr.IsReadOnly(envelope.GetMethod()) {
        rwJob.SetRWMode(def.RWModeRead)
    }
}

// 在 Worker.execWithRW() 中读取 RWMode（兜底函数）
func getRWMode(job inf.IMailboxJob) def.RWMode {
    if rwJob, ok := job.(inf.IRWModeJob); ok {
        return rwJob.GetRWMode()
    }
    return def.RWModeWrite // 未实现 IRWModeJob 的 Job 默认为写
}
```

`Job[T]` 已实现 `IMailboxJob`，现在同时实现 `IRWModeJob`——
对已有代码零影响，未实现 `IRWModeJob` 的自定义 `IMailboxJob` 也不会编译失败，
且 `getRWMode()` 兜底返回 `RWModeWrite`，保证安全。

### 4.3 Job[T] 泛型结构体新增字段

在 `engine/pkg/actor/mailbox/job/job.go` 的 `Job[T]` 中新增 `rwMode` 字段，
使 `Job[T]` 同时满足 `IMailboxJob`（原有）和 `IRWModeJob`（新增）两个接口：

```go
type Job[T any] struct {
    dto.DataRef
    Type          def.MailboxJobType
    Priority      def.Priority
    DispatcherKey string
    payload       T
    rwMode        def.RWMode  // 新增：读写模式标记（实现 IRWModeJob 接口）

    ctx      context.Context
    deadline int64
    mctx     inf.IMiddlewareContext
}

// SetRWMode 实现 IRWModeJob 接口
func (j *Job[T]) SetRWMode(mode def.RWMode) {
    j.rwMode = mode
}

// GetRWMode 实现 IRWModeJob 接口
func (j *Job[T]) GetRWMode() def.RWMode {
    return j.rwMode
}

func (j *Job[T]) Reset() {
    j.Type = def.MailboxJobTypeNone
    j.Priority = def.PriorityNormal
    j.DispatcherKey = ""
    j.rwMode = def.RWModeWrite  // 重置时回归默认写模式
    var zero T
    j.payload = zero
    // 【必须完整清零所有字段】防止 sync.Pool 回收后带脏状态。
    // 如果 EscalateFailure 内部 panic（二级 recover 后继续），Job 的 ctx/deadline/mctx
    // 可能处于部分修改状态，Release() 会把脏 Job 放回 Pool。
    // 完整 Reset 确保下次取出时是干净的。
    j.ctx = nil
    j.deadline = 0
    j.mctx = nil
}
```

### 4.4 各 Job 类型的默认 RWMode

| Job 类型 | 默认 RWMode | 理由 |
|---|---|---|
| `RpcJob`（请求） | 由方法注册层决定 | RPC **请求**方法可能是读也可能是写，取决于前缀/声明 |
| `RpcJob`（响应/异步回调） | `RWModeWrite` | 异步回调涉及状态变更（如设置回调结果），必须独占 |
| `EventBusJob` | `RWModeWrite` | 事件处理涉及状态变更 |
| `TimerJob` | `RWModeWrite` | 定时器回调涉及状态变更 |
| `ConcurrentCallbackJob` | `RWModeWrite` | 并发回调涉及状态变更 |
| `SysCtlJob` | `RWModeWrite` | 系统控制命令必须独占 |

> **⚠️ 核心原则：只有 RPC 请求（Request）可以被标记为 Read，所有其他 Job 类型一律为 Write。**
> RPC 响应（Response）中的异步 Call 回调、Timer 回调、ConcurrentCallback 均涉及状态操作，
> 绝不允许标记为 Read。框架在 `Service.PostJob` 注入 RWMode 时会严格校验 Job 类型
> 和请求/响应方向，只对 RPC Request 查询方法的 `ReadOnly` 属性（见 §5.4）。

> **EventBusJob 的未来扩展**：可在事件订阅注册时声明 `ReadOnly`（如 `eventBus.Subscribe(EventTypeXxx, handler, event.ReadOnly())`），当前阶段全部按 Write 处理，后续按需扩展。
>
> **☸️ EventBus ReadOnly 扩展的额外复杂性**：与 RPC 不同，一个事件可能有**多个订阅者**（handler），
> 其中部分只读、部分涉及写操作。事件的 ReadOnly 粒度应为“该事件的**所有**订阅处理器
> 均为 ReadOnly”时才能标记为 Read；只要有一个处理器涉及写操作，整个 EventBusJob 必须为 Write。
> 这使得 EventBus 的 ReadOnly 扩展比 RPC 复杂得多，不应简单移植 RPC 的 ReadOnly 逻辑。
> 建议未来实现时在事件派发层聚合所有 handler 的 ReadOnly 声明，全部为 ReadOnly 时才标记事件 Job 为 Read。
>
> **ℹ️ EventBus ReadOnly 的 finalize 阶段设计**：事件订阅是动态的（不同 Module 在启动阶段逐个注册），
> Module A 注册时 Module B 可能还未注册它的 handler，无法在注册时判断最终聚合结果。
> 因此需要在 Service 启动完成后（所有 Module 初始化完毕后）增加一个 **finalize 阶段**：
> 遍历所有事件类型的全部订阅者，聚合决定每个事件的 RWMode。当前阶段不实现此 finalize，
> 留作未来扩展点。
>
> **生命周期钩子设计（OnModulesReady）**：当前 Service 启动流程为
> `Init → OnInit → modules.Init → Start → OnStart`，缺少“所有模块就绪后、服务进入 Running 前”的钩子。
> 建议在 Service 生命周期中新增 `OnModulesReady()` 回调，在所有 Module 初始化完成后、
> Service 进入 Running 状态之前调用。此钩子不仅服务于 EventBus ReadOnly finalize，
> 还可用于其他需要“全部模块就绪后执行”的逻辑（如跨模块依赖校验、预热缓存等）。
>
> ```
> Service 启动流程（修改后）：
> Init → OnInit → modules.Init → **OnModulesReady** → Start → OnStart
>                                      │
>                                      ├─ EventBus finalize（聚合事件 RWMode）
>                                      ├─ 跨模块依赖校验
>                                      └─ 其他 post-init 逻辑
> ```

---

## 5. 方法注册层改动

### 5.1 ReadOnly 声明机制

当前 RPC 注册基于**方法名前缀的自动反射扫描**（`Rpc`/`RPC` → 对外方法，`Api`/`API` → 内部方法），没有手动注册环节。提供两种互补的 ReadOnly 声明方式：

#### 方式 A：前缀约定（推荐，与现有架构一致）

新增 `RpcRo`/`RPCRo`/`ApiRo`/`APIRo` 前缀，表示只读方法。

> **⚠️ 前缀命名安全性说明**：前缀选择 `RpcRo`（ReadOnly 缩写）而非 `RpcR`，
> 因为 `RpcR` 会与大量以 R 开头的现有方法名冲突（如 `RpcReload`、`RpcRemove`、
> `RpcReset`、`RpcRegister` 等），导致这些**写方法被静默误标为 ReadOnly**，
> 产生难以排查的并发数据竞争。`RpcRo` 在常见英文方法命名中几乎不会出现
> 自然冲突（没有以 "Ro" 开头的常见动词），安全性大幅提升。

**使用示例**：

```go
// ===== 写方法（现有前缀，默认行为） =====
func (s *UserService) RpcUpdateUser(ctx context.Context, uid int64, data *UserData) error
func (s *UserService) ApiReloadCache(ctx context.Context) error

// ===== 只读方法（新增前缀，自动标记为 ReadOnly） =====
func (s *UserService) RpcRoGetUser(ctx context.Context, uid int64) (*User, error)
func (s *UserService) RpcRoGetRanking(ctx context.Context) ([]RankEntry, error)
func (s *UserService) ApiRoGetStats(ctx context.Context) (*Stats, error)
```

**实现改动**（`engine/pkg/core/rpc/prefix.go`）：

```go
var (
    apiPrefixIndex   = newPrefixBucketIndex([]string{"Api", "API"})
    rpcPrefixIndex   = newPrefixBucketIndex([]string{"Rpc", "RPC"})
    // 新增只读前缀索引（使用 Ro 后缀而非单字母 R，避免与 RpcReload 等方法名冲突）
    apiRoPrefixIndex = newPrefixBucketIndex([]string{"ApiRo", "APIRo"})
    rpcRoPrefixIndex = newPrefixBucketIndex([]string{"RpcRo", "RPCRo"})
)

// SetApiReadOnlyPrefix 设置自定义只读 API 前缀
func SetApiReadOnlyPrefix(prefix ...string) {
    apiRoPrefixIndex.add(prefix...)
}

// SetRpcReadOnlyPrefix 设置自定义只读 RPC 前缀
func SetRpcReadOnlyPrefix(prefix ...string) {
    rpcRoPrefixIndex.add(prefix...)
}

func hasApiReadOnlyPrefix(s string) bool {
    return apiRoPrefixIndex.has(s)
}

func hasRpcReadOnlyPrefix(s string) bool {
    return rpcRoPrefixIndex.has(s)
}
```

**匹配优先级**：`suitableMethods()` 中必须**先匹配只读前缀**再匹配普通前缀，因为 `RpcRo` 是 `Rpc` 的超集（`"RpcRoGetUser"` 同时匹配 `Rpc` 和 `RpcRo`）：

```go
func (h *Handler) suitableMethods(method reflect.Method) error {
    name := method.Name
    isReadOnly := false

    // 先检查只读前缀（优先级更高，因为 RpcRo 是 Rpc 的超集）
    if hasRpcReadOnlyPrefix(name) || hasApiReadOnlyPrefix(name) {
        isReadOnly = true
    } else if !hasApiPrefix(name) && !hasRpcPrefix(name) {
        return nil // 不是任何已知前缀，跳过
    }

    // ... 现有参数校验/签名检查逻辑不变 ...

    // 注册方法（原签名不变）+ 标记只读
    h.mgr.AddMethodFunc(name, compiledFunc)
    if isReadOnly {
        h.mgr.MarkReadOnly(name)
    }

    // ...
}
```

#### 方式 B：接口声明（补充，适用于不改名场景）

对于已有大量 `RpcXxx` 方法不便改名的服务，可实现 `IReadOnlyDeclarer` 接口手动声明：

```go
// IReadOnlyDeclarer 只读方法声明接口（可选实现）
// 定义于 engine/pkg/interfaces/
type IReadOnlyDeclarer interface {
    // ReadOnlyMethods 返回只读方法名列表
    // 列表中的方法名应与注册的方法名完全一致（含前缀）
    ReadOnlyMethods() []string
}

// 服务实现示例
func (s *UserService) ReadOnlyMethods() []string {
    return []string{"RpcGetUser", "RpcGetRanking", "ApiGetStats"}
}
```

**两种方式可共存**，解析优先级：

1. 前缀匹配 `RpcRo`/`ApiRo` → 标记 ReadOnly ✅
2. 服务实现 `IReadOnlyDeclarer` → 对应方法名标记 ReadOnly ✅
3. 均未匹配 → 默认 Write（保证向后兼容）

**声明有效性校验**：`IReadOnlyDeclarer` 只能覆盖已注册的 RPC/API 方法。
如果 `ReadOnlyMethods()` 返回的方法名不存在，或该方法没有通过 `suitableMethods()` 注册到方法表，
框架输出 WARN 并跳过该声明，避免把拼写错误或非 RPC/API 方法误标为只读。

| 场景 | 前缀判断 | IReadOnlyDeclarer | 最终结果 | 日志 |
|---|---|---|---|---|
| `RpcRoGetUser` + `ReadOnlyMethods` 中也列出 | ReadOnly | ReadOnly | ReadOnly | 无（一致） |
| `RpcRoGetUser` + `ReadOnlyMethods` 中未列出 | ReadOnly | 无声明 | ReadOnly | 无（前缀优先） |
| `RpcGetUser` + `ReadOnlyMethods` 中列出 | Write | ReadOnly | ReadOnly | 无（显式覆盖） |
| `RpcMissing` + `ReadOnlyMethods` 中列出 | 未注册 | ReadOnly | 忽略声明 | WARN |

实现方式（在 `suitableMethods` 扫描完成后、`IReadOnlyDeclarer` 标记阶段）：

```go
// 扫描完所有方法后，检查模块是否实现 IReadOnlyDeclarer，补充手动声明
if declarer, ok := module.(inf.IReadOnlyDeclarer); ok {
    if roMgr, ok := h.mgr.(inf.IReadOnlyMethodMgr); ok {
        for _, name := range declarer.ReadOnlyMethods() {
            if _, registered := h.mgr.GetMethodFunc(name); !registered {
                h.Warnf("Method '%s' is declared as ReadOnly by IReadOnlyDeclarer "+
                    "but is not registered as an RPC/API method", name)
                continue
            }
            roMgr.MarkReadOnly(name)
        }
    }
}
```

#### 方式 B 的后续收敛方向（建议）

在服务可见性已经从方法前缀语义中解耦后，`IReadOnlyDeclarer` 保留的主要价值，
不再是“兼容旧的服务级推断”，而是**为 RW 调度提前提取方法级元信息**。因此建议把这块职责收敛为：

1. **启动期一次性提取方法级元信息**
    - 识别该方法是否属于可注册 RPC/API 方法；
    - 预编译调用闭包；
    - 确定 `readOnly` 标记；
    - 将结果写入静态方法表，供运行期无锁读取。

2. **`IReadOnlyDeclarer` 仅作为显式覆盖源**
    - 适用于已有大量 `RpcXxx` / `ApiXxx` 方法不便改名；
    - 只负责声明“这些已注册方法应按只读调度”；
    - 不再承担任何服务级可见性或集群发布语义。

3. **校验重点从“前缀风格冲突”切换到“声明有效性”**
    - 比起“`RpcXxx` 看起来像写方法但被声明为只读”的风格告警，
      更值得优先检查的是：
    - `ReadOnlyMethods()` 返回的方法是否真实存在；
    - 该方法是否已被注册为 RPC/API 方法；
    - 若声明的方法不存在或未注册，输出 `WARN`，必要时可升级为启动失败。

4. **移除 `rpcCnt` / `MethodMgr.IsPrivate()` 的历史语义**
    - 在服务可见性完全由 `visibility=node|cluster` 决定后，
      `MethodMgr.IsPrivate()` 这个命名容易继续误导读者，把“方法名前缀统计”和“服务私有性”混为一谈；
    - 当前框架不再需要根据 RPC 方法数量推断服务可见性，`rpcCnt` 没有保留价值；
    - 已删除 `rpcCnt` 字段、计数维护逻辑和 `MethodMgr.IsPrivate()` 方法，避免保留历史包袱。

推荐的目标形态如下：

```go
type MethodMeta struct {
     Name      string
     Kind      MethodKind // Rpc / Api
     ReadOnly  bool
     Invoker   def.MethodCallFunc
}
```

其中：

- `Handler.registerMethod()` 负责反射扫描并一次性构建 `MethodMeta`；
- `MethodMgr` 只保存静态方法表，不再重复分散做前缀语义判断；
- `Service.setJobRWMode()` 只依赖 `ReadOnly` 元信息，不关心方法名推断细节。

按这个方向收敛后，`handler.go` 中扫描逻辑的意义会更清晰：
它不是在“猜服务怎么暴露”，而是在**启动阶段编译接口元数据，给运行期调度和派发使用**。

建议的最小落地顺序：

1. 保留 `IReadOnlyDeclarer`，但把当前“写前缀 + 只读声明”的风格告警降级为次要检查；
2. 新增“声明的方法不存在 / 未注册”校验；
3. 抽取统一的 `MethodMeta` 或等价分析结果，减少 `HasRpcPrefix/HasApiPrefix/...` 在多个位置重复判断；
4. 已直接移除 `rpcCnt` / `MethodMgr.IsPrivate()`，并同步清理 `IMethodMgr` 中的遗留接口。

### 5.2 MethodMgr 扩展

> **ℹ️ 静态表说明**：`MethodMgr` 的方法注册表（`methods`）是**启动阶段构建的静态表**。
> 框架不支持运行期动态增删 module，所有方法在服务启动时一次性注册完成。
> 运行期 `GetMethodFunc()`/`IsReadOnly()` 仅做并发读，`methods` 不会被写入。
> 因此读路径无需加锁；启动期写入和 shutdown 阶段 `RemoveMethods()` 由 `mu` 保护。

新增 `methodEntry` 结构体，将方法的可执行函数和 ReadOnly 标记合并存储，
运行时单次 map 查找即可获取全部信息：

```go
// methodEntry 方法注册条目（包含函数和只读标记）
type methodEntry struct {
    fn       def.MethodCallFunc
    readOnly bool
}

type MethodMgr struct {
    mu          sync.RWMutex
    methods     map[string]*methodEntry
    index       inf.INodeMethodIndex
    isRWEnabled func() bool // 查询 RW 模式是否启用，封装 atomic 细节
    logger      log.ILoggerX
}

func NewMethodMgr(logger log.ILoggerX, index inf.INodeMethodIndex) inf.IMethodMgr {
    if index == nil {
        index = NewMethodIndex()
    }
    return &MethodMgr{
        methods: make(map[string]*methodEntry),
        index:     index,
        logger:    logger,
    }
}

func (m *MethodMgr) SetRWStateProvider(provider func() bool) {
    m.isRWEnabled = provider
}

// AddMethodFunc 注册方法（保持原签名兼容 IMethodMgr，默认 readOnly=false）
// 仅在启动阶段调用，运行期 methods 为只读
func (m *MethodMgr) AddMethodFunc(name string, fn def.MethodCallFunc) {
    m.AddMethod(name, fn, false)
}

// AddMethod 注册方法并指定 readOnly 标记（仅启动阶段调用）
func (m *MethodMgr) AddMethod(name string, fn def.MethodCallFunc, readOnly bool) {
    if name == "" {
        m.logger.Debugf("method[%s] register failed", name)
        return
    }
    m.mu.Lock()
    defer m.mu.Unlock()
    m.methods[name] = &methodEntry{fn: fn, readOnly: readOnly}
}

// GetMethodFunc 获取方法函数（兼容原有 IMethodMgr 接口）
// 运行期并发读安全（methods 为启动后只读的静态表）
func (m *MethodMgr) GetMethodFunc(name string) (def.MethodCallFunc, bool) {
    entry, ok := m.methods[name]
    if !ok {
        return nil, false
    }
    return entry.fn, true
}

// IsReadOnly 查询方法是否为只读（无锁，单次 map 查找）
// 运行期并发读安全（methods 为启动后只读的静态表）
func (m *MethodMgr) IsReadOnly(name string) bool {
    entry, ok := m.methods[name]
    return ok && entry.readOnly
}

// MarkReadOnly 标记指定方法为只读（启动阶段使用，供 IReadOnlyDeclarer 批量设置）
func (m *MethodMgr) MarkReadOnly(name string) {
    m.mu.Lock()
    defer m.mu.Unlock()
    if entry, ok := m.methods[name]; ok {
        entry.readOnly = true
    }
}

// RemoveMethods 移除方法（仅在服务关闭阶段、模块卸载时调用）
// 设计约束：此方法仅在 Service 已关闭对外接口后的 shutdown 阶段调用（模块 Release 流程），
// 此时所有 Worker 已停止，不存在并发读 methods 的 goroutine。
// 防御性校验：RW 模式下额外检查调用时机，防止误在运行期调用导致 map 并发读写 fatal。
func (m *MethodMgr) RemoveMethods(names []string) {
    // 【防御性校验】RW 模式下，运行期 GetMethodFunc()/IsReadOnly() 并发读 methods，
    // 如果此时 RemoveMethods 写入 map → map concurrent read/write fatal。
    // 正常调用时机是 shutdown 阶段（Worker 已停止），此校验防止误用。
    if m.isRWEnabled != nil && m.isRWEnabled() {
        m.logger.Errorf("RemoveMethods called while RW mode is active! "+
            "This may cause data race. Caller should ensure all Workers are stopped. names=%v", names)
        return
    }

    m.mu.Lock()
    defer m.mu.Unlock()
    for _, name := range names {
        if _, ok := m.methods[name]; !ok {
            continue
        }
        delete(m.methods, name)
    }
}
```

### 5.3 IMethodMgr 接口与 ReadOnly 查询

在 `engine/pkg/interfaces/` 中新增**独立接口** `IReadOnlyMethodMgr`。

> **⚠️ 接口兼容性说明**：在 Go 中向已有接口（`IMethodMgr`）添加方法是 **Breaking Change**
> ——所有已有实现都必须补充新方法否则编译失败。虽然当前 `MethodMgr` 是唯一的内部实现，
> 但 `IMethodMgr` 是 exported 接口，不能排除外部使用者实现了它。
>
> 因此采用**新增独立接口 + 类型断言**的方式，完全不修改 `IMethodMgr`：

```go
// IMethodMgr 删除服务可见性推断遗留接口，只保留方法表职责
type IMethodMgr interface {
    AddMethodFunc(name string, fn def.MethodCallFunc)
    GetMethodFunc(name string) (def.MethodCallFunc, bool)
    RemoveMethods(names []string)
}

// IReadOnlyMethodMgr 新增独立接口，MethodMgr 同时实现两者
// 调用方通过类型断言按需使用，不强制所有 IMethodMgr 实现者补充方法
type IReadOnlyMethodMgr interface {
    MarkReadOnly(name string)      // 标记指定方法为只读（启动阶段调用）
    IsReadOnly(name string) bool   // 查询方法是否为只读（无锁，运行期只读）
}
```

`MethodMgr` 同时实现 `IMethodMgr` 和 `IReadOnlyMethodMgr`。
`IsReadOnly()` 直接在 `methods` 中查找 `methodEntry.readOnly` 字段，
与 `GetMethodFunc()` 共享同一个底层 map。
`methods` 在启动阶段一次性构建完成，运行期为只读静态表，
因此 `GetMethodFunc()`/`IsReadOnly()` 的并发读操作天然安全，无需加锁，零开销。

调用方通过类型断言安全使用：

```go
// 在 suitableMethods() 中注册方法时直接带上 readOnly 标记
// 前缀匹配在扫描阶段即可确定 readOnly
if isReadOnly {
    h.mgr.AddMethod(name, compiledFunc, true)  // 新增 AddMethod 支持 readOnly 参数
} else {
    h.mgr.AddMethodFunc(name, compiledFunc)    // 原签名不变，默认 readOnly=false
}

// 扫描完所有方法后，检查模块是否实现 IReadOnlyDeclarer，补充手动声明
if declarer, ok := module.(inf.IReadOnlyDeclarer); ok {
    if roMgr, ok := h.mgr.(inf.IReadOnlyMethodMgr); ok {
        for _, name := range declarer.ReadOnlyMethods() {
            roMgr.MarkReadOnly(name)
        }
    }
}
```

```go
// 在 Service.PostJob 中查询只读状态设置 RWMode（详见 §5.4）
if roMgr, ok := s.methodMgr.(inf.IReadOnlyMethodMgr); ok {
    if roMgr.IsReadOnly(methodName) {
        rwJob.SetRWMode(def.RWModeRead)
    }
}
```

对已有代码零影响，未实现 `IReadOnlyMethodMgr` 的自定义 `IMethodMgr` 也不会编译失败。

### 5.4 RPC 调用链路中设置 RWMode

RWMode 的注入时机：**在 `Service.PostJob` 中**，即 Job 进入 mailbox 之前。
此处 Service 拥有 `methodMgr` 的引用，可直接查询方法的 ReadOnly 状态。

> **为什么选择 `Service.PostJob` 而非 Job 构造处（`sender_local.go`）？**
> RPC Job 的构造分散在多个位置（如 `rpc/client/sender_local.go` 的 `DeliverRequest` 和
> `DeliverResponse`），这些位置没有 `MethodMgr` 的引用。而 `Service.PostJob` 是所有 Job
> 进入 mailbox 的统一入口，且 Service 拥有 `methodMgr`——是最自然的注入点。

```go
func (s *Service) PostJob(job inf.IMailboxJob) error {
    // RW 模式下，为 RPC 请求 Job 设置 RWMode
    if s.mailbox.IsRWEnabled() {
        s.setJobRWMode(job)
    }
    return s.mailbox.PostJob(job)
}

func (s *Service) setJobRWMode(job inf.IMailboxJob) {
    // 只有 RPC 请求才可能是 Read，其他所有 Job 类型一律为 Write（§4.4）
    if job.GetType() != def.MailboxJobTypeRpc {
        return
    }

    rwJob, ok := job.(inf.IRWModeJob)
    if !ok {
        return
    }

    // 从 RPC Job 中提取 envelope，获取方法名
    envelope := job.GetJobPayloadAs[inf.IEnvelope](job)
    if envelope == nil {
        return
    }

    // 只有请求（非回复）才检查 ReadOnly
    data := envelope.GetData()
    if data == nil || data.IsReply() {
        return // 响应/异步回调 → 始终为 Write（§4.4）
    }

    // 查询方法是否为只读（通过 IReadOnlyMethodMgr 类型断言）
    if roMgr, ok := s.methodMgr.(inf.IReadOnlyMethodMgr); ok {
        if roMgr.IsReadOnly(data.GetMethod()) {
            rwJob.SetRWMode(def.RWModeRead)
        }
    }
    // 未匹配时 Job 零值为 RWModeWrite
}
```

> **`GetJobPayloadAs` 的类型安全**：`GetJobPayloadAs[inf.IEnvelope](job)` 需要 Job
> 的底层类型为 `*job.RpcJob`（即 `*Job[inf.IEnvelope]`），这由 `job.GetType() == MailboxJobTypeRpc`
> 的前置检查保证。如果 Job 类型不匹配，`GetJobPayload` 返回 nil，由上方 nil 检查兜底。

> **Mailbox 需新增 `IsRWEnabled()` 方法**：返回 `conf.EnableRWMode`，避免在非 RW 模式下
> 执行不必要的类型断言和 map 查找。这是一个简单的布尔读取，无性能开销。

> **为什么不在 ExecuteJob 阶段设置？** 因为 Worker 需要在取出 Job **之后、执行之前**就知道是 Read 还是 Write，以决定走 RLock 还是 WLock 分支。如果是在 `ExecuteJob()` 内部才确定 RWMode，就无法提前获取正确的锁。

---

## 6. Worker 层改动（核心）

### 6.1 WorkerPool 新增共享 RW 状态

RW 相关的锁、信号量、WaitGroup 存放在 `WorkerPool` 上，所有 Worker 通过 `w.pool` 引用：

```go
type WorkerPool struct {
    // ... 现有字段保持不变 ...

    // ---- RW 增强字段（Mailbox 级共享） ----
    enableRW       atomic.Bool    // 是否启用 RW 模式（atomic：支持运行时动态开关，详见 §10.14）
    rwMu           sync.RWMutex   // 全 Service 共享读写锁，所有 Worker 引用
    writeRequested atomic.Int32   // 正在等待写锁的 Writer 计数，读路径检查 >0 时让步避免写饥饿（详见 §6.4）
    readSem        chan struct{}  // 全 Service 读并发信号量（nil = 不限制）
    stopTimeout    time.Duration  // Stop 时等待 in-flight 读 goroutine 的最大时间
}
```

**`rwMu` 为什么不放在 Worker 上**：见 §3.2 架构图中的设计决策说明。per-Worker 锁无法保护
`Module.children`、`Module.rootContains` 等 Service 级共享状态的并发安全。
`inflightReads` 则放在 per-Worker 级别（详见 §6.1.1），避免跨 Worker 阻塞。

### 6.1.1 Worker 结构体

Worker 持有 per-Worker 的 `inflightReads` 用于跟踪本 Worker spawn 的读 goroutine，
其余 RW 字段通过 `pool` 指针引用 WorkerPool 共享：

```go
type Worker struct {
    workerId      int
    closed        atomic.Bool
    closing       atomic.Bool
    submitters    atomic.Int64
    pool          *WorkerPool    // 通过 pool 访问共享的 rwMu/readSem/writeRequested
    wg            sync.WaitGroup
    inflightReads sync.WaitGroup  // per-Worker：仅跟踪本 Worker spawn 的读 goroutine
    pendingJob    inf.IMailboxJob  // 单值字段（非队列）：TryLock/读路径轮询检测到 closed=true 时暂存已出队未执行 Job
                                   // 设计为单值而非队列：因为 closed 检查后立即 return，主循环不会再取下一个 Job，
                                   // 因此最多只有一个 Job 被暂存。defer Drain 阶段会优先处理 pendingJob
                                   // 然后通过 DrainAll 处理队列中剩余的 Job。
    queueManager  IQueueManager
    idler         *idle.AdaptiveController
    count         atomic.Int64
    drainPolicy   DrainPolicy
}
```

**关于 `inflightReads` 选型说明**：
- 使用 `sync.WaitGroup` 而非 `atomic.Int64`，因为 `WaitGroup.Wait()` 天然提供阻塞等待所有计数归零的语义
- `inflightReads` 放在 **per-Worker 级别**而非 WorkerPool 级别，每个 Worker 仅跟踪自己 spawn 的读 goroutine
- **为什么不放在 WorkerPool 级别**：
  - 如果 `inflightReads` 是全部 Worker 共享的，Worker A 的 `inflightReads.Wait()` 会等待 Worker B 的读 goroutine 完成
  - 在 `resizeWorkers` 缩容场景下，被停止的 Worker 会阻塞在其他仍在运行的 Worker 的读 goroutine 上，导致不必要的延迟
  - `StopTimeout` 会被误触发——本 Worker 无泄漏读 goroutine，却因其他 Worker 的读未完成而超时，导致 Drain 被错误降级为 DrainDiscard
- `rwMu` 仍在 WorkerPool 级别——Drain 阶段获取 `pool.rwMu.Lock()` 时，`Lock()` 会等待所有 Worker 的 RLock 释放，保证 Drain 与所有读 goroutine 互斥

**关于 `writeRequested` 计数器说明**：
- `sync.RWMutex.TryLock()` 不会在 RWMutex 内部注册 pending writer（等待中的写者），这与 `Lock()` 不同
- `Lock()` 被调用后，后续的 `RLock()` 会排队等待（Go runtime 内部设置 `readerWait`），自然防止写饥饿
- 但 `TryLock()` 失败即返回，不注册任何等待信号，连续的 `RLock()` 调用可以无限期阻止 `TryLock()` 成功（写饥饿）
- `writeRequested` 是应用层的补偿机制，类型为 `atomic.Int32` **计数器**而非 `Bool` 标志。写路径在 TryLock 轮询前 `Add(1)`，退出时 `Add(-1)`；读路径在统一轮询循环中检查 `Load() > 0` 并**自旋等待**（非单次 Gosched），确保已持有 RLock 的读 goroutine 完成后不再有新的 RLock 进入，从而让 TryLock 在有限时间内成功
- **为什么是计数器而非 Bool**：多 Worker 场景下，可能同时有多个 Writer 在等待 WLock。如果使用 Bool，Writer A 获取 WLock 后执行 `Store(false)` 会清除标志，但 Writer B 仍在等待——此时读路径不再让步，导致 Writer B 饥饿。使用计数器确保只要有任意 Writer 在等待，读路径就会让步

### 6.2 WorkerPool RW 初始化

WorkerPool 创建时初始化共享 RW 状态，所有 Worker 通过 `w.pool` 引用：

```go
func NewWorkerPool(conf *config.MailboxConf, invoker inf.IMailboxInvoker, /* ... */) *WorkerPool {
    pool := &WorkerPool{
        // ... 现有字段初始化保持不变 ...

        // ---- RW 共享状态初始化 ----
        stopTimeout: conf.StopTimeout,
    }
    pool.enableRW.Store(conf.EnableRWMode)  // atomic.Bool 初始化

    // 全 Service 读并发限制信号量
    if conf.EnableRWMode && conf.MaxConcurrentReads > 0 {
        pool.readSem = make(chan struct{}, conf.MaxConcurrentReads)
    }
    if pool.enableRW.Load() && pool.stopTimeout <= 0 {
        pool.stopTimeout = 10 * time.Second
    }

    // 创建 Workers（每个 Worker 引用同一个 pool）
    for i := 0; i < conf.SchedulePolicy.InitialWorkerNum; i++ {
        pool.workers[i] = newWorker(i, conf, pool)
    }

    return pool
}
```

### 6.2.1 newWorker 初始化（简化）

Worker 仅持有 per-Worker 的 `inflightReads` 和 `pendingJob`，其余 RW 字段通过 `pool` 引用共享：

```go
func newWorker(workerId int, conf *config.MailboxConf, pool *WorkerPool) inf.IMailboxWorker {
    w := &Worker{
        workerId: workerId,
        pool:     pool,
        drainPolicy: func() DrainPolicy {
            if pool != nil {
                return pool.drainPolicy
            }
            return DrainExecute
        }(),
    }

    w.queueManager = createQueueManager(conf)

    // ... 空闲控制器初始化（保持不变）...

    return w
}
```

### 6.3 Worker.run() 核心逻辑

```go
func (w *Worker) run() {
    defer w.wg.Done()

    // 退出时：先等待本 Worker 的 in-flight 读完成（带超时兜底），再在 WLock 下 drain 残留消息
    defer func() {
        // ① 等待本 Worker 的 in-flight 读 goroutine 完成（带超时保护）
        stopTimedOut := false
        if w.pool.enableRW.Load() {
            done := make(chan struct{})
            go func() {
                w.inflightReads.Wait() // per-Worker：仅等待本 Worker spawn 的读 goroutine
                close(done)
            }()
            select {
            case <-done:
                // 本 Worker 的所有读 goroutine 正常完成
            case <-time.After(w.pool.stopTimeout):
                // 超时：标记不安全关闭，强制继续
                stopTimedOut = true
                w.pool.logger.Errorf("Worker %d: StopTimeout (%v) exceeded, "+
                    "read goroutines still in-flight. "+
                    "Drain forced to DrainDiscard to avoid data race with leaked goroutines.",
                    w.workerId, w.pool.stopTimeout)
            }
        }

        if w.queueManager == nil {
            return
        }

        // 【P0 修复：多 Worker Drain 的并发写竞争】
        // 多 Worker 场景下，所有 Worker 的 run() goroutine 的 defer 块近乎同时执行。
        // 如果 Drain 阶段不持有 rwMu.Lock()，多个 Worker 同时执行 safeExec 处理
        // 残留 Job（写操作），会导致并发写入服务共享状态——数据竞争。
        // 因此 DrainExecute 必须在 pool.rwMu.Lock() 保护下执行，确保同一时刻只有一个
        // Worker 在 Drain，且与其他 Worker 的读 goroutine 互斥。
        //
        // 【P1 修复：StopTimeout 后 Drain 的数据竞争】
        // 如果超时后仍有泄漏的读 goroutine 在后台执行 ExecuteJob（访问服务共享状态），
        // 此时 Drain 用 safeExec 执行写操作会与泄漏的读 goroutine 产生数据竞争。
        // 因此超时后自动降级为 DrainDiscard，丢弃残留消息但保证不引入竞争。
        effectiveDrainPolicy := w.drainPolicy
        if stopTimedOut {
            effectiveDrainPolicy = DrainDiscard
        }

        switch effectiveDrainPolicy {
        case DrainDiscard:
            // discardExec 语义：不执行业务逻辑，仅触发中间件 OnComplete
            // （传入 ErrMailboxNotRunning）并释放 Job 资源（Release）。
            // DrainDiscard 不需要 WLock（不执行业务逻辑，不访问服务共享状态）。
            //
            // 优先处理 TryLock 退出时暂存的已出队 Job（§6.4 write path closed 退出场景）
            if w.pendingJob != nil {
                w.discardExec(w.pendingJob)
                w.pendingJob = nil
            }
            w.queueManager.DrainAll(func(e inf.IMailboxJob) {
                w.discardExec(e)
            })
        default:
            // ② Drain 阶段获取 WLock，保证跨 Worker Drain 串行 + 与读 goroutine 互斥。
            // rwMu.Lock() 会等待所有 Worker 的 in-flight 读 goroutine RUnlock 后才返回。
            // 注意：这里使用 Lock()（非 TryLock），因为 defer 阶段主循环已退出，
            // 无需检查 closed 标志。如果其他 Worker 的读 goroutine 泄漏导致 Lock
            // 长时间阻塞，上层 WorkerPool.Wait() 的超时机制负责兜底。
            w.pool.rwMu.Lock()
            // 优先处理 TryLock 退出时暂存的已出队 Job（§6.4 write path closed 退出场景）
            if w.pendingJob != nil {
                w.safeExec(w.pendingJob)
                w.pendingJob = nil
            }
            w.queueManager.DrainAll(func(e inf.IMailboxJob) {
                w.safeExec(e)
            })
            w.pool.rwMu.Unlock()
        }
    }()

    // 主处理循环
    for !w.closed.Load() {
        e, ok := w.queueManager.NextJob()
        if !ok {
            w.idler.Idle()
            continue
        }

        if w.pool.enableRW.Load() {
            w.execWithRW(e)
        } else {
            w.safeExec(e) // 未启用 RW，保持原有串行行为
        }
    }
}
```

### 6.4 execWithRW：读写分离核心方法

```go
// execWithRW 根据 Job 的 RWMode 执行读写分离逻辑
func (w *Worker) execWithRW(job inf.IMailboxJob) {
    if getRWMode(job) == def.RWModeRead {
        w.execRead(job)
    } else {
        w.execWrite(job)
    }
}

// execRead 读操作：统一轮询获取前置条件后 spawn goroutine 并发执行
func (w *Worker) execRead(job inf.IMailboxJob) {
        // ---- 读操作：spawn goroutine 并发执行 ----

        // 【统一轮询循环】
        // 主循环在获取 RLock 之前，通过一个统一的轮询循环完成三项前置检查：
        // ① closed 检查（响应 Stop）；② writeRequested 自旋让步（防止写饥饿）；
        // ③ 信号量令牌获取（MaxConcurrentReads 硬上限）。
        //
        // 【写饥饿防护（修正）】
        // 原方案使用单次 Gosched() 让步，但 Gosched 仅让出一个调度时间片（纳秒级），
        // 之后读者照常获取 RLock。在持续高 QPS 读流量下，不断有新读者通过 Gosched
        // 后立刻 RLock，writer 的 TryLock 永远看到活跃的 RLock → 写饥饿未解决。
        // 修正方案：当 writeRequested > 0 时，读路径**自旋等待**直到计数归零，
        // 确保已持有 RLock 的读 goroutine 完成后不再有新的 RLock 进入。
        //
        // 【信号量位置选择（修正）】
        // 原方案将信号量放在 goroutine 内部用非阻塞 select+default 获取，令牌满时
        // 降级为无令牌执行。这导致 MaxConcurrentReads 实际上是「无上限」——所有
        // goroutine 无论是否获取令牌都会执行并持有 RLock，写路径必须等待全部 RUnlock。
        //
        // 修正方案：信号量令牌在**主循环中**获取（在 RLock 之前），使
        // MaxConcurrentReads 成为同时持有 RLock 数量的**硬上限**。
        // 代价是主循环可能在令牌已满时短暂阻塞（队头阻塞），但由于：
        // ① writeRequested 阻止新读者进入令牌竞争，已有读者会自然排空
        // ② 令牌等待时间有上界（≤ 单次读操作耗时）
        // ③ 循环中每次检查 closed，保证 Stop 安全
        // 实际影响可控，远优于「MaxConcurrentReads 无效」的原方案。
        // 【读路径退避策略】
        // 与写路径一致，读路径在 writeRequested > 0 或信号量满时也使用指数退避，
        // 避免多 Worker 主循环纯 CPU 自旋浪费资源。初始 Gosched（零延迟让步），
        // 然后从 1μs 开始倍增，上限 100μs（读路径退避上限小于写路径的 1ms，
        // 因为读让步的目的是尽快恢复——writeRequested 归零或信号量释放后应立即进入）。
        readBackoff := time.Duration(0)
        const maxReadBackoff = 100 * time.Microsecond
        for {
            // ① 优先检查 Stop 状态
            if w.closed.Load() {
                w.pendingJob = job
                return
            }
            // ② 若有 pending writer，退避让步（让已有的读 goroutine 完成 RUnlock）
            if w.pool.writeRequested.Load() > 0 {
                if readBackoff == 0 {
                    runtime.Gosched()
                    readBackoff = time.Microsecond
                } else {
                    // 加入随机 jitter，避免 writeRequested 归零时多 Worker 同时唤醒的“惊群效应”。
                    // jitter 范围与 Worker 数成正比：Worker 越多，同时唤醒的竞争越激烈，
                    // 需要更大的分散空间。基础范围 ±20%，乘以 min(workerCount, 8) 的缩放因子。
                    wcFactor := int64(w.pool.workerCount.Load())
                    if wcFactor < 1 { wcFactor = 1 }
                    if wcFactor > 8 { wcFactor = 8 } // 缩放因子上限，避免 jitter 过大
                    jitterBase := int64(readBackoff) * 2 / 5             // 40% of readBackoff
                    jitterRange := jitterBase * wcFactor                  // 放大 jitter 范围
                    if jitterRange > 0 {
                        jitter := time.Duration(rand.Int63n(jitterRange)) - time.Duration(jitterRange/2)
                        time.Sleep(readBackoff + jitter)
                    } else {
                        time.Sleep(readBackoff)
                    }
                    readBackoff *= 2
                    if readBackoff > maxReadBackoff {
                        readBackoff = maxReadBackoff
                    }
                }
                continue
            }
            // ③ 信号量令牌获取（非阻塞尝试，失败则退避后重新进入循环）
            if w.pool.readSem != nil {
                select {
                case w.pool.readSem <- struct{}{}:
                    // 获取令牌成功
                default:
                    // 令牌已满，退避后重试（同时重新检查 closed 和 writeRequested）
                    if readBackoff == 0 {
                        runtime.Gosched()
                        readBackoff = time.Microsecond
                    } else {
                        time.Sleep(readBackoff)
                        readBackoff *= 2
                        if readBackoff > maxReadBackoff {
                            readBackoff = maxReadBackoff
                        }
                    }
                    continue
                }
            }
            break
        }

        // 【RLock 安全性说明】
        // RLock 在信号量和 writeRequested 检查**之后**获取，因此：
        // 1. MaxConcurrentReads 限制了同时持有 RLock 的 goroutine 数量（硬上限）
        // 2. writeRequested > 0 时不会有新的 RLock 进入，保证写路径 TryLock 能成功
        // 3. RLock 可能被其他 Worker 的 WLock 短暂阻塞（跨 Worker 耦合，见 §3.2），
        //    阻塞时间等于写操作执行耗时，通常为毫秒级
        w.pool.rwMu.RLock()

        // 【关键：RLock-after-check 重检查（§10.14 动态开关安全协议）】
        // 防止在 SetRWEnabled(false) 切换窗口内误 spawn 读 goroutine：
        // 时序：WLock 翻转 enableRW=false → Unlock → 本 Worker RLock 成功 → 此处检查
        // 如果 enableRW 已关闭，降级为串行执行，避免数据竞争。
        if !w.pool.enableRW.Load() {
            w.pool.rwMu.RUnlock()
            // 【必须】释放轮询循环中已获取的信号量令牌，否则令牌泄漏。
            // 每次动态开关触发此路径都会泄漏一个令牌，最终 MaxConcurrentReads
            // 个令牌耗尽后读路径永久阻塞在轮询循环的信号量获取步骤。
            if w.pool.readSem != nil {
                <-w.pool.readSem
            }
            w.safeExec(job) // 降级为串行执行
            return
        }

        // 【关键时序】inflightReads.Add 必须在 spawn goroutine 之前、
        // 在主循环 goroutine 中同步执行，且在 RLock + enableRW 重检查之后。
        // 确保 Stop 路径中 inflightReads.Wait() 不会在 Add 之前返回。
        // 放在 RLock 之后而非之前：避免 enableRW 重检查降级路径需要额外 Done()。
        w.inflightReads.Add(1) // per-Worker：仅跟踪本 Worker spawn 的读 goroutine

        go func() {
            // 【读 goroutine 可观测性说明】
            // 读 goroutine 的 panic 恢复和日志输出由 safeExecInternal 内部处理。
            // 框架 Logger 已在创建时预置了 serviceName、serviceUID 等上下文信息，
            // 因此 safeExecInternal 中的日志输出（包括 panic stack、error 信息）
            // 天然包含 Service 标识，无需在 goroutine 层额外注入。
            // Worker ID 信息可通过 w.workerId 在日志中附带。
            //
            // 备选方案：如需更精细的 goroutine 级别分析（如通过 pprof goroutine dump
            // 定位泄漏的读 goroutine），可使用 pprof.Do() 为 goroutine 打标签：
            //   pprof.Do(ctx, pprof.Labels("worker", strconv.Itoa(w.workerId),
            //       "method", getJobMethodName(job)), func(_ context.Context) { ... })
            // 当前阶段暂不引入，待生产环境确认需要后再启用。

            // 【WaitGroup + 锁 + 信号量泄漏防护（加固版）】
            // 三项资源释放合并到单个 defer func 中。
            // RUnlock 使用 atomic.Bool 保证最多调用一次（double-unlock 触发 runtime.throw 不可 recover）。
            // 信号量归还使用 recover 防御（channel 操作的 panic 可 recover）。
            var rlockReleased atomic.Bool
            defer func() {
                // 归还信号量令牌（最先释放，优先级最低）
                if w.pool.readSem != nil {
                    func() {
                        defer func() { recover() }() // 防御性 recover
                        <-w.pool.readSem
                    }()
                }
                // 释放读锁（必须在 Done 之前，保证 WLock 等待者看到正确的 RLock 计数）
                // 注意：不使用 recover 包装 RUnlock。在 Go 中，RWMutex.RUnlock()
                // 在计数已归零时调用会触发 runtime.throw（fatal，不可 recover），
                // 因此 recover 提供的是虚假安全感。用 atomic 标志确保最多调用一次。
                if rlockReleased.CompareAndSwap(false, true) {
                    w.pool.rwMu.RUnlock()
                }
                // WaitGroup Done（最后释放，确保 inflightReads.Wait 在锁释放后才返回）
                w.inflightReads.Done()
            }()

            w.safeExecSkipProfiler(job) // 读 goroutine 跳过共享 Profiler
        }()
    } else {
        // ---- 写操作：等待所有 in-flight 读完成，然后独占执行 ----
        //
        // 【实现说明】写路径已拆分为 execWrite() 方法（见上方 execRead/execWrite 拆分），
        // 以下为写路径的详细实现逻辑（属于 execWrite 方法体）：
        //
        // 【为什么使用 TryLock 而非 Lock】
        // 如果直接使用 rwMu.Lock()，当读 goroutine 泄漏（永不返回）时，
        // Lock() 永久阻塞 → 主循环无法退出 → run() 的 defer 不执行 →
        // StopTimeout 兜底机制失效 → Worker.Stop() 永远不返回。
        //
        // TryLock + closed 检查轮询允许主循环在等待写锁期间检查 closed 标志，
        // 保证 BeginStop() 能在有限时间内让主循环退出，然后由 defer 中的
        // w.inflightReads.Wait() + StopTimeout 处理泄漏的读 goroutine。
        //
        // Go 1.18+ 提供 sync.RWMutex.TryLock()，当前项目使用 Go 1.24，安全可用。
        //
        // 【写饥饿防护】
        // TryLock 不会在 RWMutex 内部注册 pending writer。如果不做额外处理，
        // 读路径持续不断地成功 RLock 会导致 TryLock 永远无法成功（写饥饿）。
        // 解决方案：在 TryLock 轮询前递增 writeRequested（Int32 计数器），通知读路径让步。
        // 已有读 goroutine 的 RUnlock 不受影响，但新的读操作（在同一或其他 Worker）
        // 会在统一轮询循环中检查 writeRequested > 0 并自旋等待（见上方读路径），
        // 使 in-flight 读 goroutine 自然排空后 TryLock 即可成功。
        //
        // 【指数退避说明】
        // 使用指数退避替代纯 runtime.Gosched()，避免在 RLock 持有时间较长
        // 的场景下浪费 CPU。初始 Gosched（~0 延迟），然后从 1μs 开始倍增，
        // 上限 1ms（读 goroutine 通常毫秒级完成，1ms 足够平衡响应速度和 CPU 开销）。
        w.pool.writeRequested.Add(1)
        backoff := time.Duration(0) // 首次使用 Gosched（零延迟让步）
        const maxBackoff = 1 * time.Millisecond
        for !w.pool.rwMu.TryLock() {
            if w.closed.Load() {
                w.pool.writeRequested.Add(-1)
                // Worker 正在停止但无法获取写锁（其他 Worker 的读 goroutine 可能泄漏）
                // 此 Job 已由 NextJob() 出队，暂存到 pendingJob，由 Drain 阶段处理
                w.pendingJob = job
                return
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
        // 【关键：使用 defer 保护资源释放】
        // rwMu.Unlock() 和 writeRequested.Add(-1) 必须通过 defer 保护，
        // 避免 safeExec 行为变化（如未来引入 runtime.Goexit、log.Fatal 等）
        // 导致 WLock 永远无法释放 → 全 Service 所有 Worker 的读写路径永久死锁。
        // Go defer 按 LIFO 执行，注册顺序 Add(-1) → Unlock → 实际执行顺序 Unlock → Add(-1)，
        // 恰好满足「writeRequested.Add(-1) 必须在 Unlock 之后」的时序要求（见下方修正说明）。
        defer w.pool.writeRequested.Add(-1)
        defer w.pool.rwMu.Unlock()
        w.safeExec(job)
        // 【修正说明】writeRequested.Add(-1) 必须在 Unlock 之后：
        // 如果在 Unlock 之前减计数，读路径看到 writeRequested==0 后退出自旋，
        // 获取信号量令牌，然后在 RLock() 上被仍持有的 WLock 阻塞——
        // 信号量令牌被"浪费"在阻塞的 goroutine 上，当 MaxConcurrentReads 较小时
        // 导致后续读路径主循环不必要的轮询等待。defer LIFO 天然保证此时序。
    }
}

// 【设计决策记录】readSem 信号量位置选择
//
// 方案 A（初期废弃）：纯阻塞式信号量在主循环获取
//   w.readSem <- struct{}{}  // 满时阻塞主循环
//   问题 1：高优先级 Write 被信号量间接阻塞（队头阻塞）
//   问题 2：BeginStop() 后主循环卡在 channel send 上永远不返回
//
// 方案 B（已废弃）：信号量在读 goroutine 内非阻塞获取（select + default 降级）
//   问题：MaxConcurrentReads 完全无效——令牌满时 goroutine 仍执行并持有 RLock，
//   同时持有 RLock 的数量无上界，写路径 TryLock 等待时间不可控。
//   本质上 MaxConcurrentReads 只统计了「持有令牌的执行数」，对系统行为零影响。
//
// 方案 C（采用）：信号量在主循环统一轮询中非阻塞获取 + 指数退避重试
//   通过 select+default 非阻塞尝试，失败后指数退避（1μs→100μs 上限）+ 重新进入循环。
//   循环中每次检查 closed（Stop 安全）和 writeRequested（写饥饿防护）。
//   读路径的退避上限 100μs 小于写路径的 1ms，因为读让步的目的是尽快恢复
//   ——writeRequested 归零或信号量释放后应立即进入。
//   MaxConcurrentReads 是**硬上限**：信号量令牌在 RLock 之前获取，
//   因此同时持有 RLock 的 goroutine 数 ≤ MaxConcurrentReads。
//   队头阻塞仍存在但有界（≤ 单次读操作耗时），且 closed 检查保证 Stop 安全。
//   相比方案 B，牺牲了极端场景下的主循环吞吐（令牌满时主循环短暂轮询等待），
//   但换来了真正有效的并发控制和可预测的写延迟
```

### 6.5 关键实现细节

#### 6.5.1 safeExec 改动：RW 模式下的 Profiler 处理

现有 `safeExec()` 已包含完整的 panic 恢复、profiler、中间件 OnComplete 回调、Job Release 逻辑。
在读 goroutine 中直接调用 `safeExec()`，这些保护机制自然生效。

**已审查的并发安全性**：

| 引用对象 | 并发安全性 | 说明 |
|---|---|---|
| `w.pool.invoker.ExecuteJob()` | ✅ | 业务层保证读方法不修改状态 |
| `w.pool.invoker.EscalateFailure()` | ✅ | 仅日志记录 |
| `w.pool.middlewareChain.ExecuteOnComplete()` | ✅ 已验证 | 所有内置中间件的 OnComplete 均使用 atomic 或无状态（详见下文） |
| `w.pool.profiler.Push()` / `Pop()` | ⚠️ **有并发问题** | 详见下文分析 |
| `w.pool.logger` | ✅ | Logger 线程安全 |
| `job.Release()` → sync.Pool.Put() | ✅ | `sync.Pool` 线程安全 |

**中间件 OnComplete 审查结论**：

| 中间件 | OnComplete 行为 | 线程安全 |
|---|---|---|
| RateLimitMiddleware | 空操作 | ✅ |
| DispatchKeyStatsMiddleware | 空操作 | ✅ |
| CircuitBreakerMiddleware | atomic.Add + CAS 状态转换 | ✅ |
| SentinelMiddleware | Sentinel SDK 内部安全 | ✅ |

**Profiler 并发问题（必须修复）**：

当前 `safeExec()` 中的 Profiler 调用：
```go
analyzer = w.pool.profiler.Push(fmt.Sprintf("[ STATE ]%s", reflect.TypeOf(job).String()))
```

存在两个问题：

1. **Profiler 内部数据竞争**：`Profiler.Push()/Pop()` 虽然用 `stackLocker`（sync.RWMutex）保护了
   `stack` 操作，但 `Report()` 方法在 `RLock` 下调用 `pushRecordLog()` 修改 `record` 列表，
   这与 `Pop()` 中的写操作存在竞争。在当前串行模型下不会触发（同一 Worker 只有一个
   goroutine 调用 Push/Pop），但 RW 模式下多个读 goroutine 并发调用将放大此竞争窗口。

2. **Profiler stack 语义破坏**：Profiler 的 `stack` 是调用栈概念（一次一个 Push-Pop 对），
   设计假设是串行执行。并发读 goroutine 会导致多个 Push 在 Pop 之前同时存在，栈的结构化
   追踪含义丧失。

3. **热路径性能开销**：`fmt.Sprintf` + `reflect.TypeOf` 每次调用都分配内存。在 RW 模式下
   每个并发读 goroutine 都会执行，放大 GC 压力。这是 RW 模式目标场景（高 QPS 读）的热路径。

**解决方案**：拆分 `safeExec` 为带参数的内部方法，通过 `skipProfiler` 参数控制 Profiler 行为：

> **⚠️ 实现说明**：Go 中无法在运行时判断"当前 goroutine 是否为读 goroutine"
> （没有 goroutine-local storage）。因此不能使用 `isReadGoroutine()` 这种方式，
> 而是通过**调用方传参**明确指定。

```go
// safeExec 保持原有签名不变（向后兼容，skipProfiler=false）
func (w *Worker) safeExec(job inf.IMailboxJob) {
    w.safeExecInternal(job, false)
}

// safeExecSkipProfiler RW 模式下读 goroutine 专用（跳过共享 Profiler）
func (w *Worker) safeExecSkipProfiler(job inf.IMailboxJob) {
    w.safeExecInternal(job, true)
}

func (w *Worker) safeExecInternal(job inf.IMailboxJob, skipProfiler bool) {
    // ... panic 恢复和 defer 保持不变 ...

    var analyzer *profiler.Analyzer
    // skipProfiler=true 时跳过共享 Profiler，避免并发安全问题和 stack 语义破坏
    if w.pool.profiler != nil && !skipProfiler {
        analyzer = w.pool.profiler.Push(
            fmt.Sprintf("[ STATE ]%s", reflect.TypeOf(job).String()))
        // 注：与当前 safeExec 保持一致。未来可在 Job 上缓存 typeTag 字符串，
        // 避免每次 reflect.TypeOf + fmt.Sprintf 的分配开销（见下方优化建议）。
    }

    if err := w.pool.invoker.ExecuteJob(ctx, job); err != nil {
        execErr = err
    }

    if analyzer != nil {
        analyzer.Pop()
        analyzer = nil
    }
}
```

**进一步优化建议**：
- 在 Job 类型上缓存 typeTag 字符串，避免每次 `reflect.TypeOf().String()` + `fmt.Sprintf`

**读操作耗时统计（必须实现）**：

> **⚠️** 读 goroutine 跳过共享 Profiler 后，RW 模式下读操作**完全不可观测**——
> 运维无法通过 Profiler 观察读操作的耗时分布，若一个读操作意外变慢（如触发 lazy loading
> 或 IO 阻塞），无任何感知手段，生产问题排查极其困难。
>
> 因此**必须**在 `safeExecSkipProfiler` 中实现轻量级读耗时统计（不走共享 Profiler stack），
> 作为 RW 模式的基础可观测性保证，而非可选优化。

实现方式：在 `safeExecSkipProfiler` 内部记录 `time.Now()` 开始和结束时间，
通过 per-Worker 的 `atomic` 或 lock-free histogram 记录读操作耗时。
具体指标详见 §10.7 的 `rw_read_duration` histogram。

```go
func (w *Worker) safeExecSkipProfiler(job inf.IMailboxJob) {
    // ... panic 恢复和 defer 保持不变 ...

    // 【读 goroutine panic 恢复增强】
    // 读 goroutine 中的 panic 由 safeExecInternal 内部 recover 捕获。
    // 但对于 RPC 类型的 Read Job，panic 后必须确保错误响应被发送给调用方，
    // 否则调用方会永远等待直到超时，且框架侧无法区分"读 goroutine panic 导致的响应丢失"。
    // safeExecInternal 内部的 recover 路径已通过 EscalateFailure 上报错误，
    // 但 EscalateFailure 不负责 RPC 响应回写。因此需要在 recover 路径中增加：
    //
    // 1. 对 RPC 类型 Job，从 envelope 中提取 reply channel 并发送错误响应
    //    （防止调用方永久等待超时）
    // 2. 递增 rw_read_panic_total 监控指标（区分读/写路径的 panic）
    // 3. 日志中包含 Worker ID + Job 方法名（Logger 已预置 serviceName/serviceUID）
    //
    // 具体实现：在 safeExecInternal 的 recover 块中增加 RPC 响应回写逻辑：
    // if skipProfiler { // 读 goroutine
    //     if rpcJob, ok := job.(*job.RpcJob); ok {
    //         if envelope := rpcJob.GetPayload(); envelope != nil {
    //             envelope.ReplyError(panicErr) // 确保调用方收到错误响应
    //         }
    //     }
    //     w.pool.rwReadPanicTotal.Add(1) // per-WorkerPool atomic counter
    // }

    // 读操作耗时统计（轻量级，不走共享 Profiler）
    var start time.Time
    if w.pool.enableRWMetrics {
        start = time.Now()
    }

    if err := w.pool.invoker.ExecuteJob(ctx, job); err != nil {
        execErr = err
    }

    if w.pool.enableRWMetrics && !start.IsZero() {
        elapsed := time.Since(start)
        w.rwReadDuration.Observe(elapsed) // per-Worker lock-free histogram
    }
}
```

#### 6.5.2 Stop/Drain 时的边界处理

Worker 关闭流程时序：

```
BeginStop()
  → closing.CAS(false, true)       // 拒绝新 SubmitJob
  → spin-wait submitters → 0       // 等待 in-flight SubmitJob 完成
  → closed.CAS(false, true)        // 通知 run 循环退出
  → idler.Wake()                   // 唤醒可能在等待的主循环

run() 退出时 defer:
  → w.inflightReads.Wait()         // 等待本 Worker 的读 goroutine 完成（带 StopTimeout）
  → pool.rwMu.Lock()               // 获取 WLock：等待其他 Worker 读完成 + 与其他 Drain 互斥
  → safeExec(pendingJob)           // 处理 TryLock 退出时暂存的已出队 Job（如有）
  → DrainAll(safeExec)             // 在 WLock 保护下串行处理残留消息
  → pool.rwMu.Unlock()
```

**重要**：Drain 阶段不再 spawn 读 goroutine，全部串行执行。因为此时 Worker 已关闭，不应再创建新 goroutine。
且 DrainExecute 在 `pool.rwMu.Lock()` 保护下执行，确保跨 Worker Drain 串行 + 与读 goroutine 互斥（见 §6.3）。

> **Trade-off 说明**：如果 Stop 时队列中残留大量 Read Job，Drain 串行执行会比正常 RW 模式慢。
> 这是有意的简化——Drain 是低频关闭路径，为其引入 RW 并发需要处理"关闭中又 spawn goroutine"
> 的边界问题（如 `inflightReads` 已 Wait 完成后又 Add），增加复杂度但收益极低。
> 对于 graceful shutdown 延迟敏感的场景，可配合 `DrainDiscard` 策略直接丢弃残留消息。
>
> **⚠️ Drain 串行化导致 shutdown 时间线性增长（必须关注）**：
>
> N 个 Worker 各有 M 个残留 Job，Drain 在 `pool.rwMu.Lock()` 保护下串行执行：
>
> | 场景 | 无 RW 模式 | 有 RW 模式 |
> |---|---|---|
> | N 个 Worker Drain | 并行执行: O(M × T) | 串行执行: O(N × M × T) |
> | Worker=8, M=100, T=1ms | ~100ms | ~800ms (8× 退化) |
>
> 在 rolling update / container orchestration 下可能超过 `terminationGracePeriodSeconds`。
>
> **必须实现的缓解方案**：
>
> 1. **（§10.5 已确定）先 Unlock 后 Stop + 原地排空 Drain**：调整 `resizeWorkers` 逻辑，
>    先释放 `p.mu.Lock()` 再调用 `worker.Stop()`，消除 Drain handler 自投递的死锁风险。
>    Worker 仍原地串行 Drain 队列残留 Job（在 `pool.rwMu.Lock()` 保护下），
>    保留 FIFO 因果一致性。详见 §10.5 完整方案。
>    **注意**：不使用 Job 迁移方案——将残留 Job 重新投递到 `DispatchJob()` 会追加到
>    目标 Worker 队列尾部，可能排在同 DispatcherKey 的更新 Job 之后，破坏 FIFO（§10.5）。
> 2. **Drain WLock 超时降级**：如果必须就地执行，为 Drain 阶段的 `pool.rwMu.Lock()`
>    设置超时（使用 TryLock 轮询），超时后降级为 DrainDiscard，避免全 Service 长期阻塞
>
> ℹ️ **DrainExecute 与 WLock**：多 Worker 场景下，所有 Worker 的 run() goroutine 的 defer 块
> 近乎同时执行。如果 Drain 不持有 `pool.rwMu.Lock()`，多个 Worker 同时 safeExec
> 处理残留 Job 会导致并发写入服务共享状态——数据竞争。因此 DrainExecute 必须
> 在 `pool.rwMu.Lock()` 保护下执行（见 §6.3 代码）。

**RW 模式下 Stop 的完整时序分析**：

主循环在 RW 模式下有三种可能的阻塞点，以下逐一分析 Stop 的交互：

| 主循环阻塞点 | BeginStop 能否打断 | 恢复条件 | Stop 延迟 |
|---|---|---|---|
| `idler.Idle()` | ✅ `idler.Wake()` 立即唤醒 | 即时 | ≈ 0 |
| 统一轮询循环（writeRequested/readSem） | ✅ 轮询中每次检查 `closed` | 下一次 Gosched 周期 | ≈ 0 |
| `rwMu.RLock()` | 不需要打断 | WLock 释放后自动恢复 | ≤ 写操作执行耗时 |
| `rwMu.TryLock()` 轮询 | ✅ 主循环检查 `closed` 后自行退出 | 立即（下一次退避周期） | ≈ 0 |

**Stop 延迟上界**：

```
Stop 总延迟 ≤ 当前 Job 执行时间 + w.inflightReads.Wait 时间 + pool.rwMu.Lock 时间 + Drain 耗时
```

由于写路径已改为 TryLock 轮询，写操作不会阻塞主循环退出。主循环退出后，
defer 中的 `w.inflightReads.Wait()` + `StopTimeout` 处理泄漏的读 goroutine，
然后 `pool.rwMu.Lock()` 保证 Drain 与其他 Worker 互斥。

**读 goroutine 泄漏场景**

如果读方法忽略 context cancellation 且永久阻塞（如无超时的网络 IO）：
1. 主循环的 TryLock 轮询检测到 `closed=true` → 暂存当前 Job 到 `pendingJob` → 退出主循环
2. defer 中 `w.inflightReads.Wait()` 阻塞（本 Worker 泄漏的读 goroutine 未完成）
3. `StopTimeout` 触发 → 自动降级为 `DrainDiscard` → Worker 跳过 rwMu.Lock() 直接丢弃残留消息并退出
4. 泄漏的 goroutine 在后台继续运行，但不影响 Worker 关闭流程

与旧版 `Lock()` 直接阻塞的设计相比，TryLock 方案**保证 StopTimeout 始终能生效**：
即使读 goroutine 泄漏，Worker.Stop() 也会在 StopTimeout 后返回，不会永久挂起。

**这不是理论风险——任何不 respect context 的读方法都可能触发此场景。**
因此 §10.3 的 context timeout 硬性要求和 `StopTimeout` 兜底机制是本设计的**安全前提**，
不是可选优化。

#### 6.5.3 读 goroutine 与队列消费的解耦

读 goroutine 从 run() 主循环中 spawn 出去后，主循环**立即继续消费下一个 Job**：

- 下一个也是 Read → 再 spawn 一个 goroutine（并发 RLock）
- 下一个是 Write → `rwMu.TryLock()` 轮询，等待所有 in-flight Read 完成（同时检查 closed 标志）

这意味着 **Read Job 的消费速率不受执行耗时限制**（goroutine 在后台执行），
而 Write Job 天然起到"屏障"作用——阻塞主循环直到所有前序 Read 完成。

#### 6.5.4 读 goroutine 的资源开销与控制

每个 Read Job spawn 一个 goroutine。Go goroutine 初始栈仅 2-8KB，创建和调度开销极低。

如果担心**读请求突发**导致大量 goroutine 堆积，可通过 `MaxConcurrentReads` 配置限制：

```yaml
mailbox:
  enableRWMode: true
  maxConcurrentReads: 32  # 限制整个 Service 最多 32 个并发读 goroutine
```

信号量令牌在**主循环中、RLock 之前**通过统一轮询循环获取（§6.4），因此：
- `MaxConcurrentReads` 是同时持有 RLock 的 goroutine 数量的**硬上限**
- 令牌满时主循环不会 spawn 新 goroutine，而是轮询等待（每次检查 closed 和 writeRequested）
- 不存在"大量 goroutine 等待信号量"的堆积场景——goroutine 仅在获取令牌后才被创建

**goroutine 堆积风险（已消除）**：

在原方案中，信号量在 goroutine 内部非阻塞获取（select+default 降级），极端突发场景
可能 spawn 大量 goroutine。修正后的方案中信号量在主循环获取，goroutine 数量严格受限：

| MaxConcurrentReads | 最大并发 goroutine 数 | 最大 RLock 持有数 | 内存开销 |
|---|---|---|---|
| 32 | ≤ 32 | ≤ 32 | ≤ 0.26 MB |
| 128 | ≤ 128 | ≤ 128 | ≤ 1 MB |
| 不限制（=0） | 无上界 ⚠️ | 无上界 ⚠️ | 不可控 |

**约束说明**：

- 当 `MaxConcurrentReads > 0` 时，信号量令牌在主循环中获取，goroutine 数严格受限
- 当 `MaxConcurrentReads = 0`（不限制）时，goroutine 数无上界，退化为原方案的堆积风险
- **生产环境启用 RW 模式时，必须配置 `MaxConcurrentReads`（默认值 `runtime.NumCPU()*4`）**
- 上游流量控制（SuspendPolicy、中间件限流）和 Write 屏障效应提供额外的天然约束

---

## 7. 配置层改动

### 7.1 MailboxConf 扩展

在 `engine/pkg/config/define.go` 的 `MailboxConf` 中新增：

```go
type MailboxConf struct {
    // QueueMode 队列模式: "dual" | "priority"
    QueueMode string `binding:""`

    // SchedulePolicy 调度策略配置
    SchedulePolicy *WorkerSchedulePolicy `binding:""`

    // MiddlewareConf 中间件配置
    MiddlewareConf *MailboxMiddlewareConf `binding:""`

    // ---- RW 读写分离配置 ----

    // EnableRWMode 是否启用读写分离模式
    // 启用后，标记为 ReadOnly 的方法可在 Worker 内并发执行
    // 默认: false（关闭时行为与当前完全一致）
    EnableRWMode bool `binding:""`

    // MaxConcurrentReads 整个 Service 最大并发读执行数（硬上限）
    // 仅在 EnableRWMode=true 时生效
    // ⚠️ 这是「硬上限」：信号量令牌在主循环中、RLock 之前获取，
    // 因此同时持有 RLock 的 goroutine 数量 ≤ MaxConcurrentReads。
    // 令牌满时主循环通过轮询等待（每次检查 closed 和 writeRequested），
    // 不会永久阻塞，但可能导致短暂队头阻塞（≤ 单次读操作耗时）。
    // 默认值: runtime.NumCPU() * 4（兼顾 CPU 密集和 IO 密集场景）
    // 显式设为 0 表示不限制（不推荐，极端突发场景可能导致 goroutine 堆积）
    // 建议值: CPU 密集型读 → runtime.NumCPU()，IO 密集型读 → runtime.NumCPU() * 4~8
    MaxConcurrentReads int `binding:""`

    // StopTimeout RW 模式下 Worker Stop 的最大等待时间
    // 仅在 EnableRWMode=true 时生效
    // 超时后 Worker 放弃等待泄漏的读 goroutine，强制继续 Drain 并退出
    // 默认: 10s（见 §7.3 fixConf，避免超过 K8s terminationGracePeriodSeconds 默认 30s）
    StopTimeout time.Duration `binding:""`
}
```

### 7.2 YAML 配置示例

```yaml
# 场景 1：单 Worker + RW 模式（推荐入门，最简单最安全）
mailbox:
  enableRWMode: true
  schedulePolicy:
    initialWorkerNum: 1

# 场景 2：多 Worker + RW 模式 + 并发限制
mailbox:
  enableRWMode: true
  maxConcurrentReads: 32
  schedulePolicy:
    initialWorkerNum: 4
    virtualWorkerRate: 24

# 场景 3：不启用 RW（默认，与当前完全一致）
mailbox:
  schedulePolicy:
    initialWorkerNum: 2
```

### 7.3 配置校验

在 `fixConf()` 中新增：

```go
func fixConf(conf *config.MailboxConf) *config.MailboxConf {
    // ... 现有校验保持不变 ...

    // RW 模式配置校验
    if conf.EnableRWMode {
        // 默认值: min(runtime.NumCPU() * 4, 64)，避免大核机器上默认值过高
        // （如 64 核服务器默认 256，导致 GC 压力和写等待时间不可控）
        if conf.MaxConcurrentReads == 0 {
            conf.MaxConcurrentReads = runtime.NumCPU() * 4
            if conf.MaxConcurrentReads > 64 {
                conf.MaxConcurrentReads = 64
            }
            // 单 Worker 场景不需要太多并发读（只有一个主循环 spawn goroutine）
            if conf.SchedulePolicy != nil && conf.SchedulePolicy.InitialWorkerNum == 1 &&
                conf.MaxConcurrentReads > 32 {
                conf.MaxConcurrentReads = 32
            }
        }
        if conf.MaxConcurrentReads < 0 {
            conf.MaxConcurrentReads = runtime.NumCPU() * 4
            if conf.MaxConcurrentReads > 64 {
                conf.MaxConcurrentReads = 64
            }
        }
    } else {
        // 未启用 RW 时忽略 MaxConcurrentReads
        conf.MaxConcurrentReads = 0
    }
    // StopTimeout 默认 10s（而非 30s，避免超过 K8s terminationGracePeriodSeconds 默认 30s）
    // 多 Worker Drain 串行化时总时间 = StopTimeout + N * M * T_job，
    // 需要给 Drain 留充足余量。建议 StopTimeout 不超过 terminationGracePeriodSeconds 的 1/3。
    if conf.EnableRWMode && conf.StopTimeout <= 0 {
        conf.StopTimeout = 10 * time.Second
    }
    // 注意：StopTimeout 超时后，Worker 会将 DrainPolicy 强制降级为 DrainDiscard，
    // 即使用户配置了 DrainExecute（详见 §6.3 stopTimedOut 逻辑和 §10.3 说明）。
    // 这是有损但安全的降级策略——避免泄漏的读 goroutine 与 Drain 写操作数据竞争。

    return conf
}
```

---

## 8. 因果一致性保证

### 8.1 Mailbox 级 RW 的因果一致性保证

Mailbox 级 RW 方案的因果一致性由 **队列 FIFO + 共享 RWMutex 语义** 共同保证：

```
Job 入队顺序: [Write(gold=100)] → [Read(gold)]

Worker.run() 处理：
1. 取出 Write Job → rwMu.Lock() → 执行写入 gold=100 → rwMu.Unlock()
2. 取出 Read Job  → rwMu.RLock() → 读到 gold=100 ✓ → rwMu.RUnlock()
```

因为所有 Job **通过同一 DispatcherKey 路由到同一 Worker 的同一队列**，FIFO 保证写先执行、读后执行。`sync.RWMutex` 的 happens-before 语义保证读能看到写的结果。

### 8.2 "读阻塞写"的反向场景

```
Job 入队顺序: [Read(gold)] → [Write(gold=200)] → [Read(gold)]

Worker.run() 处理：
1. 取出 Read  → rwMu.RLock() → spawn goroutine 读 gold(当前值)
2. 取出 Write → rwMu.TryLock() 轮询 → 等待步骤 1 的 Read goroutine RUnlock
3.                                   → Read 完成 RUnlock → TryLock 成功
4.                                   → 执行写入 gold=200 → rwMu.Unlock()
5. 取出 Read  → rwMu.RLock() → spawn goroutine 读 gold=200 ✓
```

因果关系正确：Write 等待之前的 Read 完成后独占执行，之后的 Read 看到新值。

### 8.3 跨 Key 的因果一致性

跨 DispatcherKey 的因果一致性不在本方案范围内——这与当前模型一致。不同 Key 路由到不同 Worker，天然独立。如果业务需要跨 Key 一致性，应通过业务层协调（如分布式事务、消息序列号等）。

### 8.4 内存可见性保证

Go 的 `sync.RWMutex` 遵循 [Go Memory Model](https://go.dev/ref/mem)：
- `RLock()` 与前一个 `Unlock()` 形成 happens-before 关系
- `Lock()` 与前一个 `RUnlock()` 形成 happens-before 关系

这保证：
- 写操作完成后，后续读 goroutine 能看到写入的值
- 多个并发读 goroutine 之间看到的是一致的快照（最后一次写之后的状态）

### 8.5 并发读的完成顺序不保证 FIFO

多个并发 Read goroutine 的**执行完成顺序不保证与入队顺序一致**。例如：

```
入队顺序: [R1, R2, R3]
spawn 顺序: R1 → R2 → R3
完成顺序: 可能是 R3 → R1 → R2（取决于各自的执行耗时和调度）
```

对于读操作，这通常不是问题——读操作之间没有因果依赖，结果由读取时的状态快照决定。
但如果业务依赖"先发的读先返回"（如流式查询分页），应将其标记为 Write（保持串行），
或在业务层通过序列号等机制保证顺序。

---

## 9. 向后兼容保证

### 9.1 零改动兼容路径

| 维度 | 兼容性 |
|---|---|
| 接口 | `IMailboxJob` **不修改**（零 Breaking Change）；RW 能力通过**独立接口 `IRWModeJob`** 提供（`Job[T]` 同时实现两者；调用方通过类型断言按需使用；未实现 `IRWModeJob` 的 Job 兜底为 `RWModeWrite`）；ReadOnly 方法管理通过**独立接口 `IReadOnlyMethodMgr`** 提供（不修改 `IMethodMgr`） |
| 配置 | `EnableRWMode` 默认 `false`，不开启时完全不走 RW 分支 |
| 方法命名 | 现有 `Rpc`/`Api` 前缀方法自动识别为 Write，行为不变 |
| Job 行为 | 未调用 `SetRWMode()` 的 Job 零值为 `RWModeWrite`，走独占串行路径 |
| Worker | `enableRW=false` 时 run() 走原有 `safeExec()` 路径，无任何额外开销 |
| 性能 | 未启用 RW 时，Worker 不调用 `rwMu`（不调用 Lock/RLock），零性能影响 |

### 9.2 渐进式迁移路径

```
Phase 1: 框架实现 RW 功能，默认关闭
         ↓ 所有已有服务行为完全不变
Phase 2: 选择一个读多写少的服务，配置 enableRWMode: true
         ↓ 此时所有方法仍按 Write 执行（因为没有 ReadOnly 标记），行为不变
Phase 3: 将该服务的查询方法改名为 RpcRo 前缀（或实现 IReadOnlyDeclarer）
         ↓ 标记的方法开始并发读
Phase 4: go test -race + 压测验证
         ↓ 确认无数据竞争
Phase 5: 逐步推广到更多服务
```

---

## 10. 风险与边界问题

### 10.1 读操作的定义边界

**严格定义**：`ReadOnly` 意味着方法执行期间 **不修改服务的任何可观测共享状态**。

| 操作 | 是否 Read | 说明 |
|---|---|---|
| 纯查询，返回数据 | ✅ Read | 标准只读操作 |
| 查询 + 更新"最后访问时间" | ❌ Write | 修改了共享状态 |
| 查询 + 递增"访问计数器" | ❌ Write | 修改了共享状态。除非计数器用 `atomic` 且不影响业务正确性 |
| 查询 + 懒加载缓存 | ❌ Write | 修改了缓存。除非缓存本身是并发安全的（如 `sync.Map`）且与正确性无关 |
| 查询 + 写日志 | ✅ Read | 日志系统通常并发安全，不算业务共享状态 |
| 查询 + 发送监控指标 | ✅ Read | 监控系统通常并发安全 |
| 调用不存在的方法或修改框架内部状态 | ❌ **Write** | 任何修改服务内部结构的操作都必须独占 |

**标错的风险不对称**：
- 把写标成读 → **⚠️ 并发安全问题**，可能数据竞争（race condition）
- 把读标成写 → ✅ 无害，只是没享受到并发收益

因此默认"不标 = 写"是安全的兜底策略。

### 10.2 Profiler 和中间件的并发安全

读 goroutine 并发调用 `safeExec()` 时，框架组件需保证并发安全：

| 组件 | 审查结论 | 所需改动 |
|---|---|---|
| `Profiler.Push() / Pop()` | ⚠️ **有并发问题** | RW 模式下读 goroutine 不调用共享 Profiler（详见 6.5.1） |
| 中间件 `OnComplete()` | ✅ 全部安全 | 已审查所有内置中间件，均为 atomic 或无状态操作（详见 6.5.1） |
| `job.Release()` (sync.Pool) | ✅ 安全 | 无需改动 |
| `Logger` | ✅ 安全 | 无需改动 |

**Profiler 具体问题**：
1. `Report()` 方法在 `RLock` 下调用 `pushRecordLog()` 修改 `record`/`stack` 列表 → 与 `Pop()` 的写锁操作竞争
2. 多个并发 Push/Pop 破坏调用栈语义
3. `fmt.Sprintf` + `reflect.TypeOf` 每次分配内存，高 QPS 读场景放大 GC 压力

**已确定方案**：RW 模式下读 goroutine 调用 `safeExecSkipProfiler()`（通过 `skipProfiler` 参数跳过共享 Profiler）。
Profiler 标签保持当前 `fmt.Sprintf + reflect.TypeOf` 方式（与现有 `safeExec` 一致），
未来可通过缓存 Job 类型标签优化热路径开销。详见 6.5.1 节代码。

> **⚠️ 用户自定义中间件警告**：上方审查表仅覆盖框架内置中间件。如果用户注册了
> **自定义中间件**，其 `OnComplete` 回调在 RW 模式下可能被多个读 goroutine **并发调用**。
> 启用 RW 模式的服务必须确保所有已注册中间件的 `OnComplete` 实现是**线程安全**的
> （使用 atomic、无状态操作或独立锁保护）。框架应在 `EnableRWMode` 配置文档和
> 中间件注册 API 注释中醒目标注此要求。

> **⚠️ Profiler 现有 bug（独立于 RW 特性，需同步修复）**：
> 1. `pushRecordLog()` 中 record 满时误删 `stack` 元素而非 `record` 元素
>    **影响范围比描述更大**：删除的是正在执行中的 handler 的栈帧（stack 中的 Element），
>    对应的 `Analyzer.Pop()` 随后对已被删除的 `list.Element` 调用 `stack.Remove()`
>    → 可能 panic 或静默损坏链表。**这在当前串行模型下已是潜在 bug，不仅是 RW 模式的问题。**
>    修复：`slf.stack.Front()` 改为 `slf.record.Front()`，`slf.stack.Remove` 改为 `slf.record.Remove`
> 2. `Report()` 在 `RLock` 下执行写操作（`pushRecordLog` 修改 record/stack，替换 `prof.record`），
>    应使用 `Lock`（写锁）
> 3. `Report()` 遍历 `mapProfiler` 时未持有 `mapLock`，与并发的 `GetProfiler()`/注册操作
>    存在 map 并发读写风险（`mapLock` 仅在 `GetProfiler` 等写入路径中使用，
>    `Report()` 的迭代路径未加锁）

### 10.3 读 goroutine 泄漏风险

如果读方法内部阻塞（如等待外部 IO 无超时），读 goroutine 不会释放 RLock。
写路径使用 TryLock 轮询并检查 closed 标志，因此**不会导致主循环卡死**（详见 §6.4 TryLock 说明）。
但泄漏的读 goroutine 会导致 `w.inflightReads.Wait()` 阻塞，需由 StopTimeout 兜底处理
（详见 §6.5.2 Stop 分析）。

**这使得 context timeout 不是「最佳实践建议」，而是 RW 模式的安全前提。**

**硬性要求（MUST）**：
1. **标记为 ReadOnly 的方法必须 respect `context.Context` cancellation 和 timeout**。
   框架应在文档和代码注释中以 **MUST** 级别要求此条，而非 SHOULD。
   建议在 `IReadOnlyDeclarer` 接口注释和 `RpcRo` 前缀文档中醒目标注。
2. `MaxConcurrentReads` 配置限制防止 goroutine 无限堆积。

**兜底机制（StopTimeout）**：

即使强制要求 context timeout，仍需防御性设计——业务代码不可控。
新增 `StopTimeout` 配置作为最后兜底：

```go
// MailboxConf 中新增
StopTimeout time.Duration `binding:""` // RW 模式下 Stop 的最大等待时间，默认 10s（见 §7.3）
```

Worker Stop 路径中使用超时：

```go
// run() 退出时 defer 中（完整逻辑见 §6.3）
stopTimedOut := false
if w.pool.enableRW.Load() {
    done := make(chan struct{})
    go func() {
        w.inflightReads.Wait()  // per-Worker：仅等待本 Worker 的读 goroutine
        close(done)
    }()
    select {
    case <-done:
        // 本 Worker 的所有读 goroutine 正常完成
    case <-time.After(w.pool.stopTimeout):
        // 超时：标记不安全关闭，Drain 自动降级为 DrainDiscard
        stopTimedOut = true
        // 注意：不尝试强制 RUnlock（会导致数据竞争和 double-unlock panic）
        w.pool.logger.Errorf("Worker %d: StopTimeout (%v) exceeded, "+
            "read goroutines still in-flight. "+
            "Drain forced to DrainDiscard to avoid data race.",
            w.workerId, w.pool.stopTimeout)
    }
}
// stopTimedOut=true 时，后续 Drain 自动使用 DrainDiscard（见 §6.3 完整代码）
```

**StopTimeout 语义**：超时后 Worker 放弃等待泄漏的读 goroutine，**自动将 Drain 策略降级为 `DrainDiscard`**
（丢弃残留消息而非执行），然后返回。

**降级为 DrainDiscard 的原因**：泄漏的读 goroutine 仍在后台执行 `ExecuteJob`（访问服务共享状态）。
如果此时 Drain 仍用 `safeExec` 执行残留的写操作，写操作与泄漏的读 goroutine 之间**不再有
`rwMu` 保护**（超时后未获取锁就开始 Drain），会产生数据竞争。
`DrainDiscard` 仅释放 Job 资源不执行业务逻辑，避免了此问题。

泄漏的 goroutine 仍在后台运行，其 `defer RUnlock()` 最终会执行（如果方法不是真正死锁）。
这是一个**有损但安全**的降级策略——优先保证进程可以正常关闭且不引入数据竞争。

**❗ 被丢弃的 Write Job 必须通知上层（修正）**：

`PostJob()` 返回 `nil`（成功）后，调用方认为 Job 已被接受。但 StopTimeout 触发时，
所有残留的 Write Job（包括 `pendingJob` 中已出队的写操作）被 **静默丢弃**。
对于业务写操作（如“扣款”、“发送奖励”），丢弃 == 无声数据丢失。

**必须修复**：`discardExec` 在处理被丢弃的 Job 时，除了触发中间件 `OnComplete`
（传入 `ErrMailboxNotRunning`）和 `Release()` 外，还应：

1. **通过 `EscalateFailure` 报告丢弃事件**：记录被丢弃的 Job 类型、方法名、原因
   （StopTimeout 导致 DrainDiscard），使运维可感知哪些写操作被丢弃
2. **发出监控指标**：`rw_drain_discard_total` 计数器，用于告警
3. **日志级别为 WARN 而非 DEBUG**：确保生产环境可见

此外，建议在 `IMailboxInvoker` 接口中新增 `OnJobDiscarded(job, reason)` 回调，
允许业务层对被丢弃的 Job 做补偿处理（如写入持久化队列、重试等）。

**不应做的事**：
- 不尝试从外部强制 `RUnlock()`——这会导致：
  - 写操作随即获得 Lock 并开始修改状态，而原读 goroutine 仍在读取 → 数据竞争
  - 原读 goroutine 执行到自己的 `defer RUnlock()` → double RUnlock → panic

**相关配置改动**：在 §7.1 的 `MailboxConf` 中需同步新增 `StopTimeout` 字段。

### 10.4 RW 模式下的优先级调度

当前 Worker 使用 `PriorityQueueManager` 按优先级调度。RW 模式下，高优先级的 Write Job 可能被低优先级的 Read goroutine 阻塞（因为 Read 先获取了 RLock）。

**分析**：这是 RWMutex 的固有语义——Write 必须等待所有 Read 完成。在多优先级 + RW 场景下，这可能导致高优先级 Write 延迟略微增加。

**影响评估**：实际场景中，Read goroutine 通常很快完成（毫秒级），对高优先级 Write 的延迟影响可忽略。如果未来发现瓶颈，可考虑引入"优先级感知的读中断"机制（复杂度高，暂不设计）。

### 10.5 AutoScaler 兼容性与 resizeWorkers 缩容风险

AutoScaler 根据 Worker 的队列长度和负载决定扩缩容。RW 模式下，Worker 的 `GetJobLen()` 仍反映队列中排队的 Job 数量（包含 Read 和 Write），in-flight 读 goroutine 不在队列中计数。

**决策逻辑影响**：AutoScaler 的扩缩容决策逻辑**不受 RW 模式影响**，无需改动。但如果需要更精确的负载指标，未来可增加 `GetInflightReadCount()` 方法。

**⚠️ resizeWorkers 缩容时的全 Service 暂停风险（RW 模式特有）**：

当前 `resizeWorkers` 缩容流程：持有 `p.mu.Lock()` → 调用 `worker.Stop()` → 触发 `run()` defer →
`pool.rwMu.Lock()` + `DrainAll(safeExec)` + `pool.rwMu.Unlock()`。

`pool.rwMu.Lock()` 会阻塞**其他未被移除的 Worker** 的读路径（`RLock()` 等待）和写路径（`TryLock()` 返回 false）。
被移除 Worker 的 Drain 持续多久（取决于残留 Job 数和每个 Job 的执行时间），
**整个 Service 的所有 Worker 就暂停多久**。在非 RW 模式下 Drain 不持有跨 Worker 的锁，
缩容期间其他 Worker 不受影响，但 RW 模式下行为退化。

**补充风险**：`resizeWorkers` 持有 `p.mu.Lock()`，Drain handler 若触发
“PostJob to self” → `DispatchJob` 需 `p.mu.RLock()` → **死锁**。
这是已有风险（非 RW 特有），但 Drain WLock 增加了锁图复杂度。

**缓解方案**（按优先级排列）：
1. **✅ （必须实现）先 Unlock 后 Stop + 原地排空**：调整 `resizeWorkers` 逻辑，
   消除死锁风险并保留 FIFO 因果一致性：
   ```
   resizeWorkers 缩容流程（修正后）：
   p.mu.Lock()
     → 从 hash ring 移除被缩容 Worker ID（新 Job 不再路由到它们）
     → 从 p.workers map 中取出被缩容 Worker 引用
   p.mu.Unlock()                              // ← 先释放 pool 锁
     → worker.BeginStop()                      // 拒绝新投递，通知主循环退出
     → worker.Wait()                           // 等待主循环退出 + Drain 完成
       → run() defer 中正常执行 Drain（原地排空队列残留 Job）
       → Drain 在 pool.rwMu.Lock() 保护下串行执行（§6.3）
       → Drain handler 若触发 PostJob to self → DispatchJob 需 p.mu.RLock()
         → p.mu 已释放，RLock 成功 → **不死锁** ✅
   p.mu.Lock()
     → 清理数据结构（delete map entry, 清理 dispatchCnt 等）
   p.mu.Unlock()
   ```

   **为什么不使用 Job 迁移**：将残留 Job 重新投递到 `DispatchJob()` 会打破 FIFO 因果一致性。
   被迁移的 Job 追加到目标 Worker 队列尾部，但该队列中可能已有同 DispatcherKey 的更新 Job
   ——导致旧 Job 排在新 Job 之后执行，破坏 §8.1 的核心保证。

   原地 Drain 保证所有 Job 在原 Worker 中按 FIFO 执行完毕，因果一致性不受影响。
   被缩容 Worker 的 Drain 持有 `pool.rwMu.Lock()` 期间，其他 Worker 的读路径被阻塞
   （队头阻塞 ≤ Drain 耗时），这是可接受的 trade-off。

2. **文档约束**：明确标注 RW 模式下缩容可能导致短暂全 Service 暂停，AutoScaler 应避免频繁缩容

> **❗ 此问题为 P0 级死锁风险，方案 1 必须在实现阶段落地**。
> 原始代码中 `resizeWorkers` 持有 `p.mu.Lock()` 期间调用 `worker.Stop()` → Drain handler
> 触发 PostJob to self → `DispatchJob` 需 `p.mu.RLock()` → **确定性死锁**。
> 修正方案通过「先 Unlock 后 Stop」打破锁环，消除死锁。

### 10.6 Module 树结构与 RW 模式

框架**不支持运行期动态增删 module**，所有模块在服务启动阶段注册完成。
运行期不存在 `AddModule()`/`ReleaseModule()` 对 `Module.children`、`Module.rootContains` 等
无锁数据结构的写操作。

RW 模式下，读 goroutine 遍历 `children`/`rootContains` map 时不会与写操作并发，
因为这些结构在运行期为只读，无需加锁保护。

同理，`MethodMgr.methods` 也是启动阶段构建的静态表（见 §5.2），
运行期 `GetMethodFunc()`/`IsReadOnly()` 的并发读天然安全。

`MethodMgr.RemoveMethods()` 仅在 Service shutdown 阶段（模块卸载流程）调用，
此时 Service 已关闭对外接口，所有 Worker 已停止，不存在并发读 `methods` 的 goroutine。
作为防御性措施，`RemoveMethods()` 内部增加了 RW 模式下的运行时校验（见 §5.2），
防止误在运行期调用导致 map 并发读写 fatal。

> **设计简化说明**：去掉动态 module 支持后，整个 RW 方案的复杂度大幅降低：
> - `MethodMgr` 无需 `sync.RWMutex`（静态表，只读无竞争）
> - 无需在 `AddModule()`/`ReleaseModule()` 入口做 ReadOnly 上下文检测
> - 无需担心 `children`/`rootContains` map 的并发读写
> - 读 goroutine 访问模块树结构时天然安全（运行期无写者）
>
> 这是用“不支持动态 module”换取“系统复杂度可控”的 trade-off。
> 若未来需要重新支持动态 module，需要：
> 1. `MethodMgr` 恢复 `sync.RWMutex` 保护
> 2. `children`/`rootContains` 要么加锁、要么通过 RW 模式的 Write 语义保证独占
> 3. 需要在 `AddModule()`/`ReleaseModule()` 入口添加 ReadOnly 上下文检测防止误用

### 10.7 可观测性

RW 模式引入了新的并发维度，运维和调优需要对应的运行时指标。**必须**在 Worker 层暴露以下指标：

| 指标 | 类型 | 获取方式 | 用途 | 优先级 |
|---|---|---|---|---|
| `rw_read_duration` | histogram | `safeExecSkipProfiler` 内计时（详见 §6.5.1） | **读操作耗时分布**（弥补跳过 Profiler 的可观测性缺失） | **P0 必须** |
| `inflight_read_count` | gauge | `atomic.Int64` 在 Add/Done 时维护 | 当前 in-flight 读 goroutine 数，用于判断读负载 | **P0 必须** |
| `rw_write_wait_duration` | histogram | TryLock 前后计时 | 写操作等待读完成的耗时，用于发现读阻塞写的瓶颈 | **P0 必须** |
| `rw_read_total` / `rw_write_total` | counter | 在 execWithRW 中递增 | 读写请求比例，验证 RW 模式的收益预期 | P1 建议 |
| `rw_rlock_wait_duration` | histogram | 读路径 `rwMu.RLock()` 前后计时 | **读路径被其他 Worker 的 WLock 阻塞的耗时**（跨 Worker 队头阻塞可观测性） | P1 建议 |
| `rw_drain_discard_total` | counter | discardExec 中递增（§10.3） | StopTimeout 导致的 Job 丢弃数，用于告警 | P1 建议 |
| `rw_job_timeout_total` | counter | 硬超时看门狗触发时递增（§11.1） | handler 执行超时数，用于发现卡死的 Worker | P1 建议 |

**实现建议**：
- `rw_read_duration` 在 `safeExecSkipProfiler` 内部通过 `time.Now()`/`time.Since()` 实现，
  使用 per-Worker lock-free histogram 避免跨 goroutine 竞争（详见 §6.5.1 代码）
- `inflight_read_count` 需在每个 Worker 的 `inflightReads.Add/Done` 旁维护一个 per-Worker `atomic.Int64` 计数器（`WaitGroup` 不提供当前计数查询），WorkerPool 汇总时遍历所有 Worker 累加
- `rw_write_wait_duration` 在写路径 `writeRequested.Add(1)` 和 `TryLock` 成功之间计时
- 指标暴露方式与现有 Profiler/Monitor 体系保持一致，不引入新依赖
- P0 指标随 RW 模式启用自动开启（零配置），P1 指标通过配置或 Profiler 开关启用

### 10.8 写序列化的量化影响分析

RW 模式下所有写操作全 Service 串行（WLock 互斥）。对于多 Worker 场景，这是从"异 Key 并行写"
到"全局串行写"的语义退化。以下提供量化方法帮助用户判断是否适合开启 RW 模式：

**写队列延迟公式**：

```
写排队延迟 = 写 QPS × 平均写耗时
```

| 场景 | Worker 数 | 写 QPS | 平均写耗时 | 原模型（异 Key 并行写） | RW 模式（全局串行写） |
|---|---|---|---|---|---|
| 读多写少（典型） | 4 | 100 | 1ms | 25ms/s（4 路并行） | 100ms/s ✅ 可接受 |
| 读写各半 | 4 | 1000 | 1ms | 250ms/s | 1000ms/s ⚠️ 队列增长 |
| 写为主 | 4 | 5000 | 1ms | 1250ms/s | 5000ms/s ❌ 不可用 |
| 写耗时较长 | 4 | 100 | 10ms | 250ms/s | 1000ms/s ⚠️ 需评估 |

**适用性判断规则**：

```
✅ 适合开启 RW：写 QPS × 平均写耗时 < 1.0（即写操作占用 < 100% 全局串行时间）
⚠️ 需评估：    写 QPS × 平均写耗时 在 0.5 ~ 1.0 之间
❌ 不适合：    写 QPS × 平均写耗时 > 1.0（写队列会无限增长）
```

**补充说明**：
- 单 Worker + RW 场景无退化（原来也是全串行，RW 只增加了读并发）
- 写 QPS × 平均写耗时 > 0.5 时，建议先用 `go test -bench` 验证再上线
- 如果当前模型已使用多 Worker 且依赖异 Key 写并行，开启 RW 会使写吞吐退化为 1/N

### 10.9 Timer/ConcurrentCallback 的 DispatcherKey 优化

当前代码中 `pushTimerCallback` 和 `pushConcurrentCallback` 使用 `uuid.NewString()` 作为
`DispatcherKey`，使回调**随机分散到任意 Worker**。这些 Job 类型在 §4.4 中定义为 `RWModeWrite`。

**RW 模式下的影响**：

Timer/Callback 被随机分配到不同 Worker → 每个 Worker 的主循环走写路径 → TryLock 轮询。
随机分散导致 `writeRequested` 被多个 Worker 交替递增，对读路径产生不必要的干扰。

**适用场景说明**：RW 模式的目标场景是**读多写少**的服务（排行榜查询、用户信息查询、配置读取等），
这类服务通常**不会有大量高频 Timer 需求**。典型的 RW 模式服务中 Timer 数量有限（如少量心跳检测、
超时监控），不会形成高频写压力。因此 Timer 在 RW 模式下的影响有限，无需过度设计。

**优化方案：使用 timerName 作为 DispatcherKey**

将 Timer/Callback 的 DispatcherKey 从随机 UUID 改为 **timerName**（定时器名称）：

```go
// pushTimerCallback 优化（修改前）
job.DispatcherKey = uuid.NewString()  // 随机分散

// pushTimerCallback 优化（修改后）
job.DispatcherKey = timer.GetName()   // 使用 timerName 作为 key
```

**优势**：
1. **确定性路由**：同名 Timer 的回调始终路由到同一 Worker，保证该 Timer 回调的执行顺序性
2. **减少跨 Worker 写冲突**：多个不同 Timer 通过哈希环分散到不同 Worker，
   避免所有 Timer 集中到一个 Worker（固定 key 的缺陷）
3. **可预测性**：Timer 的调度路径可通过名称推断，便于调试和问题排查
4. **向后兼容**：仅改变 DispatcherKey 的生成方式，不影响 Timer 的注册/触发/取消流程

**ConcurrentCallback** 可类似处理：使用回调标识（如请求 ID 或目标方法名）作为 DispatcherKey，
使同一异步操作的回调路由到固定 Worker，保证回调顺序性。

**补充约束**：如果 RW 模式服务确实有高频 Timer 需求（不推荐，与 RW 模式目标场景不匹配），
应评估写 QPS 是否满足 §10.8 的适用性判断（`写 QPS × 平均写耗时 < 1.0`），
不满足时应关闭 RW 模式或将 Timer 密集逻辑拆分到独立服务。

### 10.10 ReadOnly 标记的运行时正确性校验

设计完全依赖开发者正确标记 ReadOnly。一旦标错（把写方法标为 ReadOnly），后果是**静默数据竞争**，
极难排查：`-race` 检测器在 CI/test 中可能覆盖不到特定并发时序，且生产环境没有任何防御措施。

**标错的后果**：
- `map concurrent write panic` → 进程崩溃
- 更隐蔽的数据损坏（无 panic 但数据不一致）
- 难以复现的间歇性 bug

**必须实现的运行时校验机制**：

1. **Debug 模式写屏障（-race build 或配置开关）**：
   在 `execWithRW` 读路径中，执行 handler 前后对比 Service 的关键共享状态快照
   （如服务层业务状态的 hash），如果发生变化则立即 panic
   并输出 handler 名称和调用栈。开销较大，仅在 debug/test 环境开启。

2. **ReadOnly handler 的 context 注入**：
   在 `safeExecInternal` 中，根据 `skipProfiler` 参数（读 goroutine 为 true）
   向 context 中注入 RWMode 信息：
   ```go
   func (w *Worker) safeExecInternal(job inf.IMailboxJob, skipProfiler bool) {
       ctx := job.GetContext()
       if w.pool.enableRW.Load() && skipProfiler {
           // 读路径：注入 RWModeRead 到 context
           ctx = context.WithValue(ctx, def.RWModeContextKey, def.RWModeRead)
       }
       // ... 继续执行 ExecuteJob(ctx, job) ...
   }
   ```
   业务层可通过 `ctx.Value(def.RWModeContextKey)` 检测当前是否在 ReadOnly 上下文中执行，
   用于防御性检查（如发现意外的写操作时可主动 panic）。

   **框架层强制约束**：除了业务层自检，框架应在 `Service.PostJob` 入口检测
   调用方的 context 是否携带 `RWModeRead` 标记。如果 ReadOnly handler 内部
   尝试通过 `PostJob`/`Send`/`Call` 等接口投递新 Job（自投递），框架应：
   - **拒绝投递并返回明确错误**（如 `ErrReadOnlyPostJob`）
   - **输出 WARN 日志**，包含调用方方法名和目标方法名，便于定位误标的 ReadOnly 方法

   实现方式：
   ```go
   func (s *Service) PostJob(job inf.IMailboxJob) error {
       // 【RW 安全约束】检测 ReadOnly handler 中的自投递
       // ReadOnly handler 运行在读 goroutine 中（持有 RLock），如果它尝试投递
       // 新的 Write Job 到同一 Service，该 Write Job 最终需要 WLock 执行，
       // 而当前读 goroutine 正持有 RLock —— 虽然不会形成死锁（写 Job
       // 进入队列等待后续处理），但这暗示 ReadOnly handler
       // 存在副作用（触发写操作），应被标记为 Write 而非 Read。
       if s.mailbox.IsRWEnabled() {
           if ctx := job.GetContext(); ctx != nil {
               if mode, ok := ctx.Value(def.RWModeContextKey).(def.RWMode); ok && mode == def.RWModeRead {
                   s.logger.Warnf("ReadOnly handler attempted to PostJob (self-posting detected). "+
                       "This method should NOT be marked as ReadOnly. job_type=%v", job.GetType())
                   return def.ErrReadOnlyPostJob
               }
           }
       }
       // RW 模式下，为 RPC 请求 Job 设置 RWMode
       if s.mailbox.IsRWEnabled() {
           s.setJobRWMode(job)
       }
       return s.mailbox.PostJob(job)
   }
   ```

   > **注意**：此约束仅在 RW 模式下生效。非 RW 模式下 handler 内部的
   > PostJob 是完全安全的（串行执行，Job 进入队列等待下一轮处理）。

3. **注册阶段静态检查**：
   在 `suitableMethods()` 扫描阶段，对标记为 ReadOnly 的方法进行基本的 AST 静态分析（如
   检查方法体是否引用了已知的写操作 API），在注册时即报警告。
   这是编译期+启动期的检查，零运行时开销。

### 10.11 读饥饿风险（不在目标场景讨论范围内）

§6.4 的写饥饿防护机制（`writeRequested` 计数器）理论上存在**对称的读饥饿风险**：
当 `writeRequested` 持续 > 0 时（多个写操作串联到达），所有 Worker 的读路径被持续阻塞。

**结论：此风险不在本方案的适用范围内，无需额外缓解。**

**理由**：
1. **RW 模式的定位是读多写少场景**（§14.2）——排行榜查询、用户信息查询、配置读取等，
   这类服务的写 QPS 远低于读 QPS，`writeRequested` 持续 > 0 的概率极低
2. 读饥饿只在写 QPS 持续高于写吞吐时才可能发生（即 §10.8 中 `写 QPS × 平均写耗时 > 1.0`），
   此时 RW 模式本身已**不适用**，用户应关闭 RW 模式回退到原始串行模型
3. 如果一个服务有大量写操作，它**不应该**开启 RW 模式——这是配置层面的选择，
   而非框架需要解决的运行时问题
4. §10.8 的适用性判断规则已提供明确指导：`写 QPS × 平均写耗时 > 1.0` → **不适合开启 RW**

**监控兜底**：通过 §10.7 的 `rw_write_wait_duration` 指标可观测写排队延迟，
若发现异常增长可作为关闭 RW 模式的依据。

### 10.12 writeRequested 与信号量令牌的时序间隙（已修复）

> **⚠️ 本节描述的原始问题已在 §6.4 的写路径修正中解决。**
> 原问题：`writeRequested.Add(-1)` 在 `rwMu.Unlock()` **之前**执行，导致读路径
> 看到 `writeRequested==0`（退出自旋、获取信号量令牌）后在 `RLock()` 上被仍持有的
> WLock 阻塞，浪费信号量令牌。
>
> 修正后（§6.4）：`writeRequested.Add(-1)` 移到 `rwMu.Unlock()` **之后**执行。
> 读路径看到 `writeRequested==0` 时 WLock 必定已释放，`RLock()` 不会被阻塞，
> 信号量令牌浪费问题不再存在。

**修正后的残留间隙（良性）**：

在 `rwMu.Unlock()` 和 `writeRequested.Add(-1)` 之间存在极短的窗口：
WLock 已释放但 `writeRequested` 仍 > 0，读路径继续自旋等待。
这意味着读路径在 WLock 释放后可能多等几纳秒，但：
- 间隙时间极短（单条原子操作延迟）
- 不影响正确性（仅是微小的额外自旋）
- 不浪费信号量令牌（读路径在自旋中，未获取令牌）

此间隙无需修复，在此记录供实现者参考。

### 10.13 Drain 阶段自投递的死锁风险（补充）

§10.5 分析了 `resizeWorkers` 场景下 Drain 自投递的死锁风险。此处补充**正常 Stop 路径**的分析：

**正常 Stop 路径**：`run()` defer → `pool.rwMu.Lock()` → `DrainAll(safeExec)`

如果 Drain 执行的某个 handler 内部触发了自投递（PostJob to same Service）：
- `Service.PostJob` → `mailbox.PostJob` → `SubmitJob` → 此时 Worker 处于 `closing=true` 状态
- `SubmitJob` 检查 `closing` 标志后返回 error（`ErrMailboxNotRunning`）→ **不构成死锁** ✅

但存在一个更隐蔽的路径：如果 handler 内部做了**同步 RPC 调用同一 Service 的其他方法**：
- 调用链最终走到 `DispatchJob` → 需要 `pool.mu.RLock()`
- 如果此时 `pool.mu` 未被其他人持有 → `RLock` 成功 → `SubmitJob` → closing error → 不死锁
- 但如果此时 `resizeWorkers` 正在执行且持有 `pool.mu.Lock()` → **死锁**（`rwMu.Lock` + `pool.mu.Lock` 交叉等待）

**结论**：正常 Stop 路径本身不会死锁，但与 `resizeWorkers` 并发时存在交叉锁风险。
这进一步印证了 §10.5 方案 1（先 Unlock 后 Stop）的必要性——确保 `pool.mu` 和 `pool.rwMu`
不会同时被持有。

### 10.14 灰度/回滚策略

§10.10 讨论了 ReadOnly 误标的运行时校验，但缺少生产环境的灰度发布和快速回滚方案。

**核心问题**：一旦 ReadOnly 标记错误（把写方法标为 ReadOnly）上线，如何快速止血？

**必须实现的运维能力**：

1. **运行时动态开关**：提供 `WorkerPool.SetRWEnabled(bool)` 方法，允许在**不重启服务**的情况下
   关闭/开启 RW 模式。

   **⚠️ 竞态安全分析**：

   朴素方案（仅 `atomic.Bool` 翻转）存在**不可修复的数据竞争**：
   - Worker A（RW=true）持有 RLock，读 goroutine 正在读共享状态
   - Admin 调用 `enableRW.Store(false)`
   - Worker B 读取 `enableRW=false` → `safeExec(writeJob)` → **无锁直接写共享状态**
   - Worker A 的读 goroutine 与 Worker B 的写操作并发 → **DATA RACE**

   **正确实现：`rwMu.Lock()` + RLock-after-check 协议**：

   ```go
   // 关闭 RW 模式
   func (p *WorkerPool) SetRWEnabled(enabled bool) error {
       if !enabled && p.enableRW.Load() {
           // 步骤 1：获取 WLock，等待所有 RLock 释放 + 阻止新的 RLock 进入
           //  → Lock 返回时保证：所有读 goroutine 已完成 RUnlock，所有写操作已完成 Unlock
           //  → 被阻塞在 RLock/TryLock 上的 Worker 无法继续
           deadline := time.Now().Add(p.stopTimeout)
           for !p.rwMu.TryLock() {
               if time.Now().After(deadline) {
                   // 超时：有泄漏的读 goroutine，不翻转标志，返回错误
                   return ErrRWDisableTimeout
               }
               runtime.Gosched()
           }
           // 步骤 2：持有 WLock 期间翻转标志——此刻无任何 goroutine 访问共享状态
           p.enableRW.Store(false)
           // 步骤 3：释放 WLock
           p.rwMu.Unlock()
           // 被阻塞的 Worker 恢复后：
           // - 读路径 RLock 成功 → 重检查 enableRW → false → RUnlock + 降级 safeExec
           // - 写路径 TryLock 成功 → 正常执行 → 后续 enableRW=false → 走 safeExec
           p.logger.Warnf("RW mode disabled at runtime")
       } else if enabled && !p.enableRW.Load() {
           // 开启 RW 模式：翻转标志前需确保 readSem 已初始化。
           // 如果服务以 EnableRWMode=false 启动（readSem=nil），之后动态开启时
           // readSem 仍为 nil → 读路径跳过信号量获取 → MaxConcurrentReads 硬上限
           // 完全失效 → 高 QPS 读突发下 goroutine 无限堆积。
           // 因此必须在翻转标志前按配置初始化 readSem。
           if p.readSem == nil && p.conf.MaxConcurrentReads > 0 {
               p.readSem = make(chan struct{}, p.conf.MaxConcurrentReads)
           }
           // 后续 Job 在 execWithRW 入口读到 enableRW=true → 走 RW 路径
           // 不存在竞态：之前所有 Job 都走 safeExec（无锁串行），翻转后才开始用锁
           p.enableRW.Store(true)
           p.logger.Warnf("RW mode enabled at runtime, readSem initialized with cap=%d",
               p.conf.MaxConcurrentReads)
       }
       return nil
   }
   ```

   **读路径的必须配合改动**：`execWithRW` 中 `rwMu.RLock()` 成功后、spawn goroutine 之前，
   必须重新检查 `enableRW`，防止在切换窗口内误 spawn 读 goroutine：

   ```go
   w.pool.rwMu.RLock()
   // 【关键】重检查 enableRW：防止在 SetRWEnabled 切换窗口内误 spawn 读 goroutine
   // 时序：WLock 翻转 enableRW → Unlock → RLock 成功 → 此处检查 enableRW
   if !w.pool.enableRW.Load() {
       w.pool.rwMu.RUnlock()
       // 释放轮询循环中已获取的信号量令牌（防止令牌泄漏，详见 §6.4）
       if w.pool.readSem != nil {
           <-w.pool.readSem
       }
       w.safeExec(job)  // 降级为串行执行
       return
   }
   w.inflightReads.Add(1)
   go func() { ... }()
   ```

   此开关可通过 SysCtlJob（系统控制命令）暴露，支持远程切换。

2. **配置热更新兼容**：如果框架已支持配置热更新（config reload），`EnableRWMode` 应可通过
   热更新生效（内部调用 `SetRWEnabled`）。变更时应输出 WARN 日志标注 RW 模式状态变更。

3. **灰度发布建议**：
   - Phase 1：仅在**一个实例**开启 RW 模式，对比观察监控指标（`-race` 测试已通过前提下）
   - Phase 2：灰度扩展到 10%~30% 实例
   - Phase 3：全量发布
   - 任何阶段发现异常 → 通过动态开关立即关闭全部实例的 RW 模式

4. **回滚 SOP**：
   ```
   发现异常 → 通过 SysCtl 发送 "disableRW" 命令到所有实例
           → SetRWEnabled(false) 内部获取 WLock → 等待所有读 goroutine 完成
           → 翻转标志 → Unlock → RW 立即关闭
           → 排查误标的 ReadOnly 方法 → 修复 → 重新走灰度流程
   ```

---

## 11. 实现步骤

> **前置修复（待实现）**：`InvokeJob` 需改为同步执行（移除 goroutine+select 模式），
> 确保 handler 执行完毕后 InvokeJob 才返回。这消除了 handler goroutine 泄漏和
> Job use-after-free 风险，是 RW 模式正确运行的前提。
> 详见 `engine/pkg/core/handler_job.go` 中 `InvokeJob` 的改动说明（§11.1）。

| 步骤 | 改动文件 | 描述 | 复杂度 |
|---|---|---|---|
| 1 | `engine/pkg/def/mailbox.go` | 新增 `RWMode` 类型和常量 | ⭐ |
| 2 | `engine/pkg/interfaces/IMailBox.go` | 新增独立接口 `IRWModeJob`（`IMailboxJob` 不修改） | ⭐ |
| 3 | `engine/pkg/actor/mailbox/job/job.go` | `Job[T]` 新增 `rwMode` 字段和方法，`Reset()` 重置 | ⭐ |
| 4 | `engine/pkg/core/rpc/prefix.go` | 新增 `RpcRo`/`ApiRo` 只读前缀索引和匹配函数 | ⭐ |
| 5 | `engine/pkg/interfaces/` | 新增 `IReadOnlyDeclarer`、`IReadOnlyMethodMgr` 独立接口（不修改 `IMethodMgr`） | ⭐ |
| 6 | `engine/pkg/core/rpc/handler.go` | `suitableMethods()` 识别只读前缀；`MethodMgr` 改用 `methodEntry`（含 readOnly 字段），启动阶段一次性构建（无需加锁）；扫描完所有方法后检查 `IReadOnlyDeclarer` 批量标记 | ⭐⭐ |
| 7 | `engine/pkg/core/service.go` | `Service.PostJob` 中注入 RWMode：仅对 RPC Request 查询 `IsReadOnly` 并设置 `RWModeRead`（响应/回调/其他 Job 类型一律 Write）；新增 `OnJobDiscarded` 回调接口（§10.3） | ⭐⭐ |
| 8 | `engine/pkg/config/define.go` | `MailboxConf` 新增 `EnableRWMode`、`MaxConcurrentReads`（硬上限）、`StopTimeout` | ⭐ |
| 9 | `engine/pkg/actor/mailbox/worker_pool.go` + `worker.go` | WorkerPool 新增 `enableRW`、`rwMu`、`writeRequested`（Int32 计数器）、`readSem`（Mailbox 级共享）；Worker 新增 `inflightReads`（per-Worker）、`pendingJob`（暂存已出队未执行 Job）；实现 `execWithRW()`（统一轮询循环：closed→writeRequested 自旋→信号量硬上限→RLock；写路径 TryLock + 指数退避 + closed 退出暂存 Job） | ⭐⭐⭐ |
| 10 | `engine/pkg/actor/mailbox/worker.go` | `run()` 增加 RW 分支；Stop/Drain 路径中 `w.inflightReads.Wait()` + `StopTimeout` + `pool.rwMu.Lock()` 保护 Drain；discardExec 增加 `OnJobDiscarded` 通知和 WARN 日志（§10.3） | ⭐⭐ |
| 11 | `engine/pkg/actor/mailbox/worker_pool.go` | `fixConf()` 增加 RW 配置校验（`MaxConcurrentReads` 默认 `runtime.NumCPU()*4`） | ⭐ |
| 12 | 中间件/Profiler | ✅ 已确认中间件 OnComplete 全部线程安全；Profiler 在 RW 读 goroutine 中跳过（详见 6.5.1）；**新增 `rw_read_duration` 读耗时统计（§6.5.1 + §10.7，P0 必须）** | ⭐ |
| **13** | **`engine/pkg/actor/mailbox/worker_pool.go`** | **（P0 必须）resizeWorkers 缩容改造：先 Unlock 后 Stop + 原地排空 Drain，消除死锁风险 + 保留 FIFO 因果一致性（§10.5）** | **⭐⭐** |
| **14** | **`engine/pkg/actor/mailbox/worker_pool.go`** | **（P1 必须）`enableRW` 改为 `atomic.Bool`，实现 `SetRWEnabled(bool)` 基于 `rwMu.Lock()` + RLock-after-check 协议的安全动态开关（§10.14）** | **⭐⭐** |
| 15 | 测试 | 单元测试 + 集成测试：读并发、写独占、读写互斥、Stop 安全、resizeWorkers 缩容安全、**读饥饿场景（§10.11）** | ⭐⭐⭐ |
| 16 | 压测 | RW 模式 vs 普通模式吞吐延迟对比 | ⭐⭐ |

**建议分四个 PR**：
- **PR0**（前置）：`InvokeJob` 同步化修复，独立 PR 合入。
- **PR1**（步骤 1-8, 11）：类型定义 + 方法注册层 + 配置层。不改运行时行为，可独立合并和验证。
- **PR2**（步骤 9-10, 12-14）：Worker 运行时 RW 逻辑 + 并发安全审查 + 读耗时统计 + resizeWorkers 改造 + 动态开关。
- **PR3**（步骤 15-16）：测试 + 压测。

### 11.1 前置修复：InvokeJob 同步化

**问题**：当前 `InvokeJob`（`engine/pkg/core/handler_job.go`）使用 goroutine + select 模式执行 handler：

```go
// 当前实现（有问题）
done := make(chan error, 1)
go func() {
    done <- r.safeExec(func() error {
        return handler(ctx, mJob)  // 注意：传的是原始 ctx，不是带超时的 ctxx
    })
}()
select {
case err := <-done:
    return err
case <-ctxx.Done():
    return ctxx.Err()  // 超时返回，但 handler goroutine 仍在运行！
}
```

**四个问题**：

1. **击穿 `rwMu` 保护**：InvokeJob 超时返回后，外层 `safeExec` 返回 → `defer rwMu.RUnlock()` 释放读锁，
   但 handler goroutine 仍在后台执行（读取共享状态）。此时写操作可获取 `rwMu.Lock()` 开始修改状态
   → handler goroutine 与写操作 **数据竞争**。

2. **Job use-after-free**：`safeExec` 的 defer 中 `job.Release()` 将 Job 放回 `sync.Pool`，
   但泄漏的 handler goroutine 仍持有 `mJob` 引用。Pool 回收后该 Job 可能被重新分配给新请求，
   造成两个 goroutine 同时操作同一 Job 实例。

3. **当前串行模型也受影响**：即使在当前串行 Worker 中，InvokeJob 超时后泄漏的 handler goroutine
   与下一个 Job 的 handler goroutine 实际并发执行，破坏了"同 Key 串行"的语义。

4. **超时未传导给 handler**：handler goroutine 内部使用的是**原始 `ctx`**（无超时）而非
   带超时的 `ctxx`。这意味着即使设置了 deadline，handler **根本感知不到超时信号**，
   导致超时机制名存实亡。这是一个独立于 RW 特性的现存 bug，需优先修复。

**修复方案**：将 InvokeJob 改为同步执行，超时通过 context 传导给 handler：

```go
// 修复后（同步执行）
func (r *jobHandlerRegistry) InvokeJob(ctx context.Context, mJob inf.IMailboxJob) error {
    handler, ok := r.handlers[mJob.GetType()]
    if !ok {
        return def.ErrJobHandlerNotFound
    }

    deadline := mJob.GetDeadline()
    if deadline > 0 {
        deadlineTime := time.Unix(deadline, 0)
        timeout := deadlineTime.Sub(timelib.Now())
        if timeout <= 0 {
            return def.ErrJobTimeout
        }
        ctxx, cancel := xcontext.NewWithTimeout(ctx, timeout)
        defer cancel()
        ctx = ctxx
    }

    // 同步执行 handler：
    // 1. InvokeJob 返回时 handler 已完全执行完毕，不存在泄漏的 goroutine
    // 2. 调用方可安全地在 InvokeJob 返回后释放 Job（无 use-after-free）
    // 3. 外层 rwMu 锁的保护范围覆盖 handler 的完整执行生命周期
    // handler 必须 respect context cancellation/timeout 以避免无限阻塞。
    // TODO 这里应该还需要一个回滚机制，如果执行失败，需要回滚数据

    return r.safeExec(func() error {
        return handler(ctx, mJob)
    })
}
```

**关键变化**：
- 移除 goroutine + channel + select 模式，直接同步调用 handler
- 超时通过 `ctx`（带 timeout 的 context）传导给 handler，handler 通过检查 `ctx.Done()` 或使用 `ctx` 做 IO 来响应超时
- handler 不再使用原始 `ctx`，而是使用带超时的 `ctx`，确保超时信号能被感知（修复第 4 个 bug）
- InvokeJob 返回时 handler 一定已完成，不存在泄漏的 goroutine

**⚠️ 同步化引入的新风险：handler 永久阻塞导致 Worker 卡死**：

同步执行意味着：如果 handler 不 respect context cancellation（例如阻塞在无 timeout 的
网络 IO、第三方库的阻塞调用等），整个 Worker 主循环永久卡死：
- main loop 不返回 → defer 不执行 → `inflightReads.Wait()` 不调用
- Worker.Stop() 不返回 → Service 关闭挂起

**必须配套的硬超时看门狗**：

在 `MailboxConf` 中新增 `MaxJobExecutionTime` 配置（默认 30s），在 `safeExecInternal` 中启动看门狗计时器。
超时后记录告警日志 + 指标，标记 Worker 为 sick 状态供 AutoScaler 参考。
看门狗不强制中断 handler（Go 无法安全杀死 goroutine），但提供可观测性，
便于运维发现并介入。

```go
func (w *Worker) safeExecInternal(job inf.IMailboxJob, skipProfiler bool) {
    // ...
    if w.pool.maxJobExecTime > 0 {
        timer := time.AfterFunc(w.pool.maxJobExecTime, func() {
            w.pool.logger.Errorf("Worker %d: handler exceeded hard timeout %v, may be stuck. job=%v",
                w.workerId, w.pool.maxJobExecTime, job)
            // 在 §10.7 的 rw_job_timeout_total 指标中计数
        })
        defer timer.Stop()
    }
    // ...
}
```

> **为什么不保留 goroutine+select 方案**：原方案虽然 Worker 主循环不卡死，
> 但造成的 goroutine 泄漏 + use-after-free + 串行语义破坏更严重。
> 同步执行 + 硬超时看门狗是更安全的 trade-off。

---

## 12. 测试方案

### 12.1 单元测试

```go
// 测试 1：RW 模式关闭时行为不变
func TestWorker_RWDisabled_AllJobsSerial(t *testing.T) {
    // 构造 enableRW=false 的 Worker
    // 投递多个 Read Job，验证仍然串行执行（并发度 = 1）
    // 验证执行顺序严格 FIFO
}

// 测试 2：Read Job 并发执行
func TestWorker_RWEnabled_ReadsConcurrent(t *testing.T) {
    // 构造 enableRW=true 的 Worker
    // 投递 N 个慢 Read Job（sleep 100ms）
    // 使用 atomic 计数器 + channel 确认同时 in-flight 的 goroutine > 1
    // 验证总执行时间远小于 N * 100ms
}

// 测试 3：Write Job 独占执行
func TestWorker_RWEnabled_WriteExclusive(t *testing.T) {
    // 投递 [Read, Read, Write, Read]
    // Read 中记录自己的执行时间段，Write 中记录自己的执行时间段
    // 验证 Write 执行期间没有 Read 并发
}

// 测试 4：读写互斥（竞态检测）
func TestWorker_RWEnabled_ReadWriteMutualExclusion(t *testing.T) {
    // 并发投递大量 Read 和 Write Job
    // Read 读取共享变量，Write 修改共享变量
    // 使用 go test -race 验证无数据竞争
}

// 测试 5：因果一致性
func TestWorker_RWEnabled_CausalConsistency(t *testing.T) {
    // 投递 Write(value=X) → Read → 验证读到 X
    // 重复 1000 次，验证全部通过
}

// 测试 6：Stop 等待 in-flight 读
func TestWorker_RWEnabled_StopWaitsForReads(t *testing.T) {
    // 投递慢 Read（sleep 200ms），立即调用 BeginStop + Wait
    // 验证 Wait 返回时间 >= 200ms（等待了 Read 完成）
    // 验证无 goroutine 泄漏
}

// 测试 7：MaxConcurrentReads 限制
func TestWorker_RWEnabled_MaxConcurrentReads(t *testing.T) {
    // 设置 MaxConcurrentReads=3
    // 投递 10 个慢 Read（sleep）
    // 使用 atomic 计数器验证任意时刻并发度 <= 3
}

// 测试 8：Drain 阶段不走 RW
func TestWorker_RWEnabled_DrainIsSerial(t *testing.T) {
    // 投递多个 Read Job 后立即 Stop（让 DrainAll 处理残留）
    // 验证 Drain 阶段全部串行执行
}

// 测试 9：SetRWEnabled 动态切换竞态安全
func TestWorkerPool_SetRWEnabled_ConcurrentSafety(t *testing.T) {
    // 在高并发读写中动态关闭/开启 RW
    // 使用 go test -race 验证无数据竞争
    // 验证关闭后所有 Job 回退到串行执行
    // 验证开启后 readSem 正确初始化
}

// 测试 10：writeRequested 多 Writer 场景
func TestWorker_RWEnabled_MultipleWriters(t *testing.T) {
    // 4+ Worker 同时发起 Write
    // 验证所有 Write 最终执行完毕
    // 验证 writeRequested 最终归零
    // 验证无数据竞争
}

// 测试 11：readSem 令牌泄漏防护
func TestWorker_RWEnabled_ReadSemNoLeak(t *testing.T) {
    // RLock-after-check 降级路径（enableRW 动态关闭）中验证信号量令牌被正确归还
    // 读 goroutine panic 后验证信号量令牌被正确归还
    // 长时间运行后验证 readSem 容量未异常消耗
}

// 测试 12：StopTimeout + DrainDiscard 降级
func TestWorker_RWEnabled_StopTimeoutDrainDiscard(t *testing.T) {
    // 读 goroutine 故意阻塞 > StopTimeout
    // 验证 Drain 自动降级为 DrainDiscard
    // 验证 discardExec 触发 OnJobDiscarded 通知
    // 验证 Worker.Stop() 在 StopTimeout 后返回（不永久挂起）
}

// 测试 13：resizeWorkers 缩容 + RW
func TestWorkerPool_ResizeWorkers_ShrinkWithRW(t *testing.T) {
    // 缩容期间有 in-flight 读 goroutine
    // 验证 §10.5 的“先 Unlock 后 Stop”方案正确性
    // 验证无死锁（Drain handler 自投递场景）
    // 验证残留 Job 被正确处理
}

// 测试 14：读 goroutine panic 恢复
func TestWorker_RWEnabled_ReadGoroutinePanic(t *testing.T) {
    // Read handler 主动 panic
    // 验证 RLock/信号量/WaitGroup 全部正确释放
    // 验证 RPC 调用方收到错误响应（而非永久等待）
    // 验证后续 Job 正常处理（Worker 未卡死）
}

// 测试 15：ReadOnly 自投递检测
func TestService_ReadOnlyPostJobRejection(t *testing.T) {
    // ReadOnly handler 中调用 PostJob 投递新 Job
    // 验证返回 ErrReadOnlyPostJob 错误
    // 验证 WARN 日志输出
}
```

### 12.2 前缀匹配测试

```go
func TestPrefixIndex_ReadOnlyPrefix(t *testing.T) {
    // 验证 "RpcRoGetUser" 匹配 rpcRoPrefixIndex
    // 验证 "RpcRoGetUser" 也匹配 rpcPrefixIndex（超集）
    // 验证匹配优先级：先检查 ReadOnly 前缀
    // 验证 "RpcUpdateUser" 不匹配 rpcRoPrefixIndex
}

func TestMethodMgr_IsReadOnly(t *testing.T) {
    // 注册 "RpcRoGetUser" 为 readOnly=true
    // 注册 "RpcUpdateUser" 为 readOnly=false
    // 验证 IsReadOnly("RpcRoGetUser") == true
    // 验证 IsReadOnly("RpcUpdateUser") == false
    // 验证 IsReadOnly("UnknownMethod") == false
}
```

### 12.3 压测场景

| 场景 | 比例 | 配置 | 指标 |
|---|---|---|---|
| 纯读 | 100% Read | RW on vs off | 读吞吐提升倍数 |
| 读多写少 | 90% Read / 10% Write | RW on vs off | 综合吞吐和 p99 延迟 |
| 读写各半 | 50% / 50% | RW on vs off | 验证 RW 不退化 |
| 纯写 | 100% Write | RW on vs off | 验证无额外开销 |
| 限流读 | 100% Read | MaxConcurrentReads={4,16,64} | 并发度与吞吐的关系 |

**压测方法**：使用现有 `node_concurrency` bench 框架，新增 `BENCH_TYPE='readOnly'` 和 `BENCH_TYPE='mixed'` 模式。

---

## 13. 业界参考

### 13.1 Microsoft Orleans — `[ReadOnly]` 属性（最直接先例）

Orleans 是 Microsoft 的分布式 Virtual Actor 框架（.NET），其 Grain 默认 **单线程串行**——与 emberengine 的 mailbox 模型完全一致。

```csharp
public interface IMyGrain : IGrainWithIntegerKey
{
    Task<int> IncrementCount(int incrementBy);  // 写方法，串行执行

    [ReadOnly]
    Task<int> GetCount();  // 读方法，可与其他 ReadOnly 并发交错
}
```

**行为语义**：
- 未标记 `[ReadOnly]` → 独占执行
- 标记 `[ReadOnly]` → 可与其他 `[ReadOnly]` 方法并发交错（在 `await` 点），但与写互斥
- 执行仍然是 **单线程** 的，ReadOnly 请求在 `await` 点交错（cooperative concurrency）

**与本方案的区别**：

| 维度 | Orleans `[ReadOnly]` | EmberEngine RW |
|---|---|---|
| 并发方式 | 协程交错（`await` 点，单线程） | 真多 goroutine 并发（多线程） |
| 读吞吐 | 受限于单线程 | 充分利用多核 |
| 对读方法的要求 | 可访问 Actor 状态（单线程安全） | 需保证线程安全只读 |
| 标记方式 | `[ReadOnly]` Attribute | `RpcRo` 前缀 / `IReadOnlyDeclarer` |

Orleans 还提供 `[AlwaysInterleave]`——可与任何请求（含写）并发交错，作为极致吞吐的"逃生舱"。

### 13.2 其他框架对比

| 框架 | 平台 | Actor 串行模型 | 读写分离 | 级别 |
|---|---|---|---|---|
| **Orleans** | .NET | ✅ 默认串行 | ✅ `[ReadOnly]` 属性 | **框架内置** |
| **Akka** | JVM | ✅ 严格串行 | ❌ 需手动拆 Read-Replica Actor | 应用层 |
| **Erlang/OTP** | BEAM | ✅ 严格串行 | ❌ 使用 ETS 表（进程外共享内存） | 应用层 |
| **Proto.Actor** | Go | ✅ 严格串行 | ❌ | 无 |
| **Hollywood** | Go | ✅ 严格串行 | ❌ | 无 |
| **EmberEngine** | Go | ✅ 默认串行 | 🔜 Mailbox 级 RW 增强 | **框架内置** |

**结论**：在 Actor/Mailbox 级别内置读写分离，Orleans 是最直接先例。但 Orleans 是协程交错（单线程），本方案是 **真正的多 goroutine 并发读**，更充分利用 Go 多核优势。在 Go Actor/Service 框架生态中，这是差异化特性。

---

## 14. 使用者收益分析

### 14.1 核心收益

**1. 读多写少服务的"免费"吞吐提升**

用户只需 `RpcGetUser` → `RpcRoGetUser`，配置开启 `enableRWMode: true`，读请求自动并发。不改业务逻辑，不加锁，不理解并发模型。

典型受益服务：排行榜查询、用户信息查询、配置读取、状态查看——这类服务在游戏/分布式系统中 **非常普遍**。

**2. 第三条路：免除"单线程安全 vs 多线程性能"的两难选择**

| 选项 | 读并发 | 安全性 |
|---|---|---|
| 单 Worker（当前） | ❌ 串行 | ✅ 完全安全 |
| 多 Worker（当前） | 异 Key 并发，同 Key 串行 | ⚠️ 依赖业务 |
| **单 Worker + RW（本方案）** | **✅ 同 Key 读并发** | **✅ 完全安全** |

**3. 框架层保证，业务层零心智负担**

用户不需要知道 `sync.RWMutex` 是什么，只需知道：
- 加了 `RpcRo` 前缀 → "我保证这方法不改状态"
- 框架自动处理并发安全

对比用户自己做：每个服务自己管 RWMutex、每个读方法记得加 RLock、忘了就出 bug。框架做这件事的价值是 **业务层零成本**。

### 14.2 适用范围（非银弹）

| 服务特征 | 收益 |
|---|---|
| 读 90%、写 10%（排行榜/用户信息/配置等） | **显著提升** |
| 读写各半 | 有提升，写频繁时 WLock 等待变多 |
| 写为主 | 几乎无收益，写仍然独占串行 |
| 纯计算无状态 | 不需要，多 Worker 已够 |

**不是所有服务都需要开启**。RW 模式是一个 **可选增强**，不是默认模式。

### 14.3 与"不做这个特性"的替代方案对比

| 替代方案 | 可行性 | 问题 |
|---|---|---|
| 加更多 Worker | 只解决异 Key 并发 | 同 Key 读仍串行，根本没解决 |
| 用户自己加 RWMutex | 可以 | 重复劳动 + 容易出错 |
| 拆服务：读服务 + 写服务 | 架构膨胀 | 运维成本翻倍，状态同步复杂 |
| 用缓存/Redis 分流读 | 引入外部依赖 | 一致性窗口、网络开销 |

替代方案要么 **没解决问题** 要么 **成本更高**。框架内置 RW 分离是 **性价比最高** 的方案。

### 14.4 接入成本 vs 收益总结

| 维度 | 评价 |
|---|---|
| 接入成本 | **极低**（改前缀 + 开配置，或实现 `IReadOnlyDeclarer`） |
| 使用者收益 | 读多写少场景显著，通用场景适中 |
| 风险 | 标错读写有安全隐患，但默认安全（不标 = 写） |
| 适用面 | 读多写少服务（游戏里非常常见） |
| 框架差异化 | Go 生态独有，对标 Orleans `[ReadOnly]` |

---

## 15. 一句话定位

> **RW 模式是一个"可选的、低成本的、框架级并发增强"——它不改变现有模型，但给读多写少的服务提供了一条不用改业务逻辑就能提升吞吐的路径。Mailbox 级 RW 增强保持了 DispatcherKey 路由语义和 FIFO 因果一致性，是无侵入式的自然延伸。**
