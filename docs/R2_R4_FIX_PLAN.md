# R2-M2 / R4-M2 修复方案

## 概述

本文档用于规划并跟踪两个遗留设计问题的修复：

- **R2-M2**：`MultiLevelQueueConf` 在构造 `PriorityQueueManager` 时被原地默认化，可能污染共享配置模板。
- **R4-M2**：调用 `WithJobType*Rule` 配置 Sentinel 规则时，如果没有配套启用 job-type `resourceFunc`，规则会被加载但运行时不会命中。

修复目标是保持向后兼容、最小改动、补充回归测试，并在完成后同步更新 [ACTOR_CODE_REVIEW_2026Q2.md](ACTOR_CODE_REVIEW_2026Q2.md)。

## 需求

- `PriorityQueueManager` 不再修改调用方传入的 `*config.MultiLevelQueueConf`。
- `PriorityScheduler` 不再持有调用方传入的 `PriorityBatches` map 指针。
- `WithJobTypeFlowRule` / `WithJobTypeCircuitBreakerRule` / `WithJobTypeSlowRatioRule` / `WithJobTypeErrorCountRule` 自动启用 job-type resource 映射。
- 保留 `NewSentinelMiddlewareWithJobType` 现有 API，不破坏旧代码。
- 添加单元测试覆盖两个修复点。
- 修复后更新 [ACTOR_CODE_REVIEW_2026Q2.md](ACTOR_CODE_REVIEW_2026Q2.md)。

## 影响范围

### R2-M2 影响文件

- [../engine/pkg/actor/mailbox/scheduler.go](../engine/pkg/actor/mailbox/scheduler.go)
  - 新增配置深拷贝 / 规范化 helper。
  - `NewPriorityScheduler` 内部复制 `PriorityBatches`。

- [../engine/pkg/actor/mailbox/queue_manager_priority.go](../engine/pkg/actor/mailbox/queue_manager_priority.go)
  - `NewPriorityQueueManager` 改为使用规范化后的配置副本。
  - 删除对调用方 `conf.PriorityBatches` 的原地默认化。

- [../engine/pkg/actor/mailbox/queue_manager_priority_test.go](../engine/pkg/actor/mailbox/queue_manager_priority_test.go)
  - 增加回归测试。

- [../engine/pkg/actor/mailbox/scheduler_test.go](../engine/pkg/actor/mailbox/scheduler_test.go)
  - 增加 `NewPriorityScheduler` 防御性拷贝测试。

### R4-M2 影响文件

- [../engine/pkg/actor/mailbox/sentinel_middleware.go](../engine/pkg/actor/mailbox/sentinel_middleware.go)
  - 增加 job-type resource 模式标记。
  - `WithJobType*Rule` 自动启用 job-type resource 模式。
  - `NewSentinelMiddleware` 在需要时自动安装默认 job-type `resourceFunc`。

- [../engine/pkg/actor/mailbox/sentinel_middleware_test.go](../engine/pkg/actor/mailbox/sentinel_middleware_test.go)
  - 增加 job-type resource 自动映射测试。

- [../engine/pkg/actor/mailbox/README.md](../engine/pkg/actor/mailbox/README.md)
  - 更新 Sentinel 细粒度 resource 文档。

- [ACTOR_CODE_REVIEW_2026Q2.md](ACTOR_CODE_REVIEW_2026Q2.md)
  - 新增 R11 修复记录。
  - 标记 R2-M2 / R4-M2 已修复。

## 实施步骤

### 阶段 1：修复 R2-M2 配置原地默认化

#### 1. 新增优先级配置拷贝 helper

**文件**：[../engine/pkg/actor/mailbox/scheduler.go](../engine/pkg/actor/mailbox/scheduler.go)

**操作**：

新增私有 helper：

- `clonePriorityBatches(src map[def.Priority]*config.PriorityConfig) map[def.Priority]*config.PriorityConfig`
- `normalizeMultiLevelQueueConf(conf *config.MultiLevelQueueConf) *config.MultiLevelQueueConf`
- `normalizeMultiLevelWorkerConf(conf *config.MultiLevelWorkerConf) *config.MultiLevelWorkerConf`

