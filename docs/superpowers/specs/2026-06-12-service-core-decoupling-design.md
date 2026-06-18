# Service Core 接口收紧与状态机重构设计

> 日期：2026-06-12
> 更新：2026-06-15
> 状态：设计草案
> 适用范围：`engine/pkg/core`、`engine/pkg/interfaces`、Service 生命周期与运行时接口边界
> 背景：当前框架处于设计开发阶段，暂无外部使用者，因此允许破坏式内部接口调整。

---

## 1. 背景与重新评估

GitNexus 复评显示 `engine/pkg/core/service.go` 中的 `Service` 是 CRITICAL 影响面核心抽象。早期设计曾尝试把 `Service` 拆成 `serviceRuntime`、`serviceLifecycle`、`serviceDispatch`、`serviceEndpointBinding` 等多个组件，但进一步复核后发现该方向存在“为了拆而拆”的问题。

当前 `Service` 字段虽然较多，但这些字段不是提供给外部直接操作的裸状态，而是 `Service` 封装基础能力时持有的内部承载。用户通过嵌入 `core.Service` 直接应用这些基础能力；框架内部通过接口、Hook、配置和运行时依赖控制其行为。

其中大部分字段并不是未封装的业务逻辑，而是已经独立封装过的框架模块：

- `mailbox.Mailbox` 已封装消息队列、worker、drain、RW 执行；
- `timingwheel.ITimerScheduler` 已封装定时器调度；
- `concurrent.IConcurrent` 已封装并发任务调度；
- `event.Processor` / `event.Handler` 已封装事件触发与订阅；
- `rpc.Handler` / `rpc.MethodMgr` 已封装 RPC 方法注册、调用与只读标记；
- `profiler.IProfiler` 已封装 profiler 生命周期；
- `Module` 已作为服务和子模块共享能力的承载体。

因此真正的问题不是“`Service` 自己实现了所有功能”，也不是“字段太多所以必须拆字段”，而是：

```text
IService 聚合了太多能力，任何拿到 IService 的调用方都获得了完整服务权限。
```

当前 `IService` 同时包含生命周期控制、身份状态、配置访问、Mailbox 投递、Profiler、Logger、RPC 调用等能力。调用方只需要服务身份或投递能力时，也被迫依赖完整 `IService`，导致影响面扩大、权限边界模糊、后续重构风险升高。

本设计将方向调整为：

1. **明确 Service 定位**：`Service` 是基础能力容器与运行时能力组合器，不是外部字段访问对象。
2. **以接口拆分为主**：把 `IService` 拆成窄能力接口，调用方按需依赖。
3. **Service 字段基本保持**：不强行把已封装模块再包一层 `serviceRuntime`。
4. **只提取状态机**：将散落的 `status` / `stopRequested` 收敛到 `serviceState`。
5. **保留 Module 嵌入模型**：`IRpcHandler`、`IConcurrent`、`ITimerScheduler`、`ILoggerX` 等继续通过 `Module` 承载。

---

## 2. 设计目标

### 2.1 目标

- 收紧 `IService` 暴露面，让内部调用方依赖最小必要能力。
- 明确 `Service` 是基础能力容器与运行时能力组合器，字段是内部封装承载，不是对外自由访问面。
- 保留当前 `Service` 作为运行时装配点，不引入虚假的中间组件。
- 保留 `Module` 嵌入能力，避免破坏业务模块直接使用 RPC、日志、定时器、并发调度的习惯。
- 提取 `serviceState`，集中状态迁移和关闭防重入。
- 明确哪些调用方可以使用完整 `IService`，哪些必须迁移到窄接口。
- 为后续 ServiceManager、Endpoint、Event、Router、Mailbox 的依赖收敛提供渐进路径。

### 2.2 非目标

- 不拆分 `Service` 字段到 `serviceRuntime`、`serviceDispatch` 等大对象。
- 不重写 Mailbox、RPC、Event、TimingWheel、Profiler、Module。
- 不改变 `Node → Service → Module` 总模型。
- 不引入 DI 容器。
- 不为了文件变薄而破坏现有封装边界。
- 不把内部字段变成公开可替换状态；自定义能力应通过接口、Hook、Module、配置或明确 setter 进入。
- 暂不处理 `IComponent`，该接口原用于扩展 Service 能力，但后续扩展优先通过 Module 解决。

---

## 3. 核心判断

### 3.1 Service 不是主要问题

`Service` 当前承担三类职责：

1. **运行时装配**：创建 logger、timer、mailbox、event、concurrent、rpc、profiler 等模块。
2. **生命周期编排**：Init / Start / Stop / rollback 顺序控制。
3. **能力门面**：对外提供 `GetPid`、`PostJob`、`GetLogger`、`Select` 等能力。

第一类职责本质上是“把已封装模块装配到一起”。只要每个模块内部边界清晰，字段存在于 `Service` 中并不等同于职责失控。

### 3.2 IService 才是主要问题

当前 `IService` 组合接口过大：

```text
IService
├── ILifecycle
├── IIdentifiable
├── IServiceHandler
├── IMailboxChannel
├── IServiceProfiler
├── ILogger
└── IRpcHandler
```

