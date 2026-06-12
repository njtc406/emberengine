# Service Core 解耦重构设计

> 日期：2026-06-12<br>
> 状态：设计草案<br>
> 适用范围：`engine/pkg/core`、`engine/pkg/interfaces`、Service 生命周期与运行时装配<br>
> 背景：当前框架处于设计开发阶段，暂无外部使用者，因此允许破坏式内部接口调整。

---

## 1. 背景与问题

GitNexus 复评显示 `engine/pkg/core/service.go` 中的 `Service` 为 CRITICAL 影响面核心抽象。当前问题不是单点缺陷，而是 `Service` 同时承担了过多职责：生命周期状态机、初始化编排、启动/停止/回滚、Mailbox、Timer、Concurrent、Event、RPC、Profiler、Logger、Endpoint 注册、用户 Hook 调用等。

这会导致：

- `Service` 字段或方法语义变化容易影响全框架；
- `Init` / `Start` / `Stop` 的失败边界和回滚路径难以单独验证；
- `IService` 聚合接口过大，调用侧被迫依赖完整服务能力；
- 后续接入审计、OpenTelemetry、DrainPolicy、热加载时会继续膨胀；
- 只补测试可以提升稳定性，但无法降低长期结构风险。

因此本设计采用“分阶段解耦，不强求兼容”的方案：保留 `Node → Service → Module` 总模型和已验证的 Mailbox/RPC/Event/TimingWheel 机制，把 `Service` 拆成稳定门面、生命周期编排、运行时组件集合、Endpoint 绑定和投递路径。

---

## 2. 设计目标

### 2.1 目标

- 将 `Service` 从“大编排对象”收敛为“对外门面”。
- 集中生命周期状态迁移，避免散落的 CAS 和状态判断。
- 集中 runtime 资源所有权，明确创建、启动、停止、释放边界。
- 明确 Endpoint/PID 注册、Ready、Remove 的阶段语义。
- 隔离 `PostJob` 热路径和 RW 语义，保证 job ownership 不被破坏。
- 拆分 `IService` 大接口，使内部调用按需依赖窄接口。
- 新增失败状态，提升诊断和可观测性。
- 为后续审计、OTel、DrainPolicy、热加载提供清晰扩展点。

### 2.2 非目标

- 不重写 `Node → Service → Module` 总模型。
- 不重写 Mailbox、RPC Handler、EventBus、TimingWheel。
- 不引入复杂 DI 容器。
- 不做性能优化型重写。
- 不一次性改造所有 Node/Cluster/RPC 架构。

---

## 3. 总体架构

重构后，`Service` 保持为对外入口，但内部职责拆分为多个边界明确的小组件。

```text
Service facade
├── serviceCore             // 元数据、配置、状态、用户实现引用
├── serviceLifecycle        // Init/Start/Stop/rollback 编排
├── serviceRuntime          // logger/mailbox/timer/concurrent/event/rpc/profiler/authz
├── serviceEndpointBinding  // PID、Endpoint Add/Ready/Remove、主从标记
└── serviceDispatch         // PostJob、RWMode、Timer/Concurrent callback job
```

原则：

- `Service` 对外保留常用方法，内部尽量转发。
- `serviceLifecycle` 决定什么时候构建、启动、停止和回滚。
- `serviceRuntime` 拥有运行时资源。
- `serviceEndpointBinding` 拥有服务发现注册语义。
- `serviceDispatch` 拥有 job 投递和 RW 语义。
- 内部调用尽量依赖能力接口，而不是完整 `IService`。

---

## 4. 组件设计

### 4.1 `Service` facade

`Service` 是对外门面，继续支持用户服务嵌入：

```go
type MyService struct {
    core.Service
}
```

`Service` 保留：

- `Module` 根能力；
- 身份和状态查询；
- 配置查询；
- logger 获取；
- mailbox 获取；
- RPC handler 获取；
- `Init` / `Start` / `Stop` 入口；
- `PostJob` 入口。

`Service` 不再直接承担：

- runtime 资源创建；
- 复杂生命周期编排；
- rollback 细节；
- endpoint 注册与反注册细节；
- callback listener 细节；
- logger/profiler/timer/concurrent 释放细节。

预期字段结构示意：

```text
type Service struct {
    Module
    invoker IMessageInvoker

    core      *serviceCore
    runtime   *serviceRuntime
    lifecycle *serviceLifecycle
    endpoint  *serviceEndpointBinding
    dispatch  *serviceDispatch
}
```