实现要求：

- `map[def.Priority]*config.PriorityConfig` 必须做双层拷贝：复制 map，也复制每个 `*PriorityConfig` 指向的值。
- **重要**：`PriorityConfig` 本身是值类型（只含 `BatchSize int` 和 `Weight int`），但 map 中存的是**指针**（`*PriorityConfig`），因此必须为每个 entry 创建新的 `*PriorityConfig`：
  ```go
  dst[priority] = &config.PriorityConfig{BatchSize: src[priority].BatchSize, Weight: src[priority].Weight}
  ```
- `conf == nil` 时返回 `DefaultMultiLevelQueueConf()` 或等价默认配置副本。
- `PriorityBatches` 为空时使用默认优先级配置，或至少补充 `def.PriorityNormal`。
- 不得写回调用方传入的 `conf`。

**原因**：

`map` 和 `*PriorityConfig` 都是引用语义。只复制 `MultiLevelQueueConf` 结构体不足以隔离外部配置，必须深拷贝 `PriorityBatches`。

**依赖项**：无。

**风险**：低。

#### 2. 修改 `NewPriorityQueueManager` 使用配置副本

**文件**：[../engine/pkg/actor/mailbox/queue_manager_priority.go](../engine/pkg/actor/mailbox/queue_manager_priority.go)

**操作**：

将当前逻辑：

```go
if conf == nil {
    conf = DefaultMultiLevelQueueConf()
}
```

替换为：

```go
conf = normalizeMultiLevelQueueConf(conf)
```

同时删除以下原地默认化逻辑：

```go
if len(conf.PriorityBatches) == 0 {
    conf.PriorityBatches = map[def.Priority]*config.PriorityConfig{
        def.PriorityNormal: {BatchSize: 8},
    }
}
```

**原因**：

构造函数不能修改调用方共享配置模板。默认化应发生在内部副本上。

**依赖项**：阶段 1 步骤 1。

**风险**：低。

#### 3. 修改 `NewPriorityScheduler` 防御性拷贝

**文件**：[../engine/pkg/actor/mailbox/scheduler.go](../engine/pkg/actor/mailbox/scheduler.go)

**操作**：

在 `NewPriorityScheduler` 开始处调用：

```go
conf = normalizeMultiLevelWorkerConf(conf)
```

确保：

- `PriorityScheduler.priorities` 保存的是归一化后的副本 map。
- `weights` / `counters` 也基于副本初始化。

**原因**：

`NewPriorityScheduler` 是导出函数，即使当前只由 `PriorityQueueManager` 调用，也应具备自身防御性。

**依赖项**：阶段 1 步骤 1。

**风险**：低。

#### 4. 添加 R2-M2 单元测试

**建议文件**：

- [新建] `../engine/pkg/actor/mailbox/queue_manager_priority_test.go`
- [新建] `../engine/pkg/actor/mailbox/scheduler_test.go`

> 注：这两个测试文件当前不存在，需要新建。现有 mailbox 测试文件包括 `mailbox_lifecycle_test.go`、`postjob_ownership_test.go` 等，但无针对 `PriorityQueueManager` / `PriorityScheduler` 的单独测试文件。

**测试用例**：

- `NewPriorityQueueManager` 不修改原始 `conf.PriorityBatches`。
- 传入空 `PriorityBatches` 时，原始 map 仍为空，但 manager 可正常工作。
- 构造后修改调用方 `PriorityBatches[priority].BatchSize`，不影响 manager 内部 `batchSizes`。
- 构造后修改调用方 `PriorityBatches[priority].Weight`，不影响 scheduler 内部 `weights`。
- `NewPriorityScheduler` 构造后修改外部 map，不影响 `scheduler.priorities`。

**原因**：

防止未来回归成浅拷贝或原地默认化。

**依赖项**：阶段 1 步骤 2、3。

**风险**：低。