结果是：

- Endpoint 只需要身份、可见性、状态，却拿到生命周期和 RPC 能力；
- EventBus 只需要 `IListener`，但很多地方仍可能传完整服务；
- ServiceManager 需要生命周期控制，却同时获得 Mailbox、RPC、Logger 等能力；
- 模块或系统服务只想拿 logger/router/node context，也会依赖完整 `IService`；
- 任何 `IService` 方法变更都会放大到全框架。

因此优化重点应从“拆字段”转为“拆接口”。

### 3.3 Service 字段不是能力泄漏

`Service` 字段的存在不等于对外泄漏。当前字段大多是基础能力的内部实现引用：

- 对用户服务而言，`core.Service` 是可嵌入的基础能力容器，用户直接调用其公开方法和嵌入能力；
- 对框架内部而言，`Service` 是 Mailbox、RPC、Timer、Event、Concurrent、Profiler、Logger 等能力的组合器；
- 对调用方而言，真正的边界应该是接口，而不是结构体字段；
- 对自定义需求而言，入口应该是接口、Hook、Module、配置或受控 setter，而不是直接暴露字段。

因此，保留字段布局不是“暂时不拆”，而是符合当前模型的设计选择。只有当某个字段背后的能力本身缺少边界、生命周期或测试时，才应拆该能力模块；否则应优先收紧调用方看到的接口。

---

## 4. 总体架构

重构后，`Service` 仍是核心运行时门面，字段基本保持现状，只增加一个轻量状态机对象。

```text
Service struct
├── Module                              // 保留：RPC / Timer / Concurrent / Logger / Event 等嵌入能力
├── IMessageInvoker                     // 可保留或由 Service 显式实现
├── pid/name/src/cfg/visibility/deps    // 保留：服务元数据和运行时依赖
├── mailbox/eventProcessor/profiler     // 保留：已封装运行时模块引用
├── jobRegistry/sysCtlRegistry/txHookMgr// 保留：Job 执行与系统命令注册
└── state serviceState                  // 新增：状态、关闭请求
```

能力边界通过接口体现：

```text
Service implements
├── IServiceRef       // 身份引用
├── IServiceState     // 状态读取
├── IServiceControl   // 生命周期控制
├── IServiceHooks     // 用户 hook
├── IServiceRuntime   // 运行时上下文访问
├── IServiceRPC       // RPC 发布与路由判断
├── IJobReceiver      // Job 投递
├── IMessageInvoker   // Mailbox 执行回调
├── IServiceProfiler  // Profiler 访问
├── ILogger           // Logger 访问
├── IRpcHandler       // 兼容 Module/RPC 直接调用
└── IMailboxChannel   // 旧 mailbox 投递兼容接口
```

`IService` 迁移期仍作为完整组合接口存在，但新代码不得随意依赖它。

---

## 5. Service 结构调整

### 5.1 保持现有字段布局

不引入 `serviceRuntime`。以下字段继续留在 `Service` 或 `Module` 中，这是设计选择而不是过渡方案：

- `mailbox`
- `eventProcessor`
- `profiler`
- `jobRegistry`
- `sysCtlRegistry`
- `txHookMgr`
- `deps`
- `authorizer`
- `Module.IConcurrent`
- `Module.ITimerScheduler`
- `Module.IRpcHandler`
- `Module.methodMgr`
- `Module.eventHandler`
- `Module.ILoggerX`

理由：这些对象本身已经是封装后的模块，`Service` 持有它们是为了提供基础能力和完成 Init/Start/Stop 时的装配与释放。外部调用方不应直接操作这些字段；如果需要替换或定制，应通过明确接口、Hook、Module、配置或受控 setter 进入。

### 5.2 替换状态相关字段

当前字段：

```text
status int32
stopRequested atomic.Bool
initErr error
```

建议替换为：

```text
state serviceState
```

`serviceState` 内部保存：

```text
status int32
stopRequested atomic.Bool
```

生命周期操作失败时，错误由 `Init`、`Start`、`Stop` 直接返回给调度方/调用方。调度方决定重试、重建、移除、告警或强制退出等策略。`serviceState` 不存储错误对象。

---

## 6. `serviceState` 设计

### 6.1 状态定义

保留当前状态：

```text
SvcStatusUnknown
SvcStatusInit
SvcStatusStarting
SvcStatusRunning
SvcStatusReady
SvcStatusClosing
SvcStatusClosed
SvcStatusRetire
```

不新增失败状态。生命周期操作（Init / Start / Stop）失败时，错误直接返回给调度方/调用方，由调度方决定后续策略。

### 6.2 状态迁移

```text
Unknown -> Init
Unknown -> Closing     // Stop 未初始化实例时保持当前幂等关闭语义
Init -> Starting
Init -> Closed        // Init 失败回滚后
Init -> Closing        // Stop during Init
Starting -> Running
Starting -> Closed    // Start 失败回滚后
Starting -> Closing    // Stop during Starting
Running -> Ready
Running -> Closing
Ready -> Closing
Closing -> Closed
Closed -> Retire
```

### 6.3 建议方法

