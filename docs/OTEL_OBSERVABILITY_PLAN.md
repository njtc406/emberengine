# OpenTelemetry 可观测性完善方案

> 创建时间：2026年6月10日  
> 来源：`docs/NEXT_GOALS.md` 的 A-1-8 OpenTelemetry SDK 适配器任务，以及 `docs/P2_OBSERVABILITY_DEV_PLAN.md` 的 P2 后续演进目标  
> 当前状态：方案设计阶段  
> 前置条件：P2 可观测性 MVP 已完成，`/health`、`/ready`、`/metrics`、Prometheus text metrics、TraceID 贯通和 tracing noop 接口已具备

---

## 一、目标

本方案用于补齐 EmberEngine 当前可观测性体系中“分布式 Trace 可导出、可关联、可采样”的能力。

P2 阶段已经完成 Metrics/Health/TraceID 骨架，本阶段不重做已有能力，而是在现有 `engine/pkg/tracing` 抽象上增加 OpenTelemetry SDK 适配器，并逐步接入 RPC、Event、Node 生命周期等关键路径。

核心目标：

1. 提供默认关闭、显式启用的 OpenTelemetry tracing adapter。
2. 明确 tracing 接入路线：优先保留 `engine/pkg/tracing` facade，但其接口语义需要立即对齐 OTel 的 `SpanKind`、attributes、links/events；不要把 SpanKind 仅作为后续优化。
3. 将 RPC client/server、Event publish/consume 等关键路径纳入 span。
4. 复用现有 `Message.ContextHeaders` / `Event.ContextHeaders` 作为 W3C `traceparent` / `tracestate` 的传播通道，不重新设计 envelope/header。
5. 明确 OTel 是可选增强：未启用或未引入 OTel 时，继续使用现有 `xcontext.traceId` 保障链路可分析；启用 OTel 后使用标准 trace/span context。
6. 建立 trace 与日志、metrics 的关联规则：启用 OTel 时日志输出 `otel.trace_id` / `otel.span_id`，同时保留 `ember.traceId`。
7. 为后续 Jaeger、Tempo、OTLP Collector 接入提供标准出口。

### 1.1 OpenTelemetry 是什么

OpenTelemetry，简称 OTel，不是单一后端服务，而是一套 CNCF 标准和对应 SDK/工具链，目标是统一生成、传播、采集和导出可观测性数据。

它通常包含几层：

| 层次 | 说明 | 在本项目中的意义 |
|------|------|------------------|
| API | 定义 tracer、span、meter、context propagation 等标准接口 | 代码中创建 span、注入/提取 trace context |
| SDK | API 的具体实现，负责采样、资源信息、span processor 等 | Node 启动时初始化 TracerProvider |
| Exporter | 把 trace/metrics/logs 导出到 stdout、OTLP Collector、Jaeger、Tempo 等 | 生产环境通常使用 OTLP exporter |
| Collector | 可选独立进程，接收、处理、转发观测数据 | 用于解耦业务进程和后端存储 |
| Backend | Jaeger、Tempo、Prometheus、Grafana 等展示/存储系统 | 最终查询链路和指标 |

因此，本方案中的 “接入 OTel” 主要是指在框架内接入 Go 版 OpenTelemetry API/SDK，并提供 exporter 配置。它不是要求框架内置 Jaeger/Tempo，也不是要求所有环境必须部署 Collector。

推荐模式：

- **默认/轻量模式**：不启用 OTel，继续使用现有 `ember.traceId` / `xcontext.traceId`，日志和框架上下文中仍可分析调用链。
- **标准分布式追踪模式**：启用 OTel，使用标准 W3C `traceparent` / `tracestate` 传播，并导出 span 到 stdout、OTLP Collector、Jaeger 或 Tempo。
- **迁移期兼容**：即使启用 OTel，也保留 `ember.traceId`，并将其作为 span attribute，便于和现有日志、错误、指标关联。

---

## 二、当前现状

### 2.1 已完成能力

| 能力 | 状态 | 关键文件 |
|------|------|----------|
| Prometheus text metrics | ✅ 已完成 | `engine/pkg/metrics/` |
| RuntimeSnapshot 聚合 | ✅ 已完成 | `engine/pkg/node/diagnostics.go` |
| `/health` / `/ready` / `/metrics` | ✅ 已完成 | `engine/pkg/sysService/healthservice/healthservice.go` |
| RPC 基础指标 | ✅ 已完成 | `engine/pkg/rpc/message/msgbus/rpc_metrics.go` |
| Mailbox/Event/Pool/Node 指标 | ✅ 已完成 | `engine/pkg/metrics/*_metrics.go` |
| TraceID 贯通验证 | ✅ 已完成 | `engine/pkg/utils/xcontext/traceid_test.go` |
| Tracing 抽象接口 | ✅ 已完成 | `engine/pkg/tracing/tracer.go` |

### 2.1.1 已存在的传播通道

当前代码已经具备跨 RPC/Event wire 传播上下文的基础设施，不需要重新设计协议字段：

| 通道 | 现状 | 结论 |
|------|------|------|
| RPC message | `actor.Message.ContextHeaders map[string]string` | 可直接承载 `traceparent`、`tracestate`、`baggage`、`ember.traceId` |
| Event message | `actor.Event.ContextHeaders map[string]string` | 可直接承载事件 publish/consume 的 trace context |
| RPC outbound | `MsgEnvelope.ToProtoMsg(ctx)` 已调用 `emberctx.ToHeaders(ctx)` | 只要把 OTel propagation 注入到 ctx headers，现有序列化会自动带出 |
| RPC inbound | `remote/handler.RpcMessageHandler` 已从 `req.ContextHeaders` 恢复到 context headers | server span 可基于已恢复后的 ctx 创建 |