## 阶段 2：修复 R4-M2 Sentinel JobType 规则不自动配套 resourceFunc

### 1. 增加 job-type resource 模式标记

**文件**：[../engine/pkg/actor/mailbox/sentinel_middleware.go](../engine/pkg/actor/mailbox/sentinel_middleware.go)

**操作**：

在 `SentinelMiddleware` 增加字段：

```go
jobTypeResourceMode bool
```

**语义**：

只要用户使用过 `WithJobType*Rule`，就需要运行时 resource 按 `serviceName:jobType` 生成。

**原因**：

当前 `WithJobType*Rule` 只把规则加载到 `serviceName:jobType`，但 `OnReceive` 默认仍用 `serviceName`，导致规则静默不命中。

**依赖项**：无。

**风险**：低。

### 2. `WithJobType*Rule` 自动启用 job-type resource 模式

**文件**：[../engine/pkg/actor/mailbox/sentinel_middleware.go](../engine/pkg/actor/mailbox/sentinel_middleware.go)

**操作**：

在以下 option 内增加：

```go
m.jobTypeResourceMode = true
```

涉及函数：

- `WithJobTypeFlowRule`
- `WithJobTypeCircuitBreakerRule`
- `WithJobTypeSlowRatioRule`
- `WithJobTypeErrorCountRule`

**原因**：

用户只要配置 job-type 规则，就应该自动进入 job-type 资源匹配模式，避免静默失效。

**依赖项**：阶段 2 步骤 1。

**风险**：低。

### 3. `NewSentinelMiddleware` 自动安装默认 job-type `resourceFunc`

**文件**：[../engine/pkg/actor/mailbox/sentinel_middleware.go](../engine/pkg/actor/mailbox/sentinel_middleware.go)

**操作**：

在所有 opts 应用完成后增加逻辑：

```go
if m.jobTypeResourceMode {
    m.ruleResources = append(m.ruleResources, sentinelJobTypeResources(serviceName)...)
    if m.resourceFunc == nil {
        m.resourceFunc = func(mctx inf.IMiddlewareContext) string {
            job := mctx.Job()
            if job != nil {
                return sentinelJobTypeResource(serviceName, job.GetType())
            }
            return serviceName
        }
    }
}
```

注意事项：

- 如果用户已经设置 `WithResourceFunc`，不能覆盖。
- 内置 job-type resources 应通过 `effectiveRuleResources()` 去重。
- 自定义 job type 通过 `WithJobType*Rule` 自身已经加入 `ruleResources`，无需额外声明。

**原因**：

让 `WithJobType*Rule` 独立可用，不再强依赖 `NewSentinelMiddlewareWithJobType`。

**依赖项**：阶段 2 步骤 1、2。

**风险**：中。

**风险说明**：

旧行为中，`NewSentinelMiddleware(..., WithJobTypeFlowRule(...))` 会加载 `serviceName:jobType` 规则，但运行时仍走 `serviceName`。修复后会按 API 名称预期走 `serviceName:jobType`。这可能影响依赖旧错误行为的代码，但只在显式使用 `WithJobType*Rule` 时触发。

**附带影响（`OnStart` 警告日志）**：

`OnStart` 中有一段警告逻辑：

```go
if m.resourceFunc != nil && len(m.ruleResources) == 0 && ... {
    m.logger.Warnf("...no rule resources were declared...")
}
```

修复后，`jobTypeResourceMode` 会自动向 `m.ruleResources` 追加 job-type resources，因此该警告在 job-type 场景下不会触发——这是**正确行为**（规则 resource 已声明）。该警告仍保留对"用户自定义 `WithResourceFunc` 但忘记声明 rule resources"场景的保护。

### 4. 保留 `NewSentinelMiddlewareWithJobType` 兼容性

**文件**：[../engine/pkg/actor/mailbox/sentinel_middleware.go](../engine/pkg/actor/mailbox/sentinel_middleware.go)

**操作**：

保留函数签名不变。

可选实现方式：

