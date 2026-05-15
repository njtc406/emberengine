# P1 基础能力开发文档

> 创建时间：2026年5月12日  
> 来源：`docs/ROADMAP.md` 的 P1 短期任务与 `docs/NEXT_GOALS.md` 的 Phase A 目标  
> 前置条件：P0 稳定性闭环已完成，`go build ./...`、`go vet ./...`、`go test ./...`、关键 race 测试全部通过

---

## 一、P1 总目标

P1 不再继续扩大 P0 的稳定性修复范围，而是在已稳定的 Actor/RPC/Config 基础上补齐“可维护、可观测、可回归”的短期能力。核心目标是让框架从“核心链路稳定”推进到“问题可定位、错误可分类、运行状态可观测、关闭流程可证明”。

验收标准：

1. `errorx` 成为新的结构化错误主路径，错误码、错误链、结构化字段和 `errors.Is/As` 行为可测试。
2. 连接池已有 `PoolMetrics` 可以以稳定接口导出，并具备 Prometheus 暴露的最小可用路径。
3. mailbox/rpc/event/core/node 的核心回归测试继续扩展，新增能力必须伴随测试。
4. Node/Service 全局关闭顺序明确，重复关闭、部分启动失败、正在处理任务等场景行为确定。
5. P1 结束时仍保持 P0 门禁全绿。

---

## 二、P1 任务状态

| 编号 | 问题 | 当前状态 | 本文档处理方式 |
|------|------|----------|----------------|
| P1-1 | errorx 库完善 | ✅ 已完成 | 收敛 `errorx`/`errorlib` 边界，建立错误码契约和 RPC wire error 计划 |
| P1-2 | Pool Metrics 暴露 | ✅ 已完成 | 先导出连接池 metrics 最小闭环，再为 P2 Prometheus 全量指标铺路 |
| P1-3 | 核心模块单元测试 | ✅ 已完成 | 在 P0 测试基础上补 core/rpc/event/node 的高价值路径 |
| P1-4 | 优雅关闭完善 | ✅ 已完成 | 固定 Node/Service/Pool/Event/RPC 停止顺序并用测试保护 |

---

## 三、开发原则

1. **先做薄切片**：P1 每个能力都先做最小可用闭环，再进入 P2/P3 扩展。
2. **不破坏 P0 契约**：资源释放、Mailbox 生命周期、RPC CallState、配置负向测试必须持续通过。
3. **错误体系向后兼容**：保留 `errorlib` 兼容层或迁移策略，不一次性改动所有调用点。
4. **Metrics 先读后写**：先复用已有 `RuntimeSnapshot`、`PoolMetrics`、`EventMetrics`，避免过早引入复杂采集框架。
5. **关闭流程只收敛顺序，不重写架构**：优先补状态机和测试，避免在 P1 做大规模 Service/Node 重构。

---

## 四、P1-1：errorx 结构化错误体系

### 问题描述

当前项目同时存在 `engine/pkg/utils/errorx` 和 `engine/pkg/utils/errorlib`。`errorx` 已具备错误码、错误链、调用位置、结构化字段和 `errors.Is` 支持；`errorlib` 是旧路径，仍可能被部分代码使用。P1-1 的目标不是全仓机械替换，而是先明确新旧边界、补足测试和迁移规范。

### 重点文件

| 文件 | 关注点 |
|------|--------|
| `engine/pkg/utils/errorx/errorx.go` | 新错误模型、Wrap/Unwrap、字段收集、错误码匹配 |
| `engine/pkg/utils/errorx/errorx_test.go` | 现有行为测试，需要补充边界和并发安全用例 |
| `engine/pkg/utils/errorlib/errors.go` | 旧错误码类型，确认兼容策略 |
| `engine/pkg/def/error.go` | 框架级错误码定义入口 |
| `engine/pkg/rpc/message/` | RPC 调用返回错误的传递边界 |
| `engine/pkg/rpc/remote/` | 远程错误序列化/反序列化的后续落点 |

### 开发任务