因此，实施重点不是新增 header 字段，而是在现有 `emberctx` / `xcontext` header map 与 OTel propagator 之间提供注入/提取适配，并补充 `traceparent` 贯通测试。

### 2.2 当前缺口

| 缺口 | 影响 | 优先级 |
|------|------|--------|
| 未接 OpenTelemetry SDK | TraceID 只能在框架内流转，无法导出到 Jaeger/Tempo/Collector | 高 |
| Node 生命周期未管理 TracerProvider | exporter flush/shutdown 不可控 | 高 |
| RPC client/server 未形成 span | 无法定位跨节点调用耗时和错误边界 | 高 |
| EventBus 未形成 producer/consumer span | 异步事件链路不可见 | 中 |
| RPC metrics 缺 duration 维度 | Prometheus 侧只能看次数/错误/in-flight，无法看延迟分布 | 中 |
| 配置缺 observability/tracing 节 | 无法按环境启停、采样、切换 exporter | 高 |
| OTel 与现有 TraceID 边界不清 | 未启用 OTel 时可能丢失现有链路分析能力 | 高 |
| 日志缺 OTel trace/span 关联字段 | 即使有 span，也难以从日志跳转到 trace | 高 |
| Resource 属性未规范 | 后端难以按服务、节点、环境聚合查询 | 中 |
| transport instrumentation 边界未定义 | gRPC/NATS/rpcx 与框架层 span 可能重复或缺失 | 中 |

---

## 三、设计原则

1. **默认关闭**：未配置时继续使用 `noopTracer`，不引入额外后台 goroutine 和 exporter。
2. **接口语义对齐**：框架可保留 `tracing` facade 降低侵入，但 facade 必须表达 OTel 原生语义，包括 `SpanKind`、attributes、links/events 和 status；否则 RPC/Event span 会失真。
3. **低侵入**：优先在 RPC、Event 等统一入口埋点，不在业务 Service/Module 中散落 span 创建逻辑。
4. **低基数属性**：span attribute 不记录 request/response payload，不使用高基数 label。
5. **生命周期完整**：Node 初始化时创建 provider，Node 停止时 flush/shutdown。
6. **错误可关联**：span 记录 `errorx` 错误码、错误类型和 status，但不泄露敏感字段。
7. **TraceID fallback**：OTel 未启用时必须继续使用现有 `xcontext.traceId` 保证链路可分析；OTel 启用时使用标准 trace/span context，同时保留 `ember.traceId` 作为兼容字段。
8. **成熟方案优先**：Metrics 可以优先切换到成熟的 `client_golang` Registry/Collector 方案；现有自研 Prometheus text 输出不要求长期保留，只需保证迁移期间 `/metrics` 行为可验证。
9. **复用现有传播通道**：RPC/Event 已有 `ContextHeaders`，OTel propagation 应基于现有字段实现，不增加新的 wire 字段。
10. **日志链路可跳转**：所有框架日志应能从 context 提取 `ember.traceId`；启用 OTel 后额外提取 `otel.trace_id`、`otel.span_id`。

---

## 四、架构变更

### 4.1 新增 OTel adapter 包

建议新增：

```text
engine/pkg/tracing/oteltracer/
├── config.go
├── exporter.go
├── tracer.go
└── tracer_test.go
```

职责：

- 实现 `tracing.ITracer`。
- 包装 OTel `trace.Tracer`。
- 支持 stdout / otlp / noop exporter。
- 封装 resource、sampler、batch span processor 初始化。
- 暴露 `Shutdown(ctx)`，由 Node 生命周期调用。

推荐 Resource 属性：

| 属性 | 来源 | 说明 |
|------|------|------|
| `service.name` | `Tracing.ServiceName`，默认 `emberengine` | OTel 后端聚合的核心维度 |
| `service.version` | 构建信息或配置，可选 | 用于灰度/版本排障 |
| `service.instance.id` | Node UID / runtime node id | 区分同一服务的不同节点实例 |
| `deployment.environment` | 配置，可选 | `local` / `dev` / `staging` / `prod` |
| `telemetry.sdk.language` | OTel SDK 自动设置 | Go SDK 标准属性 |
| `telemetry.sdk.name` | OTel SDK 自动设置 | Go SDK 标准属性 |

Resource 属性必须在 provider 初始化时设置，不能作为每个 span 的重复 attribute 写入。

> 说明：`noop exporter` 只作为 OTel adapter 内部 exporter 选项；未启用 tracing 时仍应直接使用现有 `tracing.noopTracer` 和 `xcontext.traceId`，避免创建 OTel SDK 组件和后台 goroutine，同时保留当前链路分析能力。

### 4.1.1 OTel 可选模式与 fallback 行为

OTel 必须是可选能力，不应成为框架启动的硬依赖。

| 模式 | 行为 | 适用场景 |
|------|------|----------|
| OTel 未引入/未启用 | 使用现有 `xcontext.traceId`、日志上下文字段和 RPC/Event header 继续传播 `ember.traceId` | 默认、本地、轻量部署 |
| OTel 启用但 exporter 为 stdout | 创建标准 span，本地输出，保留 `ember.traceId` attribute | 开发调试 |
| OTel 启用且 exporter 为 otlp | 创建标准 span，通过 OTLP 导出到 Collector/Jaeger/Tempo，保留 `ember.traceId` attribute | 生产观测 |
| OTel 初始化失败 | 记录错误后回退到现有 `noopTracer` + `xcontext.traceId` | 防止可观测性故障影响业务启动 |

成功标准中必须包含：关闭或移除 OTel 后，现有 TraceID 链路仍可通过日志和上下文分析。

### 4.2 新增配置结构