- 保留当前显式追加 `WithSentinelRuleResources(...)` 和 `WithResourceFunc(...)` 的行为。
- 或改为内部启用 `jobTypeResourceMode`，但不建议在本轮改动中重构，避免扩大影响面。
**注意事项（option 执行顺序）**：

当前 `NewSentinelMiddlewareWithJobType` 的 opts 执行顺序为：

```go
allOpts := append([]SentinelOption{}, opts...)          // 用户 opts 先执行
allOpts = append(allOpts, WithSentinelRuleResources...) // 再追加 resources
allOpts = append(allOpts, WithResourceFunc(...))        // 最后设置 resourceFunc
```

这意味着：

- 用户在 `opts` 中传入 `WithResourceFunc` 会被后追加的 `WithResourceFunc` 覆盖——这是**已有行为**，与本次修复无关。
- 用户传入 `WithJobType*Rule` 会设置 `jobTypeResourceMode = true`，但后续 `WithResourceFunc` 会使 `resourceFunc != nil`，因此阶段 2 步骤 3 的自动安装逻辑会跳过——**兼容性正确**。
**原因**：

兼容现有用户，继续作为“显式开启 job-type 模式”的便捷构造器。

**依赖项**：阶段 2 步骤 3。

**风险**：低。

### 5. 添加 R4-M2 单元测试

**文件**：[../engine/pkg/actor/mailbox/sentinel_middleware_test.go](../engine/pkg/actor/mailbox/sentinel_middleware_test.go)

**测试用例**：

- `NewSentinelMiddleware("svc", WithJobTypeFlowRule(customType, 1))` 后：
  - `m.resourceFunc != nil`
  - `m.effectiveRuleResources()` 包含 `svc:<customType>`
  - 对带 custom job type 的 `mctx` 调用 `resourceFunc` 返回 `svc:<customType>`
- `WithJobTypeCircuitBreakerRule` / `WithJobTypeSlowRatioRule` / `WithJobTypeErrorCountRule` 同样触发 job-type resource 模式。
- 用户显式 `WithResourceFunc` 时不会被覆盖。
- 普通 `WithFlowRule` 不会自动启用 job-type resource mode。

**原因**：

覆盖核心行为和兼容性边界。

**依赖项**：阶段 2 步骤 2、3。

**风险**：低。

## 阶段 3：文档与验证

### 1. 更新邮箱 README 的 Sentinel 说明

**文件**：[../engine/pkg/actor/mailbox/README.md](../engine/pkg/actor/mailbox/README.md)

**操作**：

更新 Sentinel 细粒度 resource 小节，明确：

- 使用 `WithJobType*Rule` 会自动启用 job-type resource 映射。
- 自定义 `MailboxJobType` 可直接使用 `WithJobType*Rule(customType, ...)`。
- 若使用完全自定义 resource 名称，仍使用 `WithResourceFunc` + `WithResourceFlowRule` / `WithSentinelRuleResources`。

**原因**：

避免用户误用 API。

**依赖项**：阶段 2 完成。

**风险**：低。

### 2. 更新审查文档

**文件**：[ACTOR_CODE_REVIEW_2026Q2.md](ACTOR_CODE_REVIEW_2026Q2.md)

**操作**：

- 新增 R11 修订记录。
- 将 R2-M2 标记为已修复。
- 将 R4-M2 标记为已修复。
- 更新 §9.4 / §10.4 “仍未修复的历史问题”。
- 更新最终结论，说明 R2-M2 / R4-M2 已完成。

**原因**：

保持审查文档与代码状态一致。

**依赖项**：阶段 1、2 完成。

**风险**：低。

### 3. 执行验证命令

**命令**：

```powershell
go build ./...
go vet ./...
go test ./... -count=1 -timeout 180s
```

**原因**：

确认无编译、vet、测试回归。

**依赖项**：所有代码修改完成。

**风险**：低。

## 测试策略

### R2-M2 单元测试

