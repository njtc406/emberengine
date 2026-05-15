# P2 可观测性 MVP 开发文档

> 创建时间：2026年5月13日  
> 来源：`docs/ROADMAP.md` 的 P2 中期任务与 `docs/NEXT_GOALS.md` 的 Phase A 可观测性目标  
> 前置条件：P0/P1 已完成，`go build ./...`、`go vet ./...`、`go test ./...`、关键 race 测试全部通过

---

## 一、P2 总目标

P2 的核心目标是把 P1 已经完成的 `RuntimeSnapshot`、`PoolMetrics`、核心测试和全局关闭顺序，扩展成可被 Prometheus、Kubernetes 探针和后续 OpenTelemetry 直接接入的可观测性 MVP。

P2 不追求一次性完成全量监控体系，而是先建立稳定的最小闭环：

1. `/metrics` 可以输出 Prometheus text 格式指标。
2. `/health` 和 `/ready` 可以用于存活和就绪检查。
3. Node/Pool/RPC/Mailbox/Event 的核心运行状态可以被采集。
4. TraceID 在主要链路中不丢失，为后续 OpenTelemetry 接入铺路。
5. 所有新增能力都有单元测试、集成测试和 race 验证。

---

## 二、P2 任务状态

| 编号 | 问题 | 当前状态 | 本文档处理方式 |
|------|------|----------|----------------|
| P2-1 | Prometheus 集成 | ✅ 已完成 | 扩展 `metrics` 包，统一 snapshot 到 Prometheus text 的转换 |
| P2-2 | Health/Ready/Metrics 端点 | ✅ 已完成 | 新增 sysService/healthservice，提供 `/health`、`/ready`、`/metrics` |
| P2-3 | RPC 指标埋点 | ✅ 已完成 | 在 MessageBus Call/AsyncCall/Send 统计 total/errors/in-flight |
| P2-4 | Mailbox/Event 指标埋点 | ✅ 已完成 | 统计队列投递/拒绝/丢弃、事件发布/投递/限流 |
| P2-5 | TraceID 贯通验证 | ✅ 已完成 | traceId 全链路不丢失验证 + tracing 接口预留 |
| P2-6 | 文档与门禁回填 | ✅ 已完成 | 更新 ROADMAP/NEXT_GOALS，记录 P2 验证命令和剩余风险 |

---

## 三、开发原则

1. **先采集快照，再侵入热路径**：优先复用 `RuntimeSnapshot`、`PoolMetrics` 等已有数据源，减少对业务路径的影响。
2. **指标命名稳定**：指标名、单位和 label 一旦确定，应避免在 P2 内频繁变化。
3. **控制 label cardinality**：第一版只允许有限 label，如 `service`、`method`、`status`、`pool`、`event_type`。
4. **pull 模式优先**：优先使用 atomic counter + snapshot pull，避免每次请求构造复杂对象。
5. **HTTP 端点纳入生命周期**：metrics/health 服务必须能被 Node Stop 顺序管理，并有幂等关闭测试。
6. **OTel 不抢跑**：P2 前半段只做 TraceID 贯通验证和接口预留，避免一开始引入过重依赖。

---

## 四、P2-1：Prometheus Metrics 基础层

### 问题描述

P1 已经新增 `engine/pkg/metrics`，能够把 PoolMetrics 转换为 Prometheus text。P2-1 要把它扩展为统一的 metrics adapter，让 Node/Pool/RPC/Mailbox/Event 都能输出稳定的指标样本。

### 重点文件

| 文件 | 关注点 |
|------|--------|
| `engine/pkg/metrics/` | MetricDesc、MetricSample、Prometheus text adapter、统一转换入口 |
| `engine/pkg/node/diagnostics.go` | RuntimeSnapshot 聚合入口 |
| `engine/pkg/rpc/client/pool/manager_types.go` | PoolMetrics 字段与单位 |
| `engine/pkg/interfaces/` | 如需新增 metrics collector 接口，优先放在接口层 |