```text
Load() int32
IsClosed() bool
TryInit() bool
TryStarting() bool
TryClosing() bool
MarkRunning() bool
MarkReady() bool
MarkClosed()
MarkRetire() bool
RequestStop() bool
StoreIfMutable(status int32) bool
```

### 6.4 语义规则

- `RequestStop()` 替代当前 `stopRequested.CompareAndSwap(false, true)`。
- `IsClosed()` 保持当前保守语义：状态大于等于 `Closing` 即视为不可接收普通工作。
- `MarkRunning()` / `MarkReady()` 不允许覆盖 `Closed` / `Retire` 状态。
- `MarkClosed()` 是终态写入方法，用于 Init 失败回滚、Start 失败回滚、Stop 完成清理后。它可以从小于 `Closed` 的状态进入 `Closed`；当前已经是 `Closed` 时应幂等成功；当前是 `Retire` 时必须保持不变，不得回退到 `Closed`。
- `TryClosing()` 允许从 `Unknown`、`Init`、`Starting`、`Running`、`Ready` 进入 `Closing`，用于未初始化实例 Stop、Stop during Init、Stop during Starting 等场景，保持当前 `Stop from Unknown -> Closed` 的幂等关闭语义。实现应使用 CAS 循环，直到成功进入 `Closing`、发现状态已大于等于 `Closing`，或发现不可关闭状态。
- `StoreIfMutable(status)` 仅在当前状态小于 `Closing`、目标状态不同于当前状态且目标状态不回退时写入；`Closing` / `Closed` / `Retire` 后拒绝任何普通状态修改。用于替代旧的 `setStatus` 直接 atomic 写入，避免 `Closing -> Running` 这类回退。

---

## 7. 接口拆分设计

### 7.1 命名原则

- 保持项目已有 `I*` 风格。
- 接口按消费者需要拆分，而不是按 Service 内部字段拆分。
- 能力命名优先，避免 `View`、`Facade` 等架构味过重的名称。
- 新代码优先依赖窄接口，完整 `IService` 仅作为迁移期组合约束。

### 7.2 `IServiceRef`

服务引用能力，用于身份识别、服务发现和日志字段。

```text
IServiceRef
├── GetPid() *actor.PID
├── SetPid(*actor.PID)
├── GetName() string
├── SetName(string)
└── GetPartition() int32
```

适用调用方：Endpoint、Router、EventBus、日志上下文。

### 7.3 `IServiceState`

服务状态读取能力。

```text
IServiceState
├── GetStatus() int32
└── IsClosed() bool
```

适用调用方：health/ready、Endpoint ready 判断、诊断快照。

### 7.4 `IServiceControl`

框架内部生命周期控制能力。

```text
IServiceControl
├── Init(src interface{}, conf *config.ServiceInitConf, cfg interface{}) error
├── Start() error
└── Stop() error
```

`Stop() error` 是本轮目标签名，实施时同步迁移 `Service.Stop`、`IServiceControl`、`ILifecycle` 以及所有调用方。现有 `Stop()` 调用点必须处理或显式忽略 error。

适用调用方：ServiceManager、Node 启停流程。

### 7.5 `IServiceHooks`

用户服务 hook 能力。

```text
IServiceHooks
├── OnInit() error
├── OnStart() error
├── OnStarted() error
└── OnRelease()
```

适用调用方：`Service.Init` / `Service.Start` / `Service.release` 内部调用用户扩展点。

### 7.6 `IServiceRuntime`

服务运行时上下文访问能力。

```text
IServiceRuntime
├── GetServiceCfg() interface{}
├── GetMailbox() IMailbox
├── GetNodeContext() INodeContext
└── GetRouter() INodeRouter
```

适用调用方：模块、系统服务、少量需要上下文的运行时组件。日志访问由独立的 `ILogger` 能力负责，避免 `IServiceRuntime` 同时承担 logger 边界。

`GetMailbox()` 当前暂留在 `IServiceRuntime` 中以降低迁移面，但 Endpoint 发布路径只需要 mailbox 投递能力。实施时可优先新增更窄的 mailbox provider 接口，避免 Endpoint 因 `GetMailbox()` 被迫获得 `GetServiceCfg()`、`GetNodeContext()`、`GetRouter()`：

```text
IServiceMailboxProvider
└── GetMailbox() IMailbox
```

迁移顺序采用保守策略：阶段 1 仅新增 `IServiceMailboxProvider`，`IServiceRuntime` 暂时继续保留 `GetMailbox()`；阶段 3 将 Endpoint 等只需要 mailbox 的调用方迁移到 `IServiceMailboxProvider`；后续审计确认没有 runtime 消费方依赖 `GetMailbox()` 后，再评估是否从 `IServiceRuntime` 中移除该方法。

### 7.7 `IServiceRPC`

RPC 发布与服务可见性能力。

```text
IServiceRPC
├── GetRpcHandler() IRpcHandler
├── IsPrivate() bool
├── IsRemoteCallable() bool
├── GetVisibility() def.ServiceVisibility
└── IsPrimarySecondaryMode() bool
```

适用调用方：Endpoint、Router、RPC 注册和权限检查。