- `NewPriorityQueueManager` 不修改传入配置。
- 空 `PriorityBatches` 不污染原始配置。
- 构造后修改原始 map / `PriorityConfig` 指针不影响 manager。
- `NewPriorityScheduler` 不持有外部 map。

### R4-M2 单元测试

- `WithJobType*Rule` 自动启用 job-type resourceFunc。
- 自定义 job type 可命中 `serviceName:jobType` resource。
- 用户自定义 `WithResourceFunc` 不被覆盖。
- 普通 `WithFlowRule` 不改变默认 `serviceName` resource。

### 回归测试

- `go build ./...`
- `go vet ./...`
- `go test ./... -count=1 -timeout 180s`

## 现有测试影响评估

### R2-M2

需检查 `queue_manager_priority_test.go` 和 `scheduler_test.go` 中是否有测试依赖「配置被原地修改」的行为。如果有，需要修正测试本身。

**实际检查结果（已确认）**：

`queue_manager_priority_test.go` 和 `scheduler_test.go` **当前不存在**，需新建。

### R4-M2

需检查 `sentinel_middleware_test.go` 中是否有以下模式的测试：

- 使用 `NewSentinelMiddleware(..., WithJobTypeFlowRule(...))` 但**断言** `m.resourceFunc == nil`。
- 使用 `NewSentinelMiddleware` + `WithJobType*Rule` 后检查 `OnReceive` 的 resource 为 `serviceName`（旧错误行为）。

如果存在这类测试，需要更新断言以匹配修复后的正确行为。

**实际检查结果（已确认）**：

现有 `sentinel_middleware_test.go` 中 **所有** 使用 `WithJobType*Rule` 的测试都是通过 `NewSentinelMiddlewareWithJobType` 构造的，不存在单独使用 `NewSentinelMiddleware` + `WithJobType*Rule` 的场景。因此：

- 现有测试不会因本次修复而 fail。
- 但需要**新增测试**以覆盖 `NewSentinelMiddleware` + `WithJobType*Rule` 的自动 job-type resource 行为。

---

## 风险与缓解措施

### 风险 1：R2-M2 修复后默认配置行为变化

**说明**：

如果默认配置选择不一致，可能影响多优先级队列调度行为。

**缓解措施**：

保持默认值与 `DefaultMultiLevelQueueConf()` 一致，只改变“是否污染调用方配置”。

### 风险 2：R4-M2 自动启用 job-type resourceFunc 改变旧行为

**说明**：

旧行为下 `WithJobType*Rule` 规则本身几乎无法命中；修复后会真正生效。

**缓解措施**：

只在显式调用 `WithJobType*Rule` 时启用 job-type resourceFunc；普通 `WithFlowRule` 保持原行为。

### 风险 3：浅拷贝遗漏导致外部配置仍可影响内部

**说明**：

只复制 map 而不复制 `*PriorityConfig` 仍会保留共享指针。

**缓解措施**：

对 `map[def.Priority]*config.PriorityConfig` 做 map + struct 双层拷贝，并添加回归测试。

### 风险 4：用户自定义 `WithResourceFunc` 被覆盖

**说明**：

如果自动安装默认 job-type `resourceFunc` 覆盖用户自定义函数，会破坏自定义资源策略。

**缓解措施**：

`NewSentinelMiddleware` 只在 `m.resourceFunc == nil` 时安装默认 job-type `resourceFunc`。

## 成功标准

- [x] `NewPriorityQueueManager` 不再修改传入的 `*config.MultiLevelQueueConf`。
- [x] `NewPriorityScheduler` 不再持有调用方传入的 `PriorityBatches` map。
- [x] `WithJobType*Rule` 在没有 `NewSentinelMiddlewareWithJobType` 的情况下也能自动使用 `serviceName:jobType` resource。
- [x] 自定义 `MailboxJobType` 的 Sentinel 规则可正常命中。
- [x] 用户自定义 `WithResourceFunc` 不被覆盖。
- [x] 新增单元测试覆盖 R2-M2 / R4-M2。
- [x] `go build ./...`、`go vet ./...`、`go test ./... -count=1 -timeout 180s` 全部通过。
- [x] [ACTOR_CODE_REVIEW_2026Q2.md](ACTOR_CODE_REVIEW_2026Q2.md) 标记 R2-M2 / R4-M2 已修复。