### 指标命名规范

| 类型 | 命名规则 | 示例 |
|------|----------|------|
| Counter | `_total` 结尾 | `ember_rpc_requests_total` |
| Gauge | 描述当前状态 | `ember_node_services_total`、`ember_pool_connections_active` |
| Duration | 秒为单位，`_seconds` 结尾 | `ember_rpc_request_duration_seconds` |
| In-flight | 当前进行中数量 | `ember_rpc_in_flight` |

### 开发任务

| 子任务 | 内容 | 产物 |
|--------|------|------|
| P2-1.1 | 扩展 `metrics` 包统一 sample/desc 定义 | `engine/pkg/metrics` |
| P2-1.2 | 增加 Node/RuntimeSnapshot 到 samples 的转换 | `metrics` + `node` 测试 |
| P2-1.3 | 保持 P1 PoolMetricsToText 兼容 | 回归测试 |
| P2-1.4 | 稳定排序、label 转义、nil/空 snapshot 处理 | adapter 测试 |
| P2-1.5 | 文档记录指标名、单位、label | 本文档回填 |

### 验收标准

- `go test ./engine/pkg/metrics/... ./engine/pkg/node/... -count=1` 通过。
- `go test -race ./engine/pkg/metrics/... ./engine/pkg/node/... -count=1` 通过。
- nil snapshot、空 pool、停止后读取不 panic。
- Prometheus text 输出顺序稳定，label 转义正确。

---

## 五、P2-2：Health/Ready/Metrics HTTP 端点

### 问题描述

框架当前已有 pprof 和 RuntimeSnapshot 能力，但缺少标准运维入口。P2-2 要提供 `/health`、`/ready`、`/metrics` 三个基础端点，供 Prometheus、Kubernetes 或外部运维系统使用。

### 重点文件

| 文件 | 关注点 |
|------|--------|
| `engine/pkg/sysService/` | 建议新增 health/metrics sysService |
| `engine/pkg/node/node.go` | Node 持有服务生命周期和 stopCleanups |
| `engine/pkg/node/diagnostics.go` | `/metrics` 数据来源 |
| `template/config/` | 新增或扩展 health/metrics 端口配置 |
| `example/configs/` | 示例配置回归 |

### 端点语义

| 端点 | 语义 | 返回 |
|------|------|------|
| `/health` | 进程存活检查 | 进程可响应即 200 |
| `/ready` | 节点是否可接流量 | Node 已启动、核心组件可用、Service 可服务时 200 |
| `/metrics` | Prometheus 指标 | text/plain; version=0.0.4 |

### 开发任务

| 子任务 | 内容 | 产物 |
|--------|------|------|
| P2-2.1 | 设计 health/metrics 服务配置 | config/template 更新 |
| P2-2.2 | 实现 `/health` handler | handler 单测 |
| P2-2.3 | 实现 `/ready` handler | ready 状态单测 |
| P2-2.4 | 实现 `/metrics` handler | metrics HTTP 单测 |
| P2-2.5 | 纳入 Node Stop 顺序 | Stop 幂等/race 测试 |

### 验收标准

- 三个端点均可在无真实 etcd/NATS 的测试环境下验证。
- HTTP server Stop 多次调用不 panic、不泄漏 goroutine。
- `/metrics` 并发请求在 `-race` 下通过。
- 示例配置仍可通过 Config.Load 回归测试。

---

## 六、P2-3：RPC 指标埋点

### 问题描述

RPC 是线上排障的第一入口。P2-3 要在不改变 Envelope/Job 释放语义、不破坏 P1 错误传播测试的前提下，统计 RPC 请求量、错误量、耗时和 in-flight。

### 重点文件