| 子任务 | 内容 | 产物 |
|--------|------|------|
| P1-1.1 | 盘点 `errorx`/`errorlib` 当前调用点 | 调用点表格或文档注释 |
| P1-1.2 | 补齐 `errorx` 行为测试：nil wrap、sentinel 安全、字段链合并、RootCause、CodeFrom | `errorx` 单元测试 |
| P1-1.3 | 定义框架错误码分段：config/rpc/mailbox/cluster/event/node | `def/error.go` 或错误码文档 |
| P1-1.4 | 设计 `errorlib` 兼容策略：保留、桥接或逐步废弃 | 迁移说明 |
| P1-1.5 | 规划 RPC wire error：错误码、消息、字段、cause 的跨节点传输格式 | RPC 错误设计草案 |

### 验收标准

- `go test ./engine/pkg/utils/errorx/... ./engine/pkg/utils/errorlib/... -count=1` 通过。
- `errors.Is(err, sentinel)`、`errors.As(err, *errorx.Error)`、`errorx.HasCode` 有明确测试。
- 新增错误码命名和编号规则写入文档或 `def/error.go` 注释。
- 不要求 P1-1 一次性替换全仓旧错误，但必须给出迁移边界。

---

## 五、P1-2：Pool Metrics 最小可观测闭环

### 问题描述

`Node.GetRuntimeSnapshot()` 已能读取 `PoolManager.GetAllPoolMetrics()`，连接池内部也已有 `PoolMetrics`。P1-2 要把这条内部诊断链路变成稳定的可导出能力，先覆盖连接池三类核心指标：当前连接数、命中/请求类计数、扩缩容/健康状态。

### 重点文件

| 文件 | 关注点 |
|------|--------|
| `engine/pkg/rpc/client/pool/manager_types.go` | `PoolMetrics` 字段定义 |
| `engine/pkg/rpc/client/pool/manager_runtime.go` | 连接创建、获取、扩缩容、健康检查的指标更新点 |
| `engine/pkg/rpc/client/pool/manager.go` | PoolManager 聚合与 `GetAllPoolMetrics` |
| `engine/pkg/node/diagnostics.go` | RuntimeSnapshot 聚合入口 |
| `engine/pkg/interfaces/IMetricCollector.go` | 现有 metrics 抽象，决定保留/收敛方式 |
| `engine/pkg/metrics/` | 建议新增：Prometheus adapter 与 registry 封装 |

### 架构变更

| 变更 | 文件/目录 | 描述 |
|------|-----------|------|
| 新增 metrics adapter | `engine/pkg/metrics/` | 封装最小 Registry、Collector、Prometheus handler，不侵入业务代码 |
| PoolMetrics 快照标准化 | `engine/pkg/rpc/client/pool/manager_types.go` | 明确字段语义、单位和并发读取方式 |
| Node 暴露 metrics snapshot | `engine/pkg/node/diagnostics.go` | 保持现有 RuntimeSnapshot，新增转换到 metrics 的适配层 |
| 可选 HTTP 暴露 | `engine/pkg/sysService/` 或现有 pprof service | P1 只暴露 pool metrics，完整 /health /ready 留给 P2 |

### 开发任务

| 子任务 | 内容 | 产物 |
|--------|------|------|
| P1-2.1 | 固化 `PoolMetrics` 字段：total/idle/active/requests/errors/scale/time | 字段注释和测试 |
| P1-2.2 | 为 PoolManager 聚合写测试：多 pool、空 pool、并发读取 | `rpc/client/pool` 测试 |
| P1-2.3 | 新增 `metrics` 包最小 adapter：Gauge/Counter 描述和 snapshot 转换 | `engine/pkg/metrics` |
| P1-2.4 | 将 pool snapshot 转为 Prometheus 文本或 client_golang collector | adapter 测试 |
| P1-2.5 | 在 Node 诊断层接入 PoolMetrics 导出 | `node/diagnostics.go` 测试 |
| P1-2.6 | 写明 P2 全量 metrics 的扩展点 | 文档回填 |

### 验收标准

- `go test ./engine/pkg/rpc/client/pool/... ./engine/pkg/node/... -count=1` 通过。
- `go test -race ./engine/pkg/rpc/client/pool/... -count=1` 通过。
- Pool metrics 在空连接池、无健康连接、扩容、停止后读取时均不 panic。
- 指标名称、单位和 label 集合稳定，避免后续 Prometheus 接入时破坏兼容。

---

## 六、P1-3：核心模块单元测试扩展（✅ 已完成）

### 问题描述

P0 已覆盖资源释放、Mailbox 生命周期、RPC CallState 和配置负向路径。P1-3 继续扩大高价值测试范围，重点不是追求覆盖率数字，而是保护后续可观测性、错误体系、优雅关闭变更会触碰的核心行为。