`IsPrivate()` 必须保留，它当前属于 `IServiceHandler`，是可见性判断的一部分。

### 7.8 Endpoint 专用组合接口

Endpoint 发布路径使用两个更贴近 concrete `EndpointManager` 调用场景的组合接口：

```text
IServiceEndpointPublisher = IServiceRef + IServiceState + IServiceRPC + IServiceMailboxProvider
IServiceEndpointLifecycle = IServiceRef + IServiceRPC
```

`IServiceEndpointPublisher` 包含 `IServiceMailboxProvider` 是因为当前发布流程需要 `GetMailbox()` 创建本地 dispatcher；`IServiceEndpointLifecycle` 用于 Remove / ToNodeService 等不需要状态和 mailbox 的路径。`INodeEndpointManager` 与 concrete `EndpointManager` 的方法签名必须保持一致，实施时同步改为：

```text
INodeEndpointManager
├── CreatePid(partition int32, serviceId, serviceType, serviceName string, version int64, rpcType string) *actor.PID
├── AddService(IServiceEndpointPublisher)
├── ServiceReady(IServiceEndpointPublisher)
├── RemoveService(IServiceEndpointLifecycle)
└── ToNodeService(IServiceEndpointLifecycle)
```

`CreatePid` 参数均为基础类型，不接收 `IService`，无需改为窄接口签名。

### 7.9 `IJobReceiver`

Job 接收能力。

```text
IJobReceiver
└── PostJob(job IMailboxJob) error
```

适用调用方：Router、EventBus、其他服务向目标服务投递 Job 的路径。

命名使用 `Receiver`，因为从调用方视角目标服务是接收者。

`IJobReceiver` 与现有 `IMailboxChannel` 方法签名相同，新增它主要是为了在 Router、Discovery watcher、跨服务投递等调用方参数中表达“目标服务接收 Job”的业务语义；底层 dispatcher/mailbox 仍可继续使用 `IMailboxChannel`。如果实施阶段希望进一步降低命名负担，也可以先复用 `IMailboxChannel`，但新代码应避免因投递能力而依赖完整 `IService`。

### 7.10 `IMessageInvoker`

Mailbox 执行回调能力，实施阶段保留现有命名，避免无意义重命名。

```text
IMessageInvoker
├── GetServiceName() string
├── ExecuteJob(ctx context.Context, job IMailboxJob) error
├── EscalateFailure(ctx context.Context, reason interface{}, job IMailboxJob)
└── OnJobDiscarded(job IMailboxJob, reason error)
```

适用调用方：Mailbox / WorkerPool / Worker。

该能力仍由 `Service` 实现，不单独抽 `serviceDispatch`。原因是 `ExecuteJob` 需要访问 `jobRegistry`、`txHookMgr`、logger 等 `Service` 已持有字段，额外抽对象只会制造跨组件耦合。

### 7.11 `IServiceProfiler`

保留现有 profiler 能力接口。

```text
IServiceProfiler
├── OpenProfiler()
└── GetProfiler() IProfiler
```

### 7.12 `IService` 组合接口

迁移期保留完整 `IService`，但重新定义为窄接口组合：

```text
IService =
  IServiceRef
  + IServiceState
  + IServiceControl
  + IServiceHooks
  + IServiceRuntime
  + IServiceRPC
  + IServiceMailboxProvider
  + IJobReceiver
  + IMessageInvoker
  + IServiceProfiler
  + ILogger
  + IRpcHandler
```

保留 `IRpcHandler` 的原因：`Module` 当前嵌入 `IRpcHandler`，业务模块存在直接 `Select(...)`、`HandleRequest(...)` 等调用习惯。强制改成 `GetRpcHandler().Select(...)` 会扩大变更面，且收益有限。

`IServiceRuntime.GetMailbox()` 与 `IServiceMailboxProvider.GetMailbox()` 是同一个方法，Go 嵌入接口中相同签名自动合并。`IServiceMailboxProvider` 是为 Endpoint 等只需要 mailbox 的调用方提供的更窄视角；两者在 `IService` 组合中不产生冲突。

### 7.13 旧接口兼容形态

以下三个旧接口在重构后保留为 Deprecated 兼容组合接口，确保现有代码无需立即迁移：

```text
ILifecycle = IServiceControl + IServiceHooks
IServiceHandler = IServiceRuntime + IServiceRPC
IIdentifiable = IServiceRef + IServiceState
```

- `ILifecycle`：原为生命周期控制 + 用户 Hook 的组合，拆分为 `IServiceControl`（框架侧）和 `IServiceHooks`（用户侧）后，仍作为兼容组合保留。
- `IServiceHandler`：原为运行时上下文 + RPC 可见性的组合，拆分为 `IServiceRuntime` 和 `IServiceRPC` 后，仍作为兼容组合保留。
- `IIdentifiable`：原为身份引用 + 状态读取的组合，拆分为 `IServiceRef` 和 `IServiceState` 后，仍作为兼容组合保留。

新代码应按场景直接使用对应的窄接口，不应新增对这三个兼容接口的依赖。

---

## 8. 调用方迁移策略

### 8.1 允许继续依赖完整 `IService` 的位置