| 文件 | 关注点 |
|------|--------|
| `engine/pkg/rpc/message/msgbus/bus.go` | Call/AsyncCall/Send 入口 |
| `engine/pkg/monitor/` | 现有 RpcMonitor 能否复用 |
| `engine/pkg/utils/errorx/` | 错误码 label 的后续来源 |
| `engine/pkg/metrics/` | RPC sample 转换 |

### 核心指标

```text
ember_rpc_requests_total{service,method,status}
ember_rpc_request_duration_seconds{service,method}
ember_rpc_in_flight{service,method}
ember_rpc_errors_total{service,method,error_code}
```

### 开发任务

| 子任务 | 内容 | 产物 |
|--------|------|------|
| P2-3.1 | 盘点现有 RpcMonitor 与 MessageBus 的统计能力 | 调研记录 |
| P2-3.2 | 在 Call/AsyncCall/Send 入口增加 total/in-flight | msgbus 测试 |
| P2-3.3 | 统计错误码与 status label | error path 测试 |
| P2-3.4 | 统计 duration | 耗时指标测试 |
| P2-3.5 | 验证 context cancel/timeout 不破坏指标 | race + 单测 |

### 验收标准

- `go test ./engine/pkg/rpc/message/msgbus/... -count=1` 通过。
- `go test -race ./engine/pkg/rpc/message/msgbus/... -count=2` 通过。
- Call/AsyncCall/Send 成功、失败、空 MultiBus、context cancel 均有指标测试。
- 指标采集不改变原有错误返回行为。

---

## 七、P2-4：Mailbox/Event 指标埋点

### 问题描述

Mailbox 和 Event 是 Actor 框架的调度和异步通信核心。P2-4 要让队列深度、处理耗时、丢弃消息、事件发布/投递/失败在指标中可见。

### 重点文件

| 文件 | 关注点 |
|------|--------|
| `engine/pkg/actor/mailbox/` | PostJob、worker、worker_pool、Suspend/Drop |
| `engine/pkg/event/processor.go` | 本地事件 Trigger/handler 执行 |
| `engine/pkg/event/eventBus.go` | 跨节点事件 bus、batch、Stop |
| `engine/pkg/metrics/` | Mailbox/Event samples |

### 核心指标

```text
ember_mailbox_queue_size{service,priority}
ember_mailbox_job_duration_seconds{service,job_type}
ember_mailbox_workers_active{service}
ember_mailbox_dropped_total{service,reason}

ember_event_published_total{event_type,scope}
ember_event_delivered_total{event_type,scope}
ember_event_errors_total{event_type,scope}
ember_event_throttled_total{event_type,scope}
```

### 开发任务

| 子任务 | 内容 | 产物 |
|--------|------|------|
| P2-4.1 | 盘点 Mailbox/Event 已有统计点 | 调研记录 |
| P2-4.2 | Mailbox queue/dropped/active workers 指标 | mailbox 测试 |
| P2-4.3 | Mailbox job duration 指标 | worker 测试 |
| P2-4.4 | Event published/delivered/errors 指标 | event 测试 |
| P2-4.5 | Event throttled 指标 | 限流场景测试 |

### 验收标准

- Mailbox Suspend/Drop 路径指标准确。
- Event handler error/panic 不阻塞后续 handler，且错误指标递增。
- 新增指标在 `-race` 下稳定通过。
- 热路径额外分配可控。

---

## 八、P2-5：TraceID 贯通验证与 OTel 骨架

### 问题描述

OpenTelemetry 接入前，必须先证明现有 `xcontext.traceId` 和 ContextHeaders 在主要链路中不丢失。P2-5 先做验证和接口预留，不急于引入完整 SDK。

### 重点文件

| 文件 | 关注点 |
|------|--------|
| `engine/pkg/utils/xcontext/` | traceId/header 存取 |
| `engine/pkg/rpc/message/msgenvelope/` | envelope header 传递 |
| `engine/pkg/rpc/message/msgbus/` | Call/AsyncCall/Send header 传播 |
| `engine/pkg/rpc/remote/` | 后续远程传播落点 |