---

## R11 修复后代码审查记录

**审查时间**：2026-05-16 R11

**审查范围**：本次修复涉及的所有 Go 文件改动

**结论**：✅ **批准** — 未发现关键或高优先级问题，发现 3 个低优先级改进点（NIT），均已修复。

### NIT-1 — `NewPriorityQueueManager` 对 `PriorityBatches` 双重深拷贝 ✅ 已修复

**位置**：[../engine/pkg/actor/mailbox/queue_manager_priority.go](../engine/pkg/actor/mailbox/queue_manager_priority.go)

**原因**：

`NewPriorityQueueManager` 先调用 `normalizeMultiLevelQueueConf(conf)` 完成深拷贝，随后将拷贝后的 `conf.PriorityBatches` 传给 `NewPriorityScheduler`：

```go
m.scheduler = NewPriorityScheduler(&config.MultiLevelWorkerConf{
    Strategy:        conf.Strategy,
    PriorityBatches: conf.PriorityBatches, // 已是副本
})
```

`NewPriorityScheduler` 内部再次调用 `normalizeMultiLevelWorkerConf`，导致 `PriorityBatches` 被深拷贝两次。每个 Worker 构造时均如此，有轻微无效分配。

**建议**：可留待下次重构，现阶段功能完全正确。

**修复方法**：抽取内部 `newPrioritySchedulerFromNormalized`，`NewPriorityQueueManager` 直接调用它跳过二次 normalize；`NewPriorityScheduler`（导出）仍走 normalize 保持防御性。

**优先级**：低。

### NIT-2 — `NewSentinelMiddlewareWithJobType` + `WithJobType*Rule` 导致 `ruleResources` 重复追加 ✅ 已修复

**位置**：[../engine/pkg/actor/mailbox/sentinel_middleware.go](../engine/pkg/actor/mailbox/sentinel_middleware.go)

**原因**：

当用户通过 `NewSentinelMiddlewareWithJobType("svc", WithJobTypeFlowRule(...))` 调用时：

1. `WithJobTypeFlowRule` 将 `svc:jobType` 加入 `ruleResources`，并设置 `jobTypeResourceMode = true`。
2. `NewSentinelMiddlewareWithJobType` 末尾追加 `WithSentinelRuleResources(sentinelJobTypeResources(svc)...)`，将全部内置 job-type resources 再次加入 `ruleResources`。
3. opts 全部执行完毕后，auto-install 逻辑检测到 `jobTypeResourceMode == true`，**再次** `append(m.ruleResources, sentinelJobTypeResources(svc)...)`。

`effectiveRuleResources()` 通过 `seen` map 去重，功能正确，但底层 slice 存在冗余元素。

**建议**：`effectiveRuleResources()` 已是 source of truth，现状可接受；若后续优化可在 auto-install 追加前检测已有内容。

**修复方法**：将 auto-install 逻辑改为 `if m.jobTypeResourceMode && m.resourceFunc == nil`，仅在真正需要自动安装时才追加 ruleResources；当 `NewSentinelMiddlewareWithJobType` 已设置 resourceFunc 时跳过。

**优先级**：低（功能正确，`effectiveRuleResources()` 已去重）。

### NIT-3 — `TestCustomResourceFunc_NotOverridden` 传 `nil` 给 `mctx` ✅ 已修复

**位置**：[../engine/pkg/actor/mailbox/sentinel_middleware_test.go](../engine/pkg/actor/mailbox/sentinel_middleware_test.go)

**原因**：

```go
if m.resourceFunc(nil) != "custom" {
```

自定义函数 `func(mctx inf.IMiddlewareContext) string { return "custom" }` 不使用 `mctx`，传 nil 当前安全。但若未来测试被复制修改为会使用 `mctx` 的 resourceFunc，会引入 nil panic。