- Service 构造和注册入口；
- `Module.GetService()` 的返回值；
- 用户服务约束；
- 迁移期尚未收敛的旧代码。

实施阶段必须审计所有剩余 `IService` 使用点，并按以下规则处理：

- 如果调用方需要用户服务完整行为、`Module.GetService()` 兼容能力或迁移期完整约束，可以保留 `IService`；
- 如果调用方只读取身份、状态、可见性、运行时上下文或 Job 投递能力，必须改为对应窄接口；
- 无法立即收窄的位置必须在代码注释或后续任务中说明原因；
- Endpoint 发布路径已通过 `IServiceEndpointPublisher` / `IServiceEndpointLifecycle` 收窄，且 `INodeEndpointManager` 必须与 concrete `EndpointManager` 保持签名一致。

### 8.2 应迁移到窄接口的位置

| 调用方 | 目标接口 | 原因 |
|------|----------|------|
| ServiceManager 启停流程 | `IServiceControl` | 只需要 Init/Start/Stop |
| EndpointManager 发布路径 | `IServiceEndpointPublisher` | 需要身份、状态、可见性和 `GetMailbox()` |
| EndpointManager 下线/节点服务转换路径 | `IServiceEndpointLifecycle` | 只需要身份和 RPC 可见性信息 |
| EventBus / EventProcessor | `IListener` 或 `IJobReceiver + IServiceRef` | 只需要投递和身份 |
| Router 目标投递 | `IJobReceiver + IServiceRef` | 只需要定位和投递 |
| etcd discovery watcher | `IServiceRef + IServiceState + IServiceRPC + IJobReceiver + ILogger` | 只需要身份、状态、可见性、投递和日志 |
| Mailbox | `IMessageInvoker` | 只需要执行回调 |
| health/ready | `IServiceRef + IServiceState` | 只需要身份和状态 |
| RPC 发布 | `IServiceRef + IServiceRPC` | 只需要 RPC handler 和可见性 |

Discovery watcher 迁移时还应顺手检查 `PostJob` 失败路径的 Job ownership：当前契约是 `PostJob` 拥有 Job 所有权，失败路径由实现方完成 Release + OnJobDiscarded，调用方不应在 error 返回后再次 `Release()`。当前 watcher 主要使用 `GetPid()`、`GetName()`、`GetVisibility()`、`GetStatus()`、`IsPrimarySecondaryMode()`、`PostJob()` 和 `GetLogger()`，目标接口组合应覆盖这些使用点。

### 8.3 不做一次性全量替换

接口拆分应分阶段执行：

1. 先新增窄接口，保持 `IService` 兼容。
2. 在核心调用方逐步替换参数类型。
3. 每替换一个调用域，补对应测试。
4. 最后收敛 `IService` 的使用位置，作为完整服务约束保留。

---

## 9. 生命周期设计调整

### 9.1 Init

`Init` 仍由 `Service` 编排，不新增 `serviceLifecycle` 结构。可以继续保留 `service_init.go` 中的子方法拆分。

建议流程：

```text
Init
├── validate node logger / svc / conf
├── state.TryInit()
├── fixConf(conf)
├── bind src/cfg/name
├── initLogger
├── init stop policy
├── initTimers
├── initMailbox
├── initModule
├── initEvents
├── initConcurrent
├── initJobHandlers
├── initSysCtlRegistry
├── initPID
├── initRPC
├── initVisibility
├── hooks.OnInit
└── success
```

`Init` 参数签名迁移期仍保持 `Init(src interface{}, ...)`，但实现中不得再直接 `svc.(inf.IService)` 造成 panic。应使用安全断言验证用户服务满足完整 `IService` 约束，失败时返回 error 并进入 Init 失败回滚路径。

失败回滚：

```text
rollbackInitResources
├── stop timer
├── close concurrent
├── nil mailbox/event/rpc/method fields
├── clear pid/visibility/logger
├── close logger if owned
├── state.MarkClosed()
└── return error
```

Init 失败后同一个 `Service` 实例进入 `Closed`，调度方/调用方如需重试应创建新实例，而不是复用半初始化实例。

### 9.2 Start

`Start` 仍由 `Service` 实现，但状态操作改走 `serviceState`。

```text
Start
├── state.TryStarting()
├── mailbox.Start()
├── go startListenCallback()
├── hooks.OnStart()
├── mark master if needed
├── state.MarkRunning()
├── endpointManager.AddService(s)
├── hooks.OnStarted()
├── state.MarkReady()
├── endpointManager.ServiceReady(s)
└── success
```

Start 失败：

```text
rollbackStart
├── RemoveService if registered
├── mailbox.Stop()
├── timer.Stop()
├── concurrent.Close()
├── releaseWithEndpoint(false)
├── close logger if owned
└── return error
```

`rollbackStart` 只负责资源回滚，不负责最终状态迁移。实施时必须移除旧 `rollbackStart` 末尾直接写 `SvcStatusClosed` 的逻辑；`OnStart` 失败、endpoint manager 为空、`OnStarted` 失败三个分支必须在调用 `rollbackStart(...)` 后由调用方执行 `state.MarkClosed()`，再返回原始错误。