建议在 `engine/pkg/config/define.go` 中增加 `ObservabilityConf` / `TracingConf`，并挂载到 `NodeConf` 下：

```go
type NodeConf struct {
   // ... existing fields ...
   ObservabilityConf *ObservabilityConf
}
```

原因：当前配置根结构为 `Config.NodeConf` / `Config.ClusterConf` / `Config.SystemLogger` / `Config.ServiceConf`，运行时相关配置已集中在 `NodeConf` 下；可观测性也属于 Node 运行时能力，放入 `NodeConf` 更符合现有配置风格。

| 配置 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `NodeConf.ObservabilityConf.Enable` | bool | false | 可观测性总开关，第一阶段可只影响 tracing |
| `NodeConf.ObservabilityConf.Tracing.Enable` | bool | false | tracing 开关 |
| `NodeConf.ObservabilityConf.Tracing.ServiceName` | string | `emberengine` | OTel resource service.name |
| `NodeConf.ObservabilityConf.Tracing.Exporter` | string | `noop` | `noop` / `stdout` / `otlp` |
| `NodeConf.ObservabilityConf.Tracing.Endpoint` | string | 空 | OTLP endpoint |
| `NodeConf.ObservabilityConf.Tracing.SampleRatio` | float64 | `0.01` | 采样率，范围 0~1 |
| `NodeConf.ObservabilityConf.Tracing.Insecure` | bool | false | OTLP gRPC 是否禁用 TLS |
| `NodeConf.ObservabilityConf.Tracing.Timeout` | duration | `5s` | exporter shutdown / export timeout |
| `NodeConf.ObservabilityConf.Tracing.Environment` | string | 空 | OTel `deployment.environment` |
| `NodeConf.ObservabilityConf.Tracing.ServiceVersion` | string | 空 | OTel `service.version` |

### 4.3 Node 生命周期接入

建议在 `engine/pkg/node/node.go` 的 `Node.Start` 初始化流程中接入：

1. 在配置和 logger 初始化完成后，读取 `NodeConf.ObservabilityConf.Tracing` 配置。
2. 未启用时设置 `tracing.SetGlobalTracer(nil)`，保持 noop tracer，但继续使用现有 `xcontext.traceId` 在日志、RPC/Event header 和上下文中传播链路标识。
3. 启用时创建 `oteltracer.TracerProvider`。
4. 调用 `tracing.SetGlobalTracer(adapter)`。
5. 注册 Stop cleanup，Node 停止时调用 `Shutdown(ctx)`。

当前 `engine/pkg/node/` 下没有独立 `init.go`，Node 初始化和停止流程均集中在 `node.go`：

- `Node.Start`：初始化配置、日志、基础设施、RPC、Cluster、EventBus、Service。
- `appendCleanup` / `stopCleanups`：维护失败回滚和 `Node.Stop` 清理顺序。
- `Node.Stop`：逆序执行 cleanup。

因此 OTel 生命周期应优先落在 `node.go`，不要在文档或实现中假设存在 `engine/pkg/node/init.go`。

### 4.4 Tracer shutdown 接口

当前 `tracing.ITracer` 只有：

```go
type ITracer interface {
   Start(ctx context.Context, operationName string) (context.Context, ISpan)
   IsEnabled() bool
}
```

它没有 `Shutdown` 方法。为避免扩大所有 tracer 实现的强制接口，可以新增可选接口：

```go
type Shutdowner interface {
   Shutdown(ctx context.Context) error
}
```

Node cleanup 中通过类型断言调用：

```go
if s, ok := adapter.(tracing.Shutdowner); ok {
   _ = s.Shutdown(ctx)
}
```

也可以由 `oteltracer.New` 返回具体 adapter，Node 保存具体引用并注册 cleanup。实施时二选一即可，但文档和代码必须明确 shutdown 所属对象。

候选文件：

| 文件 | 变更 |
|------|------|
| `engine/pkg/config/define.go` | 新增配置结构 |
| `engine/pkg/node/node.go` | 在 `Node.Start` 初始化 OTel adapter，在 `stopCleanups` 注册 shutdown |
| `template/config/` | 增加 tracing 配置示例 |
| `example/configs/` | 增加最小示例 |

### 4.5 Trace Context 传播设计

OTel span 能否形成跨节点链路，关键不只是创建 span，还必须在 RPC/Event 边界注入和提取 trace context。

现有 `xcontext.traceId` 是 EmberEngine 内部 TraceID，不能直接等同于 W3C Trace Context。建议策略：

1. 保留 `ember.traceId` 作为框架内部兼容字段。
2. 新增标准 W3C headers：`traceparent`、`tracestate`，后续按需支持 `baggage`。
3. RPC client 发送前把当前 OTel context 注入现有 context headers；`MsgEnvelope.ToProtoMsg(ctx)` 会通过 `emberctx.ToHeaders(ctx)` 写入 `actor.Message.ContextHeaders`。
4. RPC server 收到请求后，`remote/handler.RpcMessageHandler` 已把 `req.ContextHeaders` 恢复到 context headers；server span 应基于该 ctx 提取 OTel context 后创建。
5. Event publish 时把 OTel context 注入 `actor.Event.ContextHeaders`。
6. Event consume 时从 `actor.Event.ContextHeaders` 提取 OTel context，再创建 consumer span。
7. span attribute 中可以记录 `ember.trace_id`，但不要强行把 `ember.traceId` 转成 OTel trace id。

需要重点确认/修改的文件：