字段名可在实施阶段按现有代码习惯微调，但职责边界应保持稳定。

### 4.2 `serviceCore`

`serviceCore` 保存稳定元数据和状态，不直接创建或释放资源。

包含：

- `pid`
- `name`
- `src`
- `cfg`
- `visibility`
- `isPrimarySecondaryMode`
- `state`
- `stopRequested`
- `initErr`
- `startErr`
- `stopGraceTimeout`
- `deps`
- `authorizer`

`runtimeDeps` 可迁入 `serviceCore` 或保持独立，但访问入口应集中，避免 `Service` 多处兜底查找。

### 4.3 `serviceState`

新增状态机封装，替代散落的 `atomic.CompareAndSwapInt32`。

当前状态：

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

建议新增：

```text
SvcStatusInitFailed
SvcStatusStartFailed
SvcStatusStopFailed
```

状态迁移规则：

```text
Unknown -> Init
Init -> Starting
Starting -> Running
Running -> Ready
Ready -> Closing
Running -> Closing
Starting -> StartFailed
Init -> InitFailed
Closing -> Closed
Closing -> StopFailed
InitFailed -> Closed
StartFailed -> Closed
StopFailed -> Closed
Closed -> Retire
```

建议方法：

```text
Load()
IsClosed()
TryInit()
TryStarting()
MarkRunning()
MarkReady()
TryClosing()
MarkInitFailed(err)
MarkStartFailed(err)
MarkStopFailed(err)
MarkClosed()
```

失败状态的作用：

- 区分“正常关闭”和“初始化/启动/停止失败”；
- 便于 `/health`、`RuntimeSnapshot`、审计日志和 metrics 暴露失败原因；
- 避免把所有失败都折叠成 `Closed`，提高排障价值。

### 4.4 `serviceRuntime`

`serviceRuntime` 统一拥有运行时资源。

包含：

- logger
- mailbox
- timer scheduler
- concurrent scheduler
- event processor
- event handler
- method manager
- rpc handler
- profiler
- job registry
- sysctl registry
- mailbox middlewares

建议方法：

```text
Build(conf, core, owner) error
StartMailbox()
StartCallbackLoop()
SuspendMailbox()
StopSchedulers()
StopMailbox()
Release(owner)
ReleasePartial()
CloseLogger()
```

关键规则：

- `Build` 只创建组件，不发布服务；
- `StartMailbox` 与 `StartCallbackLoop` 分离，便于失败回滚；
- `ReleasePartial` 用于 Init 失败；
- `Release` 用于 Stop 和 Start rollback；
- logger 关闭集中处理，避免重复 close；
- profiler 关闭集中处理；
- timer/concurrent 停止集中处理。

### 4.5 `serviceLifecycle`

`serviceLifecycle` 负责 `Init` / `Start` / `Stop` 编排。

#### Init 流程

```text
Init
├── validate input
├── state.TryInit()
├── normalize config
├── core.bindSource()
├── runtime.Build()
├── module.BindRoot()
├── endpoint.CreatePID()
├── user hooks.OnInit()
└── success
```

失败回滚：

```text
rollbackInit
├── runtime.ReleasePartial()
├── endpoint.ClearPID()
├── module.ResetRoot()
├── state.MarkInitFailed(err)
└── core.initErr = err
```

#### Start 流程

```text
Start
├── if initErr != nil return error
├── state.TryStarting()
├── runtime.StartMailbox()
├── runtime.StartCallbackLoop()
├── hooks.OnStart()
├── endpoint.MarkMasterIfNeeded()
├── state.MarkRunning()
├── endpoint.Register()
├── hooks.OnStarted()
├── state.MarkReady()
├── endpoint.Ready()
└── success
```

失败回滚使用进度标记：

```text
startProgress
├── mailboxStarted
├── callbackLoopStarted
├── onStartDone
├── endpointRegistered
└── onStartedDone
```

失败时：

```text
rollbackStart
├── endpoint.UnregisterIfRegistered()
├── runtime.StopMailbox()
├── runtime.StopSchedulers()
├── runtime.Release(owner)
├── state.MarkStartFailed(err)
└── return wrapped error
```

#### Stop 流程

因为当前没有外部使用者，建议将 `Stop()` 改为返回错误：

```text
Stop() error
```

流程：