### 9.3 Stop

目标形态：`Stop() error`。
本轮实施同步迁移 `Service.Stop`、`IServiceControl`、`ILifecycle` 以及所有调用方；不保留旧 `Stop()` 作为长期兼容入口。调用方必须处理或显式忽略返回的 error。

`Stop() error` 的影响面包括 `ServiceManager.Start()` 启动失败回滚、`ServiceManager.StopAll()`、Node cleanup 注册的 “stop all services” 步骤、现有生命周期测试以及所有手写 `IService` mock/fake。`StopAll()` 建议同步升级为返回聚合错误（例如 joined error），Node cleanup 负责记录或向上返回该错误，避免服务停止失败被静默吞掉。

建议流程：

```text
Stop
├── if !state.RequestStop() return nil
├── if !state.TryClosing()
│   ├── if state.Load() >= SvcStatusClosing return nil
│   └── return error or retry according to policy
├── mailbox.Suspend()
├── timer.Stop()
├── concurrent.Close()
├── releaseWithEndpoint(true)
│   ├── hooks.OnRelease()
│   ├── closeProfiler()
│   └── endpointManager.RemoveService(s)
├── close logger if owned
├── state.MarkClosed()
└── return error
```

`releaseWithEndpoint` 仍需要 recover，但 recover 粒度必须按资源释放步骤拆开：用户 `OnRelease` panic 只能转为记录/返回错误，不得阻断后续 `closeProfiler()`、`endpointManager.RemoveService(s)` 和 logger close。Stop 进入 `Closing` 后必须尽力执行所有释放步骤，最后进入 `Closed` 并返回聚合错误。

正常 Stop 与 Start 失败回滚的 mailbox 策略不同：正常 Stop 先 `Suspend()`，后续释放流程按 mailbox drain/stop 策略完成；Start rollback 发生在服务尚未稳定 ready 的阶段，可直接调用 `mailbox.Stop()` 终止已启动的 mailbox。

---

## 10. Module 与用户服务模型

继续支持：

```go
type MyService struct {
    core.Service
}
```

`Module` 保持当前嵌入模型：

```text
Module
├── concurrent.IConcurrent
├── timingwheel.ITimerScheduler
├── inf.IRpcHandler
├── log.ILoggerX
├── methodMgr
└── eventHandler
```

不把这些字段迁移到新结构中。原因：

- 它们已经是独立封装模块；
- 业务模块直接使用嵌入能力是当前 API 设计的一部分；
- 迁移字段会制造大量转发方法；
- 字段物理位置移动不能显著降低耦合，接口收紧才是主要收益。

`Module.GetService()` 迁移期继续返回 `IService`。未来如果完整 `IService` 使用点已经收敛，可以评估是否新增更窄的 `GetServiceRuntime()` 或 `GetServiceRef()`，但本阶段不做。

### 10.1 自定义扩展入口

自定义能力不通过直接暴露 `Service` 内部字段实现，而按需求选择以下入口：

- **生命周期行为**：通过 `OnInit`、`OnStart`、`OnStarted`、`OnRelease` 等 Hook 定制；
- **业务能力扩展**：通过嵌入 `core.Module` 或在服务下注册子模块实现；
- **框架依赖替换**：通过窄接口和受控 setter 注入，例如 runtime deps、authorizer、middleware；
- **运行参数定制**：通过 `config.ServiceInitConf`、Mailbox 配置、Timer 配置、日志配置、StopPolicy 配置；
- **消息处理能力**：通过 Job handler、SysCtl handler、RPC handler 注册机制扩展；
- **不推荐方式**：直接把 `mailbox`、`eventProcessor`、`profiler`、`jobRegistry` 等内部字段公开给外部修改。

该规则使 `Service` 既能保持基础能力容器定位，又不会把内部实现细节变成外部依赖。

---

## 11. 错误处理与诊断

### 11.1 生命周期错误策略

- `Init()`、`Start()`、`Stop()` 的操作失败直接以 `error` 返回给调度方/调用方。
- `serviceState` 不保存 `initErr`、`startErr`、`stopErr` 等错误对象。
- 不引入 `SvcStatusInitFailed`、`SvcStatusStartFailed`、`SvcStatusStopFailed`。
- 调度方/调用方负责决定 retry、rebuild、remove、alert 或 force close 等策略。
- `Stop()` 进入 `Closing` 后必须尽力释放所有资源。若释放过程中出现 error，仍应在完成尽力释放后进入 `Closed`，并将 error 返回给调用方；不引入 StopFailed 状态。
- `ServiceManager.StopAll()` 不应吞掉单个服务 `Stop()` 的错误；应聚合后返回或由上层 cleanup 统一记录。

### 11.2 回滚原则

- Init 失败不允许服务进入 endpoint。
- Start 失败如果已 AddService，必须 RemoveService。
- Stop 失败必须尽力释放所有资源。
- logger close 必须幂等或由调用方避免重复 close。
- `OnRelease` panic 不得阻断 profiler close 和 endpoint remove。
- `PostJob` 失败后调用方不得再次释放 Job，除非目标接口明确不接管所有权；迁移 discovery watcher 等投递路径时必须复核该契约。