| 文件 | 说明 |
|------|------|
| `engine/pkg/utils/xcontext/` | 现有 trace id 与 context header 工具 |
| `engine/pkg/utils/emberctx/` | `ToHeaders` / header map 与 OTel propagator 的 carrier 适配 |
| `engine/pkg/actor/actor.proto` | 已有 `Message.ContextHeaders`、`Event.ContextHeaders`，不新增 wire 字段 |
| `engine/pkg/rpc/message/msgenvelope/` | 已把 ctx headers 写入 RPC message |
| `engine/pkg/rpc/remote/handler/handler.go` | 已把 RPC message headers 恢复到 ctx headers |
| `engine/pkg/rpc/message/msgbus/bus.go` | RPC client 创建 client span 并注入 context |
| `engine/pkg/core/rpc/handler.go` | RPC server 基于已恢复 context 创建 server span |
| `engine/pkg/event/bus_global.go` / `bus_server.go` / `bus_specific.go` | Event producer 注入 context |
| `engine/pkg/event/processor.go` | Event consumer 提取 context 并创建 consumer span |

### 4.6 `ITracer` 接口演进

当前 `ITracer.Start(ctx, operationName)` 无法表达 OTel SpanKind、links、attributes 等 options。该接口必须在阶段 1 扩展，或明确放弃 facade、改为框架层直接使用 OTel API：

此处需要在实施前做明确决策，不能把 SpanKind/options 推迟到“后续再评估”。对 RPC/Event 框架来说，`client`、`server`、`producer`、`consumer` 是链路语义的一部分，不只是展示字段。

业界常见做法有两类：

| 路线 | 做法 | 优点 | 代价 |
|------|------|------|------|
| A. 直接使用 OTel API | 框架 middleware/transport 层直接调用 `otel.Tracer(...).Start(ctx, name, trace.WithSpanKind(...))` | 与 OTel 生态完全一致，SpanKind/status/links/events 无损 | 框架核心包会直接依赖 OTel API |
| B. 保留框架 facade | `engine/pkg/tracing` 暴露与 OTel 语义对齐的 `SpanOption`，adapter 内转换到 OTel API | 降低框架其他包对 OTel 的直接依赖，便于默认 noop/fallback | 需要维护 facade 与 OTel 语义映射 |

本项目建议采用 **路线 B**：保留 `engine/pkg/tracing` facade，但在阶段 1 即扩展接口，确保后续 RPC/Event 埋点不需要二次大改。

```go
type SpanOption func(*SpanConfig)

type SpanKind string

const (
   SpanKindInternal SpanKind = "internal"
   SpanKindClient   SpanKind = "client"
   SpanKindServer   SpanKind = "server"
   SpanKindProducer SpanKind = "producer"
   SpanKindConsumer SpanKind = "consumer"
)

type ITracer interface {
   Start(ctx context.Context, operationName string, opts ...SpanOption) (context.Context, ISpan)
   IsEnabled() bool
}
```

最小 options 至少包含：

- `WithSpanKind(kind SpanKind)`。
- `WithAttributes(attrs ...Attribute)`。
- `WithLinks(links ...SpanLink)`，Event fan-out 或异步 callback 后续需要。
- `WithEvent(name string, attrs ...Attribute)` 或等价能力，可选。

如果坚持暂不扩展接口，则必须在文档中接受以下限制：

- client/server/producer/consumer 只能用属性表达，不能映射到 OTel 原生 `SpanKind`。
- 后续切换为原生 `SpanKind` 时需要一次接口兼容性调整。

因此，正式实施建议不要采用“仅 attribute 表达 kind”的临时方案。

### 4.7 日志与 Trace 关联

Trace 只有能和日志互相跳转，才具备生产排障价值。日志策略如下：

| 模式 | 日志字段 |
|------|----------|
| OTel 未启用 | 继续输出 `ember.traceId` / `xcontext.traceId` |
| OTel 启用且当前 ctx 有有效 span | 输出 `otel.trace_id`、`otel.span_id`、`ember.traceId` |
| OTel 启用但当前 ctx 无有效 span | 输出 `ember.traceId`，不伪造 OTel trace/span id |

建议在日志包增加或复用 `log.WithContext(ctx)` 一类入口，从 context 中提取上述字段并写入结构化日志。不要把业务 payload、token、用户隐私字段写入 trace/log correlation 字段。

### 4.8 Transport instrumentation 边界

EmberEngine 的核心语义在框架 RPC/Event 层，而底层 transport 包括 gRPC、NATS、rpcx。因此埋点分层建议为：

| 层级 | 第一阶段策略 | 说明 |
|------|--------------|------|
| 框架 RPC/Event span | 必做 | 表达 `rpc.call`、`rpc.handle`、`event.publish`、`event.consume` 等业务框架语义 |
| gRPC `otelgrpc` | 可选评估 | 可补充底层网络耗时，但可能与框架 RPC span 重叠 |
| NATS instrumentation | 可选评估 | 适合排查 broker/pubsub 层问题 |
| rpcx instrumentation | 可选评估或自定义 | 若无成熟 instrumentation，先不阻塞框架层 tracing |

第一阶段不要因为 transport instrumentation 复杂而延迟框架层 tracing。后续如果启用底层 instrumentation，应通过命名、SpanKind 和 attributes 明确区分 transport span 与 framework span，避免 trace 中出现含义重复的 span。

### 4.9 Go 依赖策略

建议将 OTel 依赖集中在 `engine/pkg/tracing/oteltracer` 及必要的 propagation adapter 中，避免业务示例和无关包直接 import SDK。

核心依赖：