### 完成情况

| 子任务 | 内容 | 状态 | 产物 |
|--------|------|------|------|
| P1-3.1 | 覆盖率基线盘点（已有测试清单） | ✅ | 盘点 26(core) + 10(msgbus) + 11(event) + 4(services) + 4(node) 已有测试 |
| P1-3.2 | core service 生命周期测试 | ✅ | `core/service_lifecycle_test.go` — 8 tests: Stop 幂等/并发/状态机、Start 保护 |
| P1-3.3 | rpc/message 错误传播测试 | ✅ | `rpc/message/msgbus/bus_test.go` — 新增 6 tests: MultiBus AsyncCall/Send 空/错误聚合 |
| P1-3.4 | event 发布订阅测试 | ✅ | `event/event_lifecycle_test.go` — 6 tests: handler error 不阻塞/Destroy 安全/并发/重复名/nil trigger |
| P1-3.5 | services 停止顺序测试 | ✅ | `services/services_test.go` — 新增 3 tests: StopAll 幂等/空列表/Start 空列表 |
| P1-3.6 | race 门禁验证 | ✅ | `go test -race -count=2` 全部通过（core/event/services/msgbus） |

### 新增测试总计：23 tests

### 验收标准 — 全部达成

- [x] `go test ./engine/pkg/core/... ./engine/pkg/event/... ./engine/pkg/rpc/... ./engine/pkg/services/... -count=1` 通过
- [x] 涉及并发的新增测试在 `-race` 下稳定通过
- [x] 新测试不依赖真实 etcd、真实远端节点或长时间 sleep
- [x] `go build ./...` / `go vet ./...` / `go test ./...` 全量通过

---

## 七、P1-4：优雅关闭完善（✅ 已完成）

### 问题描述

P0 已证明 Mailbox 局部 Stop/Drain 行为。P1-4 要把局部能力串成全局关闭顺序：Node 停止时，Service、Mailbox、RPC sender/pool、EventBus、Cluster discovery 等组件的停止顺序要固定，并且重复调用、部分组件启动失败、仍有 in-flight 请求等场景不能导致泄漏或死锁。

### 实际停止顺序（已验证）

```text
1. ServiceManager.StopAll()      — 业务服务（最后启动，最先停止）
2. EventBus.Stop()               — 事件总线及 NATS 连接
3. Cluster.Close()               — EndpointManager + RPC Handler
4. SenderManager.Close()         — RPC 发送管理器
5. PoolManager.Close()           — 连接池（sync.Once + wg.Wait 保证 goroutine 退出）
6. RpcMonitor.Stop()             — RPC 监控统计
7. DeDuplicator.Close()          — 请求去重器
8. TimingWheel.Stop()            — 时间轮
9. AntsPool.Release()            — 协程池
10. Logger.Close()               — 日志（最后关闭，确保前面的日志输出）
```

所有组件均具备幂等保护（atomic.Bool CAS / sync.Once），Node.Stop 使用 `stopped.CompareAndSwap` 防重入。

### 完成情况

| 子任务 | 内容 | 状态 | 产物 |
|--------|------|------|------|
| P1-4.1 | 梳理当前停止顺序 | ✅ | 上方顺序表（已对比源码确认） |
| P1-4.2 | Node Stop 幂等测试 | ✅ | `node/node_stop_test.go` — 7 tests: 幂等/并发/逆序清理/panic恢复/空清理 |
| P1-4.3 | ServiceManager 停止测试 | ✅ | P1-3 已补：StopAll 幂等/空列表/Start 空列表（3 tests） |
| P1-4.4 | Pool Stop goroutine 退出 | ✅ | `pool/manager_runtime_test.go` — 5 tests: 幂等/goroutine退出/连接关闭/PoolManager Close/Close幂等 |
| P1-4.5 | in-flight 关闭行为 | ✅ | P0 已有 PostJob_Suspended 测试 + P1-3 Service Stop 幂等/并发测试覆盖 |
| P1-4.6 | 文档回填 + 基线验证 | ✅ | 本文档更新 + build/vet/test/race 全绿 |

### 新增测试总计：12 tests（node 7 + pool 5）

### 验收标准 — 全部达成