---

## 12. 测试策略

### 12.1 接口编译期测试

新增或调整编译期断言：

```text
var _ inf.IService = (*Service)(nil)
var _ inf.IServiceRef = (*Service)(nil)
var _ inf.IServiceState = (*Service)(nil)
var _ inf.IServiceControl = (*Service)(nil)
var _ inf.IServiceHooks = (*Service)(nil)
var _ inf.IServiceRuntime = (*Service)(nil)
var _ inf.IServiceRPC = (*Service)(nil)
var _ inf.IServiceMailboxProvider = (*Service)(nil)
var _ inf.IServiceEndpointPublisher = (*Service)(nil)
var _ inf.IServiceEndpointLifecycle = (*Service)(nil)
var _ inf.IJobReceiver = (*Service)(nil)
var _ inf.IMailboxChannel = (*Service)(nil)
var _ inf.IMessageInvoker = (*Service)(nil)
var _ inf.IServiceProfiler = (*Service)(nil)
var _ inf.ILogger = (*Service)(nil)
var _ inf.IRpcHandler = (*Service)(nil)
var _ inf.IListener = (*Service)(nil)
```

`EndpointManager` 对 `INodeEndpointManager` 的断言应放在 `engine/pkg/cluster/endpoints` 包内：

```text
var _ inf.INodeEndpointManager = (*EndpointManager)(nil)
```

`IJobReceiver` 与 `IMailboxChannel` 当前方法签名相同，新增 `IMailboxChannel` 断言用于保证旧 mailbox 投递能力兼容。

### 12.2 `serviceState` 单元测试

覆盖：

- Unknown → Init → Starting → Running → Ready → Closing → Closed；
- 重复 Init / Start / Stop；
- Start before Init；
- Stop during Starting；
- RequestStop 只允许第一次成功；
- `MarkRunning()` / `MarkReady()` 不能覆盖 `Closed` / `Retire`；
- `Closed -> Retire` 成功，`Retire` 后禁止回退；
- Init 失败回滚后 `Init -> Closed`；
- Start 失败回滚后 `Starting -> Closed`。

### 12.3 生命周期回归测试

覆盖：

- logger 初始化失败；
- timer 初始化失败；
- mailbox 初始化失败；
- pid 创建失败；
- rpc handler 初始化失败；
- `OnInit` 失败；
- mailbox start 后 `OnStart` 失败；
- endpoint registered 后 `OnStarted` 失败；
- Stop 重复调用；
- Stop from Unknown / Init 的语义明确并有测试覆盖；
- Stop 中 `OnRelease` panic 不阻断 endpoint remove；
- Stop 返回聚合错误时仍进入 `Closed`；
- `ServiceManager.StopAll()` 正确处理并暴露服务停止错误；
- `Init` 传入非 `IService` 实现时返回 error，不发生 panic。

### 12.4 接口迁移测试

覆盖：

- ServiceManager 可以只依赖 `IServiceControl` 完成启停；
- Endpoint 发布路径可以只依赖 `IServiceEndpointPublisher`，下线路径可以只依赖 `IServiceEndpointLifecycle`；
- EventBus 继续通过 `IListener` 工作；
- Discovery watcher 不再依赖完整 `IService`，且 `PostJob` 失败路径不重复释放 Job；
- Mailbox 继续通过 `IMessageInvoker` 执行 Job；
- 模块中 `Select(...)`、`GetLogger()`、timer/concurrent 能力保持可用。

### 12.5 Dispatch / Job ownership 回归测试

保持现有语义测试：

- ReadOnly 自投递拒绝；
- ReadOnly 跨服务投递允许；
- RW disabled 不检查；
- RPC read method 设置 `RWModeRead`；
- RPC write method 保持 `RWModeWrite`；
- reply envelope 不设置 Read；
- nil envelope 不 panic；
- mailbox rejected 后 job ownership 正确。

---

## 13. 分阶段实施

### 阶段 1：新增窄接口并同步生命周期签名

- 修改 `engine/pkg/interfaces/IService.go`。
- 增加 `IServiceRef`、`IServiceState`、`IServiceControl`、`IServiceHooks`、`IServiceRuntime`、`IServiceRPC`、`IServiceMailboxProvider`、`IServiceEndpointPublisher`、`IServiceEndpointLifecycle`、`IJobReceiver`。
- 将 `Stop()` 同步迁移为 `Stop() error`，更新 `Service.Stop`、`ILifecycle`、`IServiceControl`、`ServiceManager.Start` 回滚、`ServiceManager.StopAll`、Node cleanup 和直接调用点。
- 保留 `IService` 作为组合接口。
- 增加编译期断言。

### 阶段 2：提取 `serviceState`

- 新增 `engine/pkg/core/service_state.go`。
- 将 `status`、`stopRequested` 替换为 `state serviceState`。
- 移除 `initErr` 字段，并删除 `Start()` 中基于 `initErr` 的启动守卫。Init 失败后同实例通过 `state.MarkClosed()` 进入终态；后续 `Start()` 只需基于当前状态不是 `Init` 返回错误，不再读取历史 init error。
- 更新 `Init` / `Start` / `Stop` / `IsClosed` / `GetStatus`。