```text
Stop
├── state.TryClosing()
├── runtime.SuspendMailbox()
├── runtime.StopSchedulers()
├── runtime.Release(owner)
├── endpoint.Unregister()
├── runtime.CloseLogger()
├── if error: state.MarkStopFailed(err)
├── else: state.MarkClosed()
└── return error
```

如实施时发现改动面过大，可以短期保留 `Stop()`，新增内部 `stopWithResult() error`，但最终接口应支持错误返回。

### 4.6 `serviceEndpointBinding`

集中处理 PID 和服务发现阶段。

职责：

- `CreatePID`
- `ClearPID`
- `MarkMasterIfNeeded`
- `Register`
- `Ready`
- `Unregister`

阶段语义：

```text
PIDCreated -> Registered -> Ready -> Removed
```

规则：

- `OnStarted` 之前可以 `Register`，因为现有语义允许 `OnStarted` 使用集群能力；
- `OnStarted` 失败必须 `Unregister`；
- `Ready` 必须发生在 `state.MarkReady()` 之后；
- 非集群或非主从服务默认 `pid.SetMaster(true)`；
- 主从服务在集群模式下不由普通 Start 直接设置 master。

### 4.7 `serviceDispatch`

集中处理投递与 callback job 构造。

职责：

- `PostJob`
- ReadOnly 自投递检测；
- RPC Job RWMode 推断；
- timer callback job 创建；
- concurrent callback job 创建。

必须保留的语义：

- `PostJob` 拥有 job 所有权；
- 早期拒绝路径负责 `OnJobDiscarded` 和 `Release`；
- ReadOnly handler 中同 Service 自投递写 job 应拒绝；
- ReadOnly handler 跨 Service 投递允许；
- RPC 请求根据 ReadOnly method 设置 `RWModeRead`；
- RPC reply / async callback 不设置 Read；
- nil envelope 不 panic；
- timer/concurrent 内部 callback 直接进入 mailbox，避免用户检查路径开销。

---

## 5. 接口重构设计

### 5.1 命名原则

原先的 `ServiceIdentity`、`ServiceRuntimeView`、`ServiceRPCView` 命名偏“分层视图”，不够贴近 Go 项目习惯。新的接口命名采用以下原则：

- 能力优先，而不是对象分类优先；
- 命名短，调用侧读起来像“需要什么能力”；
- 尽量使用项目已有 `I*` 风格，避免一半新风格一半旧风格；
- 避免 `View`、`Facade` 这类偏架构术语；
- 接口由消费者侧定义或至少按消费者侧能力拆分。

### 5.2 建议接口分组

建议将接口拆成以下能力组。

#### `IServiceRef`

服务引用能力，只包含身份和名称。

```text
IServiceRef
├── GetPid()
├── SetPid()
├── GetName()
├── SetName()
└── GetPartition()
```

适用场景：Endpoint、Router、日志字段、服务发现只需要识别服务，不需要生命周期能力。

#### `IServiceState`

服务状态读取能力。

```text
IServiceState
├── GetStatus()
└── IsClosed()
```

适用场景：健康检查、诊断、Endpoint ready 判断。

#### `IServiceControl`

框架内部生命周期控制能力。

```text
IServiceControl
├── Init(...)
├── Start() error
└── Stop() error
```

适用场景：ServiceManager / Node 启停服务。该接口不包含用户 hook。

#### `IServiceHooks`

用户服务 hook 能力。

```text
IServiceHooks
├── OnInit() error
├── OnStart() error
├── OnStarted() error
└── OnRelease()
```

适用场景：`serviceLifecycle` 调用用户扩展点。框架内部不应把 hooks 当完整服务使用。

#### `IServiceRuntime`

服务运行时访问能力。

```text
IServiceRuntime
├── GetServiceCfg()
├── GetMailbox()
├── GetNodeContext()
├── GetRouter()
└── GetLogger()
```

适用场景：模块、业务服务和少量运行时组件需要访问上下文。

#### `IServiceRPC`

RPC 暴露能力。

```text
IServiceRPC
├── GetRpcHandler()
├── IsRemoteCallable()
├── GetVisibility()
└── IsPrimarySecondaryMode()
```

适用场景：RPC 注册、Router、Endpoint 发布和权限检查。

#### `IJobReceiver`

服务接收 job 的能力。

```text
IJobReceiver
└── PostJob(job) error
```

命名使用 `Receiver` 而不是 `Poster`，因为从调用者角度看目标服务是 job 接收方；同时避免和 `PostJob` 方法重复形成奇怪读法。