- [x] `Stop()`、`BeginStop()`、`Wait()` 多次调用不 panic、不死锁
- [x] 停止过程中新请求被明确拒绝（Mailbox Suspend + PostJob 拒绝已有 P0 测试）
- [x] 所有后台 ticker/goroutine 在 Stop 后可退出（Pool wg.Wait 验证通过）
- [x] P0 Mailbox 和 RPC race 门禁继续通过
- [x] `go build/vet/test ./...` + `go test -race -count=2` 全量通过

---

## 八、推荐实施顺序

```text
Step 1：P1-1 errorx 契约收敛
  └── 先固定错误码和错误链行为，避免后续测试/metrics 记录的错误形态摇摆

Step 2：P1-2 Pool Metrics MVP
  └── 复用已有 RuntimeSnapshot/PoolMetrics，完成最小可导出链路

Step 3：P1-3 核心测试扩展
  └── 围绕 errorx、metrics、关闭流程可能影响的 core/rpc/event/node 补保护

Step 4：P1-4 优雅关闭完善
  └── 在 P0 mailbox 关闭契约之上串起 Node/Service/Pool/Event 全局顺序

Step 5：P1 收口验证与文档回填
  └── 更新 ROADMAP/NEXT_GOALS，明确进入 P2 可观测性全量集成的条件
```

---

## 九、验证命令

### P1 最小验证

```powershell
go test ./engine/pkg/utils/errorx/... ./engine/pkg/utils/errorlib/... -count=1
go test ./engine/pkg/rpc/client/pool/... ./engine/pkg/node/... -count=1
go test ./engine/pkg/core/... ./engine/pkg/rpc/... ./engine/pkg/event/... -count=1
```

### P1 并发验证

```powershell
go test -race -count=1 ./engine/pkg/rpc/client/pool/...
go test -race -count=1 ./engine/pkg/core/... ./engine/pkg/rpc/... ./engine/pkg/event/... ./engine/pkg/node/...
go test -race -count=2 ./engine/pkg/actor/...
```

### P1 收口验证

```powershell
go build ./...
go vet ./...
go test ./... -count=1
go test -race -count=2 ./engine/pkg/actor/...
go test -race -count=1 ./engine/pkg/core/... ./engine/pkg/rpc/... ./engine/pkg/event/... ./engine/pkg/node/... ./engine/pkg/config/...
```

---

## 十、完成定义

P1 可以关闭的条件：

- [x] P1-1：`errorx` 行为测试补齐，错误码分段和 `errorlib` 兼容策略明确。
- [x] P1-2：Pool Metrics 最小导出链路完成，空池/并发/停止后读取均有测试。
- [x] P1-3：core/node/rpc/event 高价值路径新增回归测试，race 稳定。
- [x] P1-4：Node/Service/Pool/Event 全局关闭顺序明确并有测试保护。
- [x] `go build ./...`、`go vet ./...`、`go test ./... -count=1` 全部通过。
- [x] P0 完整验证命令仍通过，无稳定性回归。
- [x] ROADMAP/NEXT_GOALS 回填 P1 状态，并列出进入 P2 的剩余风险。

---

## 十一、风险与缓解措施

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| `errorlib` 调用点较多 | 一次性迁移风险高 | P1 先明确兼容层和新代码使用规则，不强制全仓替换 |
| Prometheus 依赖引入过早 | 增加依赖和 API 锁定成本 | 先定义 adapter 和 snapshot 转换，必要时再接 `client_golang` |
| Node smoke test 依赖配置和外部服务 | 测试不稳定 | 使用最小本地配置、fake service、禁用远程依赖 |
| 优雅关闭牵涉组件多 | 容易扩大重构范围 | 先画顺序、补测试，再做最小修复；不在 P1 重写生命周期架构 |
| race 测试耗时增加 | 本地/CI 反馈变慢 | 区分最小门禁和收口门禁，记录耗时后再纳入 CI |

---

## 十二、首轮推荐切片

如果下一轮直接开始编码，建议从 **P1-1.1 + P1-1.2** 开始：

1. 盘点 `errorx`/`errorlib` 调用点。
2. 补齐 `errorx` 的边界测试。
3. 跑 `go test ./engine/pkg/utils/errorx/... ./engine/pkg/utils/errorlib/... -count=1`。
4. 文档记录错误码分段草案。

这一步改动小、依赖少，又能为后续 RPC 错误传播、metrics 错误 label 和关闭流程错误返回提供统一语义。