### 开发任务

| 子任务 | 内容 | 产物 |
|--------|------|------|
| P2-5.1 | 盘点 traceId 当前生成和传递路径 | 调研记录 |
| P2-5.2 | 本地 RPC traceId 不丢失测试 | msgbus 测试 |
| P2-5.3 | Envelope encode/decode header 不丢失测试 | msgenvelope 测试 |
| P2-5.4 | 定义最小 tracing interface | `metrics` 或 `tracing` 包 |
| P2-5.5 | 规划 OTel SDK 接入边界 | 文档回填 |

### 验收标准

- traceId 在 envelope/header 转换中保持一致。
- 没有 traceId 时行为保持兼容。
- 不强制引入 OTel SDK；只预留接口或设计落点。

### P2-5 实施结果

#### TraceID 传递验证结论

| 链路阶段 | 传递方式 | 验证状态 |
|----------|----------|----------|
| `xcontext.NewWithCloneCtx` | 从旧 ctx 复制 headers 到新 ctx（切断 Cancel 传播） | ✅ traceId 保持 |
| `MsgEnvelope.ToProtoMsg` | `emberctx.ToHeaders(ctx)` → `actor.Message.ContextHeaders` | ✅ traceId 保持 |
| 远程 Handler 接收 | `ContextHeaders` → `xcontext.New().AddHeaders()` 重建 ctx | ✅ traceId 保持 |
| 本地 Sender | `ctx` 直接传递给 `job.SetContext(ctx)` | ✅ traceId 保持 |
| Event `marshalEvent` | `emberctx.ToHeaders(ctx)` → `actor.Event.ContextHeaders` | ✅ traceId 保持 |
| Event `unmarshalEvent` | `buildContextFromHeaders` 重建 ctx | ✅ traceId 保持 |
| Mailbox Worker 执行 | `job.GetContext()` 保持原始 ctx | ✅ traceId 保持 |
| 无 traceId 场景 | 空 headers 不 panic | ✅ 兼容 |

#### OTel 接入边界规划

P2 已完成 `engine/pkg/tracing` 最小接口定义：

| 接口 | 职责 | OTel 适配方式 |
|------|------|---------------|
| `ITracer` | 创建 Span，判断启用状态 | 实现 `otelTracer` 包装 `trace.Tracer` |
| `ISpan` | Span 生命周期、属性、错误记录 | 实现 `otelSpan` 包装 `trace.Span` |
| `SetGlobalTracer` | 注入全局 tracer | Node 初始化时调用一次 |
| `GlobalTracer` | 获取全局 tracer | 框架内部使用 |

P3 接入 OTel 的推荐步骤：
1. 引入 `go.opentelemetry.io/otel` SDK
2. 实现 `otelTracer`/`otelSpan` 适配 `tracing.ITracer`/`tracing.ISpan`
3. 在 `Node.Init` 中调用 `tracing.SetGlobalTracer(otelTracer)`
4. 在 `bus.call()`/`bus.asyncCall()` 入口调用 `GlobalTracer().Start(ctx, "rpc.Call")`
5. 将 `span.SpanContext()` 中的 W3C trace headers 写入 `ContextHeaders` 传播

---

## 九、推荐实施顺序

```text
Step 1：P2-1 Metrics 基础层
  └── 先统一样本模型、命名、Prometheus text 输出

Step 2：P2-2 Health/Ready/Metrics HTTP 端点
  └── 先跑通外部可见入口，让 Prometheus/K8s 能接入

Step 3：P2-3 RPC 指标埋点
  └── 优先覆盖最常用、最影响排障的 Call/AsyncCall/Send

Step 4：P2-4 Mailbox/Event 指标埋点
  └── 补齐 Actor 调度和事件系统可见性

Step 5：P2-5 TraceID 贯通验证
  └── 为后续 OpenTelemetry 链路追踪打基础

Step 6：P2 收口验证与文档回填
  └── 更新 ROADMAP/NEXT_GOALS，明确进入 P3 的剩余风险
```

