## Context

`core.Service` 是 EmberEngine Actor 框架的核心基类，所有业务服务（dbservice、healthservice、pprofservice 等）和用户自定义服务通过嵌入 `core.Service` 获取 Mailbox 调度、RPC、事件、定时器等能力。

当前 `Service` 结构体拥有 25+ 个直属字段（加 Module 嵌入共 40+ 字段），`Init()` 方法 180+ 行集中完成所有组件的创建与装配。`ServiceManager` 通过 Aware 接口在 Init 前注入运行时依赖（cluster、endpoints、router、nodeCtx、authorizer）。

主要消费者：
- `services.ServiceManager`：统一调用 Init/Start/Stop
- 用户服务：嵌入 `core.Service`，覆写 `OnInit`/`OnStart`/`OnRelease`
- sysModule（gate、router 等）：嵌入 `core.Module`，间接依赖 Service

约束：
- `IService` 接口不可变（公开契约）
- `core.Service` 嵌入 API 不可变（所有下游服务依赖）
- 必须保持回滚语义：Init 任一步失败需清理已分配资源

## Goals / Non-Goals

**Goals:**

1. 将 `Init()` 拆为 6 个可独立测试的子方法，主 Init 仅做编排
2. 将 5 个运行时依赖字段收拢为 `runtimeDeps` 内部 sub-struct
3. 删除废弃的 `msgHooks` 字段和 `AddMsgHook` 方法
4. 提取 Profiler Open/Close 逻辑到 `profilerBridge` helper
5. 保持所有外部 API 和嵌入行为完全兼容

**Non-Goals:**

- 不拆分 Mailbox/PostJob/RW 模式逻辑（三者紧耦合，拆分产生更多复杂性）
- 不将 Module 嵌入改为组合（会破坏所有用户服务调用链）
- 不将 Service 拆为多个独立包（框架内核适度耦合正常，拆包会循环依赖）
- 不将所有字段强制分组为 sub-struct（`s.identity.pid` 替代 `s.pid` 增加间接层无实质收益）
- 不改变 `IService` 接口签名

## Decisions

### Decision 1：Init 分解策略 — 私有方法拆分 vs Builder Pattern

**选择**：私有方法拆分

**理由**：
- 私有方法拆分零 API 变更，Init 签名不变
- Builder Pattern 会改变调用方式（`New().WithLogger().WithMailbox().Build()`），破坏 ServiceManager 的统一 Init 调用
- 私有方法可被单独测试（通过构造最小 Service 实例 + mock nodeCtx）
- 拆分后每个 init 子方法 20-40 行，可读性优秀

**替代方案**：
- Functional Options：适合可选配置，但 Init 中的步骤是必须的且有顺序依赖
- InitPhase 接口链：过度抽象，增加运行时开销

### Decision 2：运行时依赖组织 — sub-struct vs 保持扁平

**选择**：`runtimeDeps` 内部 sub-struct

**理由**：
- 5 个字段（nodeCtx、cluster、endpointManager、profilerRegistry、router）语义上是一组"Node 层注入的运行时依赖"
- 收拢后 `SetRuntimeDeps` 的语义更清晰
- 未来新增运行时依赖只需扩展一个结构体
- 内部 sub-struct 对外部完全不可见

**替代方案**：
- 保持扁平：可行但 5 个字段散落在 25+ 字段中语义模糊
- 接口注入：增加间接层但无实质收益（这些依赖生命周期与 Service 一致）

### Decision 3：Profiler 提取 — profilerBridge helper vs 保持内联

**选择**：`profilerBridge` 内部 helper struct

**理由**：
- 当前 `OpenProfiler` 和 `closeProfiler` 各自重复构造 `profilerRegistryAdapter`
- 提取后消除重复，且 profiler 逻辑可独立测试
- 优先级低，可作为可选步骤

### Decision 4：废弃代码处理 — 删除 vs 保留标记

**选择**：直接删除

**理由**：
- `msgHooks` 已有 TODO 标注"暂时废弃"，功能已被 mailbox 中间件完全替代
- grep 确认无外部使用者
- 保留废弃代码增加认知负担

## Risks / Trade-offs

- **[低]** Init 子方法之间存在隐式顺序依赖（如 initLogger 必须在其他 init 之前） → **缓解**：在 Init 编排方法中以注释明确标注顺序约束，子方法入口检查前置条件
- **[低]** `runtimeDeps` sub-struct 使得访问路径多一层（`s.deps.nodeCtx`） → **缓解**：保留 `GetNodeContext()` 等公开 getter，外部无感知；内部改动量约 15 处
- **[极低]** 删除 `AddMsgHook` 是 Breaking Change → **缓解**：grep 确认零使用者，框架已有 `AddMailboxMiddlewares` 作为替代
- **[低]** Profiler bridge 增加一层间接 → **缓解**：纯内部 helper，对性能无影响（非热路径）

## Open Questions

- 无。方案已在架构审查中与用户确认过核心决策。