**建议**：改用类型化零值（如 `(*mockMiddlewareContext)(nil)`）或空 mock 实现，使意图更明确。

**修复方法**：改为 `var nilCtx inf.IMiddlewareContext`，使用接口类型的零值而非 untyped nil。

**优先级**：低（影响范围仅限测试代码）。

---

## R11 NIT 修复后再次审查记录

**审查时间**：2026-05-16 R11 复审

**审查范围**：包括但不限于刚才针对 R11 NIT 的修复，覆盖当前 Go 文件改动。

**验证结果**：

- `go vet ./...`：通过。
- `staticcheck ./engine/pkg/actor/mailbox/...`：通过。
- `go test ./... -count=1 -timeout 180s`：通过（未发现 `FAIL` / `panic` / `fatal`）。
- `staticcheck ./...`：仍存在大量历史问题；本次 mailbox 相关修复未新增 staticcheck 问题。

**结论**：⛔ **阻止** — 发现 1 个高优先级问题和 2 个低优先级问题。

### H1 — `ApplySnapshot` 未拷贝权限 slice，外部可在应用后篡改授权策略

**位置**：[../engine/pkg/authz/authz.go](../engine/pkg/authz/authz.go)

**当前代码**：

```go
newRoles[name] = &Role{Name: name, Permissions: pr.Permissions}
```

**原因**：

`pr.Permissions` 是外部 `PolicySnapshot` 持有的 slice。`ApplySnapshot()` 应用后，如果调用方继续修改 `snapshot.Roles[name].Permissions`，会直接影响 `Authorizer` 内部策略。

**风险**：

- 可绕过 `Validate()`。
- 可在未持有 `Authorizer` 锁的情况下改变权限。
- 可造成授权策略被静默放宽，例如把某个权限改成 `*`。
- 与前面 `AddRole()` 已修复的 slice copy 问题属于同类安全问题。

**建议修复**：

```go
perms := make([]string, len(pr.Permissions))
copy(perms, pr.Permissions)
newRoles[name] = &Role{Name: name, Permissions: perms}
```

**优先级**：高。

**状态**：✅ 已修复（R11-H1）— `ApplySnapshot` 中增加 `copy(perms, pr.Permissions)` 防御性拷贝。

### L1 — `TestCustomResourceFunc_NotOverridden` 的 typed nil 仍然是 nil，NIT 修复不彻底

**位置**：[../engine/pkg/actor/mailbox/sentinel_middleware_test.go](../engine/pkg/actor/mailbox/sentinel_middleware_test.go)

**当前代码**：

```go
var nilCtx inf.IMiddlewareContext
if m.resourceFunc(nilCtx) != "custom" {
```

**原因**：

`nilCtx` 仍是 nil interface。相比直接传 `nil`，并未真正避免未来 `resourceFunc` 使用 `mctx` 时 panic。

**建议修复**：

使用真实中间件上下文，例如：

```go
ctx := NewMiddlewareContext(context.Background(), nil, "svc-custom")
if m.resourceFunc(ctx) != "custom" {
```

**优先级**：低。

**状态**：✅ 已修复 — 使用 `NewMiddlewareContext(context.Background(), nil, "svc-custom")` 替代 typed nil。

### L2 — `AllowedOrigins` 注释与实现不一致，可能误导安全配置

**位置**：[../engine/pkg/utils/network/ws_server.go](../engine/pkg/utils/network/ws_server.go)

**当前注释**：

```go
AllowedOrigins []string // 允许的 Origin 列表；为空或包含 "*" 时允许所有来源
```

**实际实现**：

- 空列表：返回 `nil`，使用 gorilla/websocket 默认同源检查。
- 包含 `*`：允许所有来源。

注释中的“为空允许所有来源”与实现相反，可能误导安全配置。

**建议修复**：

```go
AllowedOrigins []string // 允许的 Origin 列表；为空时使用默认同源策略；包含 "*" 时允许所有来源
```

**优先级**：低。

**状态**：✅ 已修复 — 注释已更正。