#### `IServiceProfiler`

保留现有 profiler 能力接口。

```text
IServiceProfiler
├── OpenProfiler()
└── GetProfiler()
```

### 5.3 组合接口策略

迁移期保留 `IService`，但它只作为“完整服务约束”，内部调用应逐步替换为窄接口。

```text
IService =
  IServiceRef
  + IServiceState
  + IServiceControl
  + IServiceHooks
  + IServiceRuntime
  + IServiceRPC
  + IServiceProfiler
  + IJobReceiver
```

规则：

- 新代码不得随意依赖完整 `IService`；
- 只有 ServiceManager、Service 初始化入口、用户服务约束可以依赖 `IService`；
- Endpoint 只依赖 `IServiceRef + IServiceRPC + IServiceState`；
- Mailbox 只依赖 `IJobReceiver` 或必要的 discard callback；
- Lifecycle 只依赖 `IServiceHooks` 调用用户扩展点；
- Router/RPC 注册只依赖 `IServiceRPC`。

---

## 6. 用户服务模型

继续支持嵌入式模型：

```text
type MyService struct {
    core.Service
}
```

用户服务主要实现 `IServiceHooks`。

框架内部需要区分：

```text
user hooks != runtime service facade
```

也就是说：

- 用户服务提供 hook；
- `core.Service` 提供运行时门面；
- `serviceLifecycle` 调 hook；
- 其他组件不应把用户服务到处当完整 `IService` 使用。

---

## 7. 错误处理与诊断

### 7.1 新失败状态

新增：

```text
SvcStatusInitFailed
SvcStatusStartFailed
SvcStatusStopFailed
```

建议语义：

- `InitFailed`：初始化未完成，runtime 资源已尽力回滚；
- `StartFailed`：启动未完成，可能经过部分启动阶段，已执行 start rollback；
- `StopFailed`：停止过程中至少一个释放步骤失败，服务不可视为 Ready，但需要诊断保留；
- `Closed`：正常关闭或失败状态最终被确认清理后进入的终态。

### 7.2 错误保留

`serviceCore` 保存：

- `initErr`
- `startErr`
- `stopErr`

这些错误用于：

- `RuntimeSnapshot`
- health/ready 诊断；
- metrics；
- 审计日志；
- 后续错误码化。

### 7.3 回滚原则

- Init 失败：不允许服务进入 endpoint；
- Start 失败：如果已注册 endpoint，必须反注册；
- Stop 失败：必须尽力停止所有资源，再返回聚合错误；
- 回滚函数本身应防 panic，并把 panic 转换为错误或日志；
- logger close 必须幂等。

---

## 8. 测试策略

### 8.1 状态机测试

覆盖：

- Unknown → Init → Starting → Running → Ready → Closing → Closed；
- Init 失败进入 InitFailed；
- Start 失败进入 StartFailed；
- Stop 失败进入 StopFailed；
- 重复 Init / Start / Stop；
- Start before Init；
- Stop during Starting；
- Failed 状态最终可 MarkClosed。

### 8.2 生命周期回滚测试

覆盖：

- logger 初始化失败；
- timer 初始化失败；
- mailbox 初始化失败；
- pid 创建失败；
- rpc handler 初始化失败；
- `OnInit` 失败；
- mailbox start 后 `OnStart` 失败；
- endpoint registered 后 `OnStarted` 失败；
- stop 过程中 release 出错。

断言：

- mailbox stopped；
- timer stopped；
- concurrent closed；
- endpoint removed；
- logger closed；
- profiler closed；
- 状态正确；
- 错误被保存；
- 不重复 release。

### 8.3 Dispatch 测试

覆盖：

- ReadOnly 自投递拒绝；
- ReadOnly 跨服务投递允许；
- RW disabled 不检查；
- RPC read method 设置 `RWModeRead`；
- RPC write method 保持 `RWModeWrite`；
- reply envelope 不设置 Read；
- nil envelope 不 panic；
- mailbox rejected 后 job ownership 正确。

### 8.4 Endpoint 测试

覆盖：

- 非集群服务默认 master；
- 集群 + 主从服务不直接 master；
- Register / Ready / Unregister 顺序；
- `OnStarted` 失败后 RemoveService；
- Ready 前不可被普通路由选中。

### 8.5 集成与 race 测试

覆盖：