| 依赖 | 用途 |
|------|------|
| `go.opentelemetry.io/otel` | OTel API、trace、propagation |
| `go.opentelemetry.io/otel/sdk` | TracerProvider、Resource、Sampler、SpanProcessor |
| `go.opentelemetry.io/otel/exporters/stdout/stdouttrace` | 本地 stdout exporter |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc` | 生产 OTLP gRPC exporter |
| `go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc` | 可选，gRPC transport instrumentation |

如果项目希望保持最小依赖树，可先只引入 API/SDK/stdout exporter，OTLP exporter 在配置/构建确认后再加入。但生产方案最终应支持 OTLP。

---

## 五、实施步骤

### 阶段 1：Tracing adapter 最小闭环

0. **扩展 tracing facade 语义**  
   文件：`engine/pkg/tracing/tracer.go`
   - 操作：扩展 `ITracer.Start(ctx, name, opts ...SpanOption)`，新增 `SpanKind`、attributes、links/events 相关 option；noop 实现保持零副作用。
   - 原因：RPC/Event 接入前必须具备 OTel 原生语义表达能力，避免用 attribute 临时模拟 SpanKind。
   - 依赖：无。
   - 风险：中 — 需同步所有现有 fake/noop tracer 调用点。

1. **新增 OTel adapter 包**  
   文件：`engine/pkg/tracing/oteltracer/tracer.go`
   - 操作：实现扩展后的 `tracing.ITracer` 和 `tracing.ISpan`，将 `SpanKind` / attributes / links 映射到 OTel API。
   - 原因：保留框架 facade，同时保证 OTel 语义无损。
   - 依赖：步骤 0。
   - 风险：低。

2. **新增 adapter 配置与构造器**  
   文件：`engine/pkg/tracing/oteltracer/config.go`
   - 操作：定义 `Config`、默认值、校验逻辑。
   - 原因：便于 Node 按配置初始化。
   - 依赖：步骤 1。
   - 风险：低。

3. **新增 exporter 初始化**  
   文件：`engine/pkg/tracing/oteltracer/exporter.go`
   - 操作：支持 `noop`、`stdout`、`otlp`。
   - 原因：本地调试可用 stdout，生产接 OTLP Collector。
   - 依赖：步骤 2。
   - 风险：中 — OTLP gRPC/TLS 配置需要谨慎处理。

4. **补充 adapter 单元测试**  
   文件：`engine/pkg/tracing/oteltracer/tracer_test.go`
   - 操作：测试 Start/End、SetAttribute、RecordError、Shutdown 幂等。
   - 原因：adapter 是后续链路埋点基础。
   - 依赖：步骤 1~3。
   - 风险：低。

### 阶段 2：配置与 Node 生命周期

5. **扩展配置结构**  
   文件：`engine/pkg/config/define.go`
   - 操作：新增 `ObservabilityConf` / `TracingConf`，并挂载到 `NodeConf.ObservabilityConf`。
   - 原因：支持按环境启用和切换 exporter。
   - 依赖：阶段 1。
   - 风险：中 — 需保持旧配置兼容，默认禁用。

6. **Node 初始化 OTel tracer**  
   文件：`engine/pkg/node/node.go`
   - 操作：在 `Node.Start` 中根据配置创建 adapter，并通过 `stopCleanups` 注册 shutdown cleanup。
   - 原因：TracerProvider 生命周期必须由 Node 管理。
   - 依赖：步骤 5。
   - 风险：中 — Stop 顺序需确保 exporter flush。

7. **补充配置示例**  
   文件：`template/config/`、`example/configs/`
   - 操作：增加 disabled/stdout/otlp 示例。
   - 原因：降低接入成本。
   - 依赖：步骤 5。
   - 风险：低。

### 阶段 3：RPC 链路埋点

8. **RPC trace context 注入/提取**  
   文件：`engine/pkg/utils/emberctx/`、`engine/pkg/utils/xcontext/`、`engine/pkg/rpc/message/msgbus/bus.go`、`engine/pkg/core/rpc/handler.go`、`engine/pkg/rpc/remote/handler/handler.go`
   - 操作：基于现有 `ContextHeaders` 支持 `traceparent` / `tracestate` 传播；client 发送前注入到 ctx headers，server 处理前从已恢复 headers 提取。
   - 原因：没有 context propagation 时，client/server span 无法形成同一 trace。
   - 依赖：阶段 1。
   - 风险：中 — 需要确认 `emberctx.ToHeaders(ctx)`、`remote/handler.RpcMessageHandler` 与不同 transport 的兼容性。

9. **RPC client span**  
   文件：`engine/pkg/rpc/message/msgbus/bus.go`
   - 操作：在 `call`、`asyncCall`、`send` 内部热路径创建 span，优先覆盖真实公共入口复用后的逻辑。
   - 推荐 operation：`rpc.call`、`rpc.async_call.enqueue`、`rpc.send`。
   - 推荐属性：`rpc.system`、`rpc.method`、`rpc.sender`、`rpc.receiver`、`node.uid`。
   - 原因：RPC 是跨节点排障核心入口。
   - 依赖：步骤 8。
   - 风险：中 — 需避免改变原有错误返回和 envelope 生命周期。

   > `AsyncCall` 第一阶段建议只覆盖“请求创建并投递”阶段，即 `rpc.async_call.enqueue`。完整异步生命周期 span 需要跨 `RpcMonitor`、response、callback、timeout/cancel 结束 span，改动更大，建议作为后续增强。

10. **RPC server span**  
   文件：`engine/pkg/core/rpc/handler.go`
   - 操作：在 `HandleRequest` 方法分发前基于提取后的 context 创建 server span，结束时记录 error/status。
   - 推荐 operation：`rpc.handle`。
   - 原因：定位服务端处理耗时和授权/分发失败。
   - 依赖：步骤 8。
   - 风险：中 — recover、doResponse、授权失败路径都要覆盖。

11. **RPC 埋点测试**  
    文件：`engine/pkg/rpc/message/msgbus/*test.go`、`engine/pkg/core/rpc/*test.go`
   - 操作：用 fake tracer / fake propagator 断言 span 创建、属性、错误记录和 `traceparent` 传递。
    - 原因：避免引入真实 OTel exporter 到单测。
   - 依赖：步骤 8~10。
    - 风险：低。

### 阶段 4：Event 链路埋点

12. **Event producer span 与 context 注入**  
   文件：`engine/pkg/event/bus_global.go`、`engine/pkg/event/bus_server.go`、`engine/pkg/event/bus_specific.go`
   - 操作：在 `PublishGlobal`、`PublishServer`、`PublishSpecific` / 内部 publish 函数中创建 producer span，并向事件 headers 注入 `traceparent` / `tracestate`。
   - 推荐属性：`messaging.system`、`messaging.operation`、`event.type`、`event.scope`、`event.target_node`。
   - 原因：补齐异步事件发布侧可见性。
   - 依赖：阶段 1、Trace Context 传播设计。
   - 风险：中 — event publish 热路径要控制开销。

13. **Event consumer span 与 context 提取**  
   文件：`engine/pkg/event/processor.go`
   - 操作：在 `EventHandler` 从事件 headers 提取 context，在 `safeExec` 或 handler 执行前后创建 consumer span，记录 panic/error。
   - 原因：定位事件消费耗时和异常。
   - 依赖：步骤 12。
   - 风险：中 — 不应改变 handler 执行顺序和错误隔离语义。

   > Event publish → consume 属于异步边界。第一阶段建议沿用 parent/child 关系，保持链路连续；如果后续出现 fan-out、批处理或延迟消费导致父子关系不准确，再引入 `SpanLink` 表达因果关系。

### 阶段 5：Metrics 成熟化与 RPC duration 增强

14. **评估并切换成熟 Metrics 方案**  
   文件：`engine/pkg/metrics/`、`engine/pkg/sysService/healthservice/healthservice.go`
   - 操作：评估将当前自研 Prometheus text 输出切换为 `client_golang` Registry/Collector；如切换成本可控，优先采用成熟方案。
   - 原因：减少自研 exposition/histogram 维护成本，复用成熟库的 Counter/Gauge/Histogram/Summary 和注册机制。
   - 依赖：无，可与 OTel tracing 并行。
   - 风险：中 — 需保持 `/metrics` 输出兼容 Prometheus 抓取，避免破坏现有 healthservice 暴露路径。

   推荐迁移路径：

   | 路径 | 做法 | 适用 |
   |------|------|------|
   | A. 源头替换 | 将 RPC/Pool/Mailbox 等指标源直接改为 `CounterVec`、`GaugeVec`、`HistogramVec` | 新指标和改动成本可控的指标 |
   | B. ConstMetric 包装 | 保留现有 atomic/snapshot 源，用自定义 Collector + `prometheus.NewConstMetric` 暴露 | 迁移期保留现有 snapshot 聚合逻辑 |

   建议：新指标直接使用路径 A；已有 RuntimeSnapshot/atomic 指标短期用路径 B 迁移，验证稳定后再逐步删除自研 text renderer。

15. **扩展 RpcMetrics duration bucket**  
    文件：`engine/pkg/rpc/message/msgbus/rpc_metrics.go`
   - 操作：如继续使用自研 metrics，则增加简化直方图 bucket 计数；如切换 `client_golang`，则改为标准 `HistogramVec`。
    - 原因：Prometheus 侧需要延迟分布，而不仅是 total/errors。
   - 依赖：步骤 14 的选型结果。
    - 风险：中 — 需控制热路径 atomic 开销。

16. **扩展 Prometheus samples / Collector 输出**  
    文件：`engine/pkg/metrics/rpc_metrics.go`
   - 操作：输出 `ember_rpc_call_duration_seconds_bucket` 等指标；如果切换 `client_golang`，则由标准 collector 暴露。
    - 原因：补齐 P2 文档中已规划但当前未完全落地的 duration 指标。
   - 依赖：步骤 14~15。
    - 风险：中 — Prometheus histogram 格式需稳定。

> 阶段 5 不阻塞 OTel tracing 最小闭环。Metrics 可以独立切换到成熟库；如切换为 `client_golang`，现有自研 Prometheus text 输出可逐步删除，不要求长期兼容双实现。

---

## 六、推荐 Span 规范

### 6.1 RPC client span

| 字段 | 示例 | 说明 |
|------|------|------|
| operation | `rpc.call` | 同步 RPC |
| `rpc.system` | `emberengine` | 固定值 |
| SpanKind | `client` | 如接口暂不支持 SpanKind，可临时用 `rpc.kind=client` 属性表达 |
| `rpc.method` | `UserService.GetUser` | 方法名 |
| `rpc.sender` | `NodeA/UserService` | 调用方，尽量低基数 |
| `rpc.receiver` | `NodeB/UserService` | 接收方，尽量低基数 |
| `rpc.request_id` | 不默认记录 | 高基数字段，仅 debug 采样或本地排障时临时开启 |

### 6.2 RPC server span

| 字段 | 示例 | 说明 |
|------|------|------|
| operation | `rpc.handle` | 服务端处理 |
| SpanKind | `server` | 如接口暂不支持 SpanKind，可临时用 `rpc.kind=server` 属性表达 |
| `rpc.method` | `UserService.GetUser` | 方法名 |
| `rpc.authz.result` | `allow` / `deny` | 授权结果 |
| `error.code` | `authz.denied` | 如可从 errorx 提取 |

### 6.3 Event span

| 字段 | 示例 | 说明 |
|------|------|------|
| operation | `event.publish` / `event.consume` | 事件发布/消费 |
| SpanKind | `producer` / `consumer` | 如接口暂不支持 SpanKind，可临时用属性表达 |
| `messaging.system` | `emberengine` / `nats` | 消息系统 |
| `messaging.operation` | `publish` / `process` | 消息操作 |
| `event.type` | `PlayerLogin` | 事件类型 |
| `event.scope` | `global` / `server` / `specific` | 投递范围 |
| `event.target_node` | `node-1` | specific/server 场景可选 |

### 6.4 Trace Context headers

| 字段 | 说明 |
|------|------|
| `traceparent` | W3C Trace Context 主传播字段 |
| `tracestate` | W3C Trace Context vendor 状态字段 |
| `baggage` | 可选，默认不建议携带业务敏感信息 |
| `ember.traceId` | 现有框架 TraceID，继续保留并作为兼容 attribute |

### 6.5 禁止记录内容

以下内容不得默认写入 span attribute：

- request/response payload。
- token、密码、证书私钥、连接串。
- 用户隐私字段。
- 高基数动态值，如完整 actor path、随机 request id、大量业务 id。

### 6.6 Attribute 基数规则

| 类型 | 默认策略 | 示例 |
|------|----------|------|
| 低基数稳定字段 | 允许 | `rpc.system`、`rpc.method`、`event.type`、`event.scope`、`node.role` |
| 中等基数字段 | 谨慎，需白名单 | `service.name`、`node.uid`、`target.node` |
| 高基数字段 | 默认禁止 | request id、完整 actor instance path、用户 ID、订单 ID、payload hash |
| 敏感字段 | 永远禁止 | token、password、secret、证书私钥、连接串 |

如确需记录高基数字段，应只在本地 debug 或显式采样场景开启，并避免作为 metrics label。

---

## 七、配置示例

### 7.1 默认禁用

```yaml
NodeConf:
   ObservabilityConf:
      Enable: false
      Tracing:
         Enable: false
         Exporter: noop
```

### 7.2 本地 stdout 调试

```yaml
NodeConf:
   ObservabilityConf:
      Enable: true
      Tracing:
         Enable: true
         ServiceName: emberengine-local
         Exporter: stdout
         SampleRatio: 1.0
```

### 7.3 OTLP Collector

```yaml
NodeConf:
   ObservabilityConf:
      Enable: true
      Tracing:
         Enable: true
         ServiceName: emberengine-node
         Exporter: otlp
         Endpoint: 127.0.0.1:4317
         Insecure: true
         SampleRatio: 0.01
         Timeout: 5s
```

---

## 八、测试策略

### 8.1 单元测试

| 测试对象 | 文件 | 重点 |
|----------|------|------|
| OTel adapter | `engine/pkg/tracing/oteltracer/tracer_test.go` | Start/End、Attribute、RecordError、Shutdown |
| 配置默认值 | `engine/pkg/config/*test.go` | 默认关闭、采样率边界、非法 exporter |
| Trace context propagation | `engine/pkg/utils/xcontext/*test.go`、`engine/pkg/rpc/message/msgenvelope/*test.go` | `traceparent` 注入/提取、兼容 `ember.traceId` |
| ContextHeaders 复用 | `engine/pkg/rpc/message/msgenvelope/*test.go`、`engine/pkg/rpc/remote/handler/*test.go`、`engine/pkg/event/*test.go` | `Message.ContextHeaders` / `Event.ContextHeaders` 可承载并恢复 `traceparent`、`tracestate` |
| RPC client span | `engine/pkg/rpc/message/msgbus/*test.go` | 成功/失败/cancel/timeout 路径、AsyncCall enqueue span |
| RPC server span | `engine/pkg/core/rpc/*test.go` | 授权失败、方法不存在、panic recover |
| Event span | `engine/pkg/event/*test.go` | publish/consume/error/panic |
| OTel disabled fallback | `engine/pkg/utils/xcontext/*test.go`、RPC/Event 相关测试 | OTel 关闭时 `ember.traceId` 仍可传播和日志关联 |
| 日志关联 | `engine/pkg/log/*test.go` 或调用日志的上层测试 | OTel 启用时输出 `otel.trace_id` / `otel.span_id`，关闭时仍输出 `ember.traceId` |
| Resource 属性 | `engine/pkg/tracing/oteltracer/*test.go` | provider resource 包含 `service.name`、`service.instance.id`、`deployment.environment` |
| Metrics 成熟化 | `engine/pkg/metrics/*test.go`、`engine/pkg/sysService/healthservice/*test.go` | `client_golang` 或自研输出的 `/metrics` 格式、duration histogram |

### 8.2 集成测试

- 启用 stdout exporter，启动单节点示例，确认 span 可输出。
- 启用 OTLP exporter，连接本地 OpenTelemetry Collector，确认 Jaeger/Tempo 可见。
- `/metrics` 在 tracing 启用/禁用时均可正常输出。
- Node Stop 后 exporter shutdown 不阻塞、不 panic。

### 8.3 Race / 性能测试

建议验证命令：

```text
go test ./engine/pkg/tracing/... -count=1
go test ./engine/pkg/rpc/message/msgbus/... ./engine/pkg/core/rpc/... ./engine/pkg/event/... -count=1
go test -race ./engine/pkg/tracing/... ./engine/pkg/rpc/message/msgbus/... ./engine/pkg/core/rpc/... ./engine/pkg/event/... -count=1
go test ./engine/pkg/... -count=1
```

性能关注：

- tracing disabled 时，热路径不应出现明显额外分配。
- tracing enabled 但低采样率时，吞吐下降应可控。
- exporter 异常不可反压 RPC/Event 主路径。

建议补充基准：

```text
BenchmarkMessageBus_Call_TracingDisabled
BenchmarkMessageBus_Call_TracingEnabledSampled
BenchmarkMessageBus_Send_TracingDisabled
BenchmarkEventBus_Publish_TracingDisabled
BenchmarkEventProcessor_Consume_TracingDisabled
```

---

## 九、风险与缓解措施

| 风险 | 级别 | 说明 | 缓解措施 |
|------|------|------|----------|
| OTel 依赖增加模块体积 | 中 | 引入 SDK/exporter 后依赖树变大 | adapter 独立包，默认禁用 |
| 热路径性能下降 | 中 | RPC/Event span 创建增加开销 | `IsEnabled()` 快速判断，低采样率，避免 payload attribute |
| exporter 阻塞或不可用 | 中 | Collector 异常可能影响 flush | 使用 batch processor，设置 timeout，错误只记录日志 |
| 属性高基数 | 高 | method/request id 等可能导致存储膨胀 | 制定 attribute 白名单，默认不记录 request id |
| Stop 阶段卡住 | 中 | exporter shutdown 阻塞退出 | context timeout + 幂等 cleanup |
| 与现有 TraceID 语义冲突 | 中 | xcontext traceId 与 OTel trace id 不完全一致 | OTel 未启用时继续使用 xcontext traceId；OTel 启用时保留 `ember.traceId` attribute，不强制替换 |
| Metrics 切换破坏现有输出 | 中 | 从自研 text 切到 `client_golang` 可能改变 HELP/TYPE/order/bucket | 用 golden test 固定关键指标名和 label，healthservice `/metrics` 路径保持不变 |
| SpanKind 缺失导致语义失真 | 高 | RPC/Event 如果只用 attribute 表示 kind，后端拓扑和分析会不准确 | 阶段 1 扩展 `ITracer` options，或直接使用 OTel API |
| 底层 transport span 与框架 span 重复 | 中 | 同时启用 `otelgrpc` / NATS instrumentation 可能出现重复 span | 第一阶段只做框架层；后续启用 transport span 时明确命名和层级 |
| 日志无法跳转 Trace | 高 | 只有 span 没有日志 trace/span id，生产排障效率低 | `log.WithContext(ctx)` 输出 `otel.trace_id`、`otel.span_id`、`ember.traceId` |

---

## 十、成功标准

- [ ] 未启用 OTel/tracing 时，框架行为与当前 noop 实现一致，且现有 `ember.traceId` 链路仍可通过日志和上下文分析。
- [ ] 启用 stdout exporter 后，RPC 调用能生成 client/server span。
- [ ] 启用 OTLP exporter 后，span 可被 OpenTelemetry Collector 接收。
- [ ] RPC/Event 不新增 wire 字段，直接复用 `Message.ContextHeaders` / `Event.ContextHeaders` 传播 `traceparent` / `tracestate`。
- [ ] `ITracer` facade 能表达 `client`、`server`、`producer`、`consumer` SpanKind，或框架层明确直接使用 OTel API。
- [ ] 启用 OTel 后，日志能输出 `otel.trace_id`、`otel.span_id`，并保留 `ember.traceId`。
- [ ] TracerProvider Resource 包含 `service.name`、`service.instance.id`、`deployment.environment` 等关键属性。
- [ ] Node Stop 时 TracerProvider 能被有超时地 shutdown。
- [ ] RPC 成功、失败、timeout、cancel、授权失败路径均能记录 span 状态。
- [ ] Event publish/consume 能生成可关联 span。
- [ ] `/metrics` 与 tracing 配置互不影响。
- [ ] 如切换到 `client_golang`，`/metrics` 仍可被 Prometheus 抓取，核心指标名和 label 保持可迁移。
- [ ] `go test ./engine/pkg/... -count=1` 通过。
- [ ] 关键路径 `go test -race` 通过。
- [ ] 文档同步更新 `docs/NEXT_GOALS.md` 的 Top 10 优先级。

---

## 十一、建议优先级调整

考虑到 mTLS、RBAC、策略分发已完成，但审计日志不是当前最急迫的排障入口，建议短期优先级调整为：

| 排名 | 任务ID | 任务名称 | 理由 |
|------|--------|----------|------|
| 1 | A-1-8 | OpenTelemetry SDK 适配器 | Metrics/Health 已完成，补齐分布式 trace 是可观测性闭环关键 |
| 2 | P2-7 | Metrics 成熟化 / RPC duration metrics | 优先评估 `client_golang`，并补齐 Prometheus 侧延迟分布 |
| 3 | B-1-6 | 审计日志 | 安全追责能力仍重要，但可在可观测性完善后推进 |
| 4 | B-1-7 | 开发证书工具/证书轮转辅助 | 降低 mTLS 运维接入成本 |
| 5 | C-2-4 | 标准化 benchmark suite | 为性能优化和回归建立基线 |

---

## 十二、与现有文档关系

| 文档 | 关系 |
|------|------|
| `docs/P2_OBSERVABILITY_DEV_PLAN.md` | 本文档承接 P2 已完成后的 OTel/Tracing 演进 |
| `docs/NEXT_GOALS.md` | 本文档建议将 A-1-8 提升为短期第一优先级 |
| `docs/ROADMAP.md` | 本文档属于 Phase B/C 前的生产可观测性补强 |
| `docs/CONFIG_REFERENCE.md` | 实施后需补充 observability/tracing 配置章节 |
| `docs/QUICK_START.md` | 实施后可补充本地 stdout tracing 或 Collector 示例 |

---

*本文档为 2026-06-10 可观测性完善方案。建议先完成阶段 1~3，形成 OTel tracing 最小闭环，并确保 OTel 关闭时现有 TraceID 链路仍可分析；随后推进 Event span、Metrics 成熟化与 RPC duration metrics 增强。*