---

## 十、验证命令

### P2 最小验证

```powershell
go test ./engine/pkg/metrics/... ./engine/pkg/node/... -count=1
go test ./engine/pkg/sysService/... -count=1
go test ./engine/pkg/rpc/message/msgbus/... -count=1
```

### P2 并发验证

```powershell
go test -race -count=2 ./engine/pkg/metrics/... ./engine/pkg/node/...
go test -race -count=2 ./engine/pkg/sysService/...
go test -race -count=2 ./engine/pkg/rpc/message/msgbus/... ./engine/pkg/actor/mailbox/... ./engine/pkg/event/...
```

### P2 收口验证

```powershell
go build ./...
go vet ./...
go test ./... -count=1
go test -race -count=2 ./engine/pkg/metrics/... ./engine/pkg/node/... ./engine/pkg/sysService/... ./engine/pkg/rpc/message/msgbus/... ./engine/pkg/actor/mailbox/... ./engine/pkg/event/...
```

---

## 十一、完成定义

P2 可以关闭的条件：

- [x] P2-1：Prometheus metrics 基础层完成，Node/Pool snapshot 可输出稳定 text。
- [x] P2-2：`/health`、`/ready`、`/metrics` 三个端点可测试、可关闭、可并发访问。
- [x] P2-3：RPC Call/AsyncCall/Send 具备 total/errors/in-flight/duration 指标。
- [x] P2-4：Mailbox/Event 核心指标可见，Drop/Error/Throttle 路径有测试。
- [x] P2-5：TraceID 贯通验证完成，并明确 OTel 接入边界。
- [x] `go build ./...`、`go vet ./...`、`go test ./... -count=1` 全部通过。
- [x] 关键包 race 验证通过，无稳定性回归。
- [x] ROADMAP/NEXT_GOALS 回填 P2 状态，并列出进入 P3 的剩余风险。

**P2 完成时间**: 2026-05-13

---

## 十二、进入 P3 的剩余风险

| 风险 | 说明 | 建议处理时机 |
|------|------|--------------|
| OTel SDK 未引入 | P2 只预留了 `tracing.ITracer`/`ISpan` 接口，实际 SDK 接入留给 P3 | P3 早期 |
| 指标缺少 per-service label | 当前 Mailbox/Event 指标是节点级聚合，不区分具体 Service | P3 按需扩展 |
| `/metrics` 端点无认证 | health/metrics HTTP 端点对外暴露无 TLS/Token 保护 | P3 安全认证体系 (B-1) |
| 远程 RPC duration 未埋点 | 当前 RPC duration 只在 MessageBus 本地层，不含网络传输耗时 | P3 RPC 韧性增强 |
| Event throttle 指标依赖 NATS | 跨节点事件限流计数需真实 NATS，单元测试用 mock 覆盖 | 持续 |
| 无 Grafana dashboard 模板 | 有 Prometheus text 但无预置 dashboard | P3/P4 运维文档 |

---

## 十三、首轮推荐切片

建议从 **P2-1.1 + P2-1.2 + P2-2.4** 开始：

1. 扩展 `engine/pkg/metrics` 的统一 sample model。
2. 把 `RuntimeSnapshot` + `PoolMetrics` 转成统一 Prometheus text。
3. 先实现不依赖真实 Node 启动的 `/metrics` handler 单元测试。
4. 跑通 `go test ./engine/pkg/metrics/... ./engine/pkg/node/... -count=1`。
5. 再补 `/health` 和 `/ready`，把 HTTP 运维入口闭环起来。

这一步最贴近 P1 已完成资产，改动小、反馈快，并能为后续 RPC/Mailbox/Event 指标埋点提供统一出口。