- example service 正常 Init/Start/Stop；
- local RPC call；
- remote callable service 注册；
- health/ready 可见性；
- `go test ./engine/pkg/core/...`；
- 关键包 race：actor/mailbox、core、rpc/message/msgbus、node。

---

## 9. 分阶段实施

### 阶段 1：测试锁定当前语义

不改结构，补生命周期、回滚和 dispatch 关键测试，为后续移动代码建立安全网。

### 阶段 2：抽 `serviceState`

新增 `service_state.go`，集中状态迁移和失败状态。

### 阶段 3：抽 `serviceRuntime`

新增 `service_runtime.go`，迁移 logger/mailbox/timer/concurrent/event/rpc/profiler 创建和释放。

### 阶段 4：抽 `serviceLifecycle`

新增 `service_lifecycle.go`，迁移 `Init` / `Start` / `Stop` / rollback 编排，`Service` 保留转发入口。

### 阶段 5：抽 `serviceEndpointBinding`

新增 `service_endpoint.go`，迁移 PID 创建、AddService、ServiceReady、RemoveService、master 设置。

### 阶段 6：抽 `serviceDispatch`

新增 `service_dispatch.go`，迁移 `PostJob`、`setJobRWMode`、timer/concurrent callback job 构造。

### 阶段 7：拆接口

调整 `engine/pkg/interfaces/IService.go`，按 `IServiceRef`、`IServiceState`、`IServiceControl`、`IServiceHooks`、`IServiceRuntime`、`IServiceRPC`、`IJobReceiver` 等能力拆分。迁移期保留组合 `IService`。

---

## 10. 推荐文件布局

```text
engine/pkg/core/
├── service.go              // Service facade、基础 getter/setter
├── service_state.go        // 状态机
├── service_runtime.go      // runtime 组件创建/释放
├── service_lifecycle.go    // Init/Start/Stop/rollback 编排
├── service_endpoint.go     // PID/Endpoint 注册/Ready/Remove
├── service_dispatch.go     // PostJob/RW/callback job
├── service_hooks.go        // hook 调用保护
└── service_*_test.go       // 分组件测试
```

接口侧可先继续集中在 `engine/pkg/interfaces/IService.go`，避免文件数量过快膨胀；后续稳定后再按能力拆文件。

---

## 11. 风险与缓解

| 风险 | 级别 | 缓解 |
|------|------|------|
| 生命周期语义被改坏 | 高 | 阶段 1 先补测试；每阶段只移动一个职责域 |
| 回滚路径遗漏资源 | 高 | runtime 统一拥有资源；rollback 使用 progress 标记 |
| 接口拆分影响范围大 | 中高 | 最后拆接口；迁移期保留组合 `IService` |
| 用户 hook 与 runtime facade 边界不清 | 中 | 明确 `IServiceHooks` 只用于用户扩展点 |
| `PostJob` ownership 被破坏 | 高 | dispatch 测试覆盖 Release/Discard 所有早期拒绝路径 |
| `Stop() error` 改动影响调用侧 | 中 | 当前无外部使用者可接受；必要时短期保留兼容 wrapper |
| 新失败状态影响旧判断 | 中 | 更新 `IsClosed`、health/ready、RuntimeSnapshot 语义并补测试 |

---

## 12. 成功标准

- `Service` 主文件明显变薄，主要作为 facade。
- `Init` / `Start` / `Stop` 主逻辑移动到 `serviceLifecycle`。
- runtime 资源创建和释放集中在 `serviceRuntime`。
- endpoint 注册、ready、remove 集中在 `serviceEndpointBinding`。
- `PostJob` 和 RW 语义集中在 `serviceDispatch`。
- 内部调用不再普遍依赖完整 `IService`。
- 新增 `InitFailed` / `StartFailed` / `StopFailed` 状态并有测试覆盖。
- 状态迁移、失败回滚、job ownership 均有独立测试。
- `go test ./engine/pkg/core/...` 通过。
- 关键包 race 测试通过。
- GitNexus 复评时，`Service` 即使仍是核心门面，也不再是复杂逻辑集中点。

---

## 13. 结论

推荐采用“分阶段破坏式解耦”：先测试锁定语义，再抽状态机、runtime、lifecycle、endpoint、dispatch，最后拆接口。

该方案比只补测试更能降低长期维护风险，也比彻底重写更安全。它保留已验证的 Actor/RPC/Module 基础模型，同时把 `Service` 的复杂度拆到可测试、可解释、可演进的小组件中。
