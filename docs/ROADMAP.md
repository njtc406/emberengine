# EmberEngine 开发路线图

> 更新时间：2026年2月3日


## 📊 当前状态评估

| 维度 | 评分 | 说明 |
|------|------|------|
| 架构设计 | ⭐⭐⭐⭐ | Actor 模型设计合理，分层清晰 |
| 代码质量 | ⭐⭐⭐⭐ | 代码规范，注释较全 |
| 功能完整性 | ⭐⭐⭐ | 核心功能完整，周边能力待完善 |
| 可观测性 | ⭐⭐ | 基础设施待建设 |
| 运维友好度 | ⭐⭐⭐ | 有基础，需要加强 |

---

## 🎯 框架愿景

统一集群中所有模块的交互方式，让游戏服务、运维服务等都可以使用相同的框架处理，使用相同的交互方式调度资源。

---

## 📋 任务清单

### P0 - 立即处理（框架稳定性）

> 目标：确保核心流程稳定可靠

| # | 任务 | 描述 | 状态 |
|---|------|------|------|
| P0-1 | 资源释放规范统一 | 明确 Envelope/Job/Pool 对象的所有权和释放职责 | 🔄 进行中 |
| P0-2 | Mailbox 流程稳定 | 确保 mailbox 的 submit/drain/stop 流程无泄漏 | 🔄 进行中 |
| P0-3 | RPC 调用链完善 | 确保 Call/AsyncCall/Send 三种调用方式正确释放资源 | ✅ 已完成 |

**资源释放所有权约定（供参考）**：
```text
Envelope 所有权规则：
├── 本地请求：创建者 → Deliver → Job.Release() 统一释放 payload
├── 本地回复：handler 创建 respEnv → sender_local 释放
├── 远程请求：创建者 → remote_sender 释放
└── 远程回复：handler 创建 → remote_sender 释放

核心规则：
1. Job 释放时必须释放其 payload
2. 谁最后持有 envelope，谁负责释放
```

---

### P1 - 短期任务（1-2个月）

> 目标：完善错误处理和基础监控能力

| # | 任务 | 描述 | 依赖 | 状态 |
|---|------|------|------|------|
| P1-1 | errorx 库完善 | 实现结构化错误码体系，支持错误链、堆栈追踪 | 无 | 🔄 进行中 |
| P1-2 | Pool Metrics 暴露 | 将现有 pool stats 暴露为 Prometheus metrics | 无 | ⏳ 待开始 |
| P1-3 | 核心模块单元测试 | 为 mailbox/rpc/event 核心模块增加测试覆盖 | P0 完成 | ⏳ 待开始 |
| P1-4 | 优雅关闭完善 | 完善全局关闭协调，确保关闭顺序正确 | P0 完成 | ⏳ 待开始 |

**P1-1 errorx 设计建议**：
```go
// 错误码结构
type Error struct {
    Code    int         // 错误码：模块(2位) + 类型(2位) + 序号(4位)
    Message string      // 用户可见消息
    Cause   error       // 原始错误
    Stack   []uintptr   // 堆栈（可选）
    Fields  map[string]any // 附加字段
}

// 使用示例
return errorx.New(errorx.CodeRPCTimeout).
    WithMessage("调用服务超时").
    WithCause(err).
    WithField("service", "UserService")
```

**P1-2 Pool Metrics 实现要点**：
```go
// engine/pkg/utils/pool/metrics.go
var (
    poolCurrentSize = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{Name: "ember_pool_current_size"},
        []string{"pool_name"},
    )
    poolHitTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{Name: "ember_pool_hit_total"},
        []string{"pool_name"},
    )
    poolMissTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{Name: "ember_pool_miss_total"},
        []string{"pool_name"},
    )
)

func RegisterMetrics(registry prometheus.Registerer) {
    registry.MustRegister(poolCurrentSize, poolHitTotal, poolMissTotal)
}
```

---

### P2 - 中期任务（3-4个月）

> 目标：建立完整的可观测性体系

| # | 任务 | 描述 | 依赖 | 状态 |
|---|------|------|------|------|
| P2-1 | Prometheus 集成 | 完整的 metrics 暴露（RPC QPS/延迟/错误率等） | P1-2 | ⏳ 待开始 |
| P2-2 | 限流中间件集成 | 集成成熟限流库（如 uber-go/ratelimit） | 无 | ⏳ 待开始 |
| P2-3 | 熔断器集成 | 将 CircuitBreaker 集成到 RPC 调用链 | P1-1 | ⏳ 待开始 |
| P2-4 | 分布式追踪增强 | 基于现有 traceid 对接 Jaeger/OpenTelemetry | 无 | ⏳ 待开始 |
| P2-5 | 健康检查端点 | 提供 /health /ready 端点 | 无 | ⏳ 待开始 |
| P2-6 | 配置项集中化 | 将所有魔数移入配置，提供完整 example 配置 | 无 | ⏳ 待开始 |