### 阶段 3：迁移核心调用方到窄接口

- ServiceManager 启停路径改用 `IServiceControl`。
- Endpoint 发布路径改用 `IServiceEndpointPublisher`，下线/节点服务转换路径改用 `IServiceEndpointLifecycle`。
- `INodeEndpointManager` 与 concrete `EndpointManager` 的 AddService / ServiceReady / RemoveService / ToNodeService 签名同步改为窄接口。
- etcd discovery watcher 改用 `IServiceRef + IServiceState + IServiceRPC + IJobReceiver + ILogger` 组合能力，并修正 `PostJob` 失败后重复 `Release()` 的所有权问题。
- Mailbox 保持 `IMessageInvoker`。
- EventBus 保持 `IListener`，必要时减少完整 `IService` 传递。

### 阶段 4：整理文档与审计完整 `IService` 使用点

- 搜索剩余 `inf.IService` 参数和字段。
- 判断是否确实需要完整服务能力。
- 能改窄接口的逐步改；不能改的保留并注明原因。

---

## 14. 推荐文件布局

```text
engine/pkg/core/
├── service.go              // Service facade、Start/Stop、getter、PostJob
├── service_init.go         // Init 编排及初始化子方法
├── service_state.go        // 新增：状态机
├── handler_job.go          // Job registry、ExecuteJob、tx hooks 调用保持在此域
├── service_profiler.go     // profiler 保持现状
└── service_*_test.go       // 状态机、生命周期、接口断言测试

engine/pkg/interfaces/
├── IService.go             // 窄接口与 IService 组合
├── IMailBox.go             // IMessageInvoker / IMailboxChannel 保持
└── IModule.go              // Module.GetService 迁移期保持返回 IService
```

不新增 `service_runtime.go`、`service_dispatch.go`、`service_endpoint.go`。如果后续某个职责域继续膨胀，再基于真实内聚关系拆分，而不是预先抽象。

---

## 15. 风险与缓解

| 风险 | 级别 | 缓解 |
|------|------|------|
| 接口拆分后调用方改动遗漏 | 中 | 分阶段迁移；先新增接口和断言，再逐域替换 |
| `Stop() error` 改动影响面 | 中 | 统一迁移 `Stop() error`；调用方必须处理或显式忽略 error；通过编译错误驱动补齐遗漏调用点 |
| `StopAll()` 或 Node cleanup 吞掉服务停止错误 | 中 | `StopAll()` 返回聚合错误；Node cleanup 负责记录或上抛，测试覆盖启动失败回滚和正常停止 |
| 完整 `IService` 继续被滥用 | 中 | 每阶段搜索 `inf.IService` 参数，能收窄就收窄 |
| 状态迁移并发竞争 | 低中 | `serviceState` 使用 CAS/atomic 保护状态迁移；通过 race 测试验证 |
| 通用状态写入导致关闭态回退 | 中 | `StoreIfMutable` 禁止 `Closing` 后修改并禁止状态回退；优先使用显式 Mark/Try 方法 |
| `OnRelease` panic 中断后续释放 | 中 | 分段 recover，确保 profiler、endpoint、logger 仍尽力释放，并返回聚合错误 |
| Job ownership 迁移时重复释放 | 中 | 复核所有 `PostJob` error 分支，遵守 `PostJob` 接管所有权契约 |
| 误把字段物理位置当作职责边界 | 高 | 本设计明确不拆已封装模块，只拆接口与状态机 |

---

## 16. 成功标准

- `IService` 被拆成清晰的窄能力接口。
- 新代码和核心内部调用不再默认依赖完整 `IService`。
- `Service` 字段布局基本保持，不引入 `serviceRuntime` 等假抽象。
- 状态迁移集中到 `serviceState`。
- Init / Start / Stop 失败将错误返回给调度方，由调度方决定后续策略。
- `Module` 嵌入 RPC、日志、timer、concurrent 的使用方式保持可用。
- `IMessageInvoker`、`IListener`、`IRpcHandler` 兼容现有调用路径。
- `go test ./engine/pkg/core/...` 通过。
- 关键包 race 测试通过。
- GitNexus 复评时，`IService` 的影响面降低，`Service` 作为装配门面的风险解释更清晰。

---

## 17. 结论

推荐采用“Service 保持基础能力容器定位，接口收紧优先，状态机轻量提取”的方案。

该方案承认当前 Mailbox、RPC、Event、Timer、Concurrent、Profiler 已经是独立封装模块，`Service` 持有这些字段是为了组合基础能力，并不等同于能力泄漏。真正需要降低的是 `IService` 的能力暴露面，以及生命周期状态散落造成的诊断和回滚不清晰。

因此，本轮重构不应强行拆 `Service` 字段，而应：

1. 保持 `Service` 的基础能力容器与运行时能力组合器定位；
2. 通过接口、Hook、Module、配置和受控 setter 支持自定义；
3. 拆分窄接口；
4. 迁移调用方到最小能力依赖；
5. 提取 `serviceState`；
6. 补齐测试。

这是改动最小、收益明确、符合现有代码结构的重构路径。