**P2-1 核心 Metrics 清单**：
```text
RPC 指标：
├── ember_rpc_requests_total{service, method, status}     # 请求总数
├── ember_rpc_request_duration_seconds{service, method}   # 请求延迟直方图
├── ember_rpc_in_flight{service}                          # 当前进行中的请求数
└── ember_rpc_errors_total{service, method, error_code}   # 错误计数

Mailbox 指标：
├── ember_mailbox_queue_size{service, priority}           # 队列大小
├── ember_mailbox_job_duration_seconds{service, job_type} # Job 处理耗时
└── ember_mailbox_dropped_total{service, reason}          # 丢弃的消息数

Event 指标：
├── ember_event_published_total{event_type}               # 发布事件数
├── ember_event_delivered_total{event_type}               # 投递事件数
└── ember_event_throttled_total{event_type}               # 被限流事件数
```

**P2-4 Jaeger 对接要点**：
```go
// 现有 xcontext 已有 traceid，只需增加 span 创建
import "go.opentelemetry.io/otel"

func (mb *MessageBus) call(ctx context.Context, ...) error {
    tracer := otel.Tracer("ember")
    ctx, span := tracer.Start(ctx, "rpc.call",
        trace.WithAttributes(
            attribute.String("service", receiverName),
            attribute.String("method", method),
        ))
    defer span.End()
    
    // ... 原有逻辑
}
```

---

### P3 - 长期任务（6个月+）

> 目标：企业级能力和生态完善

| # | 任务 | 描述 | 依赖 | 状态 |
|---|------|------|------|------|
| P3-1 | 安全认证体系 | RPC 调用认证、TLS 完善 | P2 完成 | ⏳ 待开始 |
| P3-2 | 热更新方案 | 研究 AB 模式/状态迁移的最佳实践 | P2 完成 | ⏳ 待开始 |
| P3-3 | 运维控制台 | 可视化管理界面 | P2 完成 | ⏳ 待开始 |
| P3-4 | 完整文档体系 | API 文档、最佳实践、架构图 | 持续进行 | ⏳ 待开始 |
| P3-5 | 多语言 SDK | 其他语言客户端支持 | P3-1 | ⏳ 待开始 |

**P3-2 热更新方案思考**：
```text
方案对比：
├── AB 模式（蓝绿部署）
│   ├── 优点：简单可靠，回滚容易
│   ├── 缺点：需要双倍资源，有状态服务需要状态迁移
│   └── 适用：无状态服务、可迁移状态的服务
│
├── 滚动更新
│   ├── 优点：资源利用率高
│   ├── 缺点：版本混跑期间需要兼容性
│   └── 适用：无状态服务
│
└── 原地热更（代码热替换）
    ├── 优点：无中断
    ├── 缺点：Go 不原生支持，实现复杂
    └── 适用：特定场景（如游戏逻辑层）

建议方案：
1. 无状态服务 → 滚动更新
2. 有状态服务 → AB 模式 + 状态迁移
3. 利用现有 etcd 服务发现 + StopGraceTimeout + DrainPolicy 实现平滑过渡
```

---

## ✅ 已完成任务归档

| # | 任务 | 完成时间 | 说明 |
|---|------|----------|------|
| - | RpcJob.Release 泄漏修复 | 2026-02-03 | 添加 payload.Release() |
| - | sender_local 回复泄漏修复 | 2026-02-03 | 本地回复场景添加 envelope.Release() |

---

## 📝 备注

### 设计合理的现有模块
- ✅ Actor Mailbox（双队列/优先级队列、自适应空闲控制）
- ✅ 对象池（SyncPool/PerPPool、泄漏检测统计）
- ✅ RPC 通信（多协议、本地远程透明）
- ✅ 事件系统（三级事件、限流、NATS 集成）
- ✅ 工具库（TimingWheel、ShardedLock、CircuitBreaker、Dedup）

### 文档待补充清单
- [ ] 架构设计图
- [ ] 服务开发指南
- [ ] 配置项说明文档
- [ ] API 参考文档
- [ ] 部署运维手册
- [ ] 性能调优指南

---

*保持稳扎稳打，每个阶段确保质量后再进入下一阶段* 🚀
