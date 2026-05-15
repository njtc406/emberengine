# EmberEngine 开发路线图

> 更新时间：2026年5月14日


## 📊 当前状态评估

| 维度 | 评分 | 说明 |
|------|------|------|
| 架构设计 | ⭐⭐⭐⭐⭐ | Actor 模型设计精良，Node 自包含完成，Mailbox RW 分离 + 洋葱中间件 |
| 代码质量 | ⭐⭐⭐⭐ | 代码规范，注释较全，panic→error 改造 100% |
| 功能完整性 | ⭐⭐⭐⭐ | 核心功能完整，actor 重整落地，配置基线回归已建立 |
| 可观测性 | ⭐⭐⭐⭐ | Prometheus text metrics 全链路（Node/RPC/Mailbox/Event/Pool）、/health+/ready+/metrics 端点、TraceID 贯通验证、tracing 接口预留 |
| 运维友好度 | ⭐⭐⭐½ | StopPolicy/DrainPolicy 已落地，全局关闭顺序已固定并有测试保护（10步逆序清理） |
| 测试覆盖 | ⭐⭐⭐⭐ | 70+ 测试文件，P0-P2 全链路覆盖（actor/core/config/event/cluster/node/pool/services/rpc/metrics/tracing），-race 门禁全绿 |
| 安全能力 | ⭐⭐⭐ | gRPC/NATS mTLS 已落地、tlsx 工具包(12 tests)、RBAC 授权引擎(24 tests)、RPC Handler 拦截集成、JWT 工具 | 缺少策略存储分发、审计日志、证书工具 |

---

## 🎯 框架愿景

统一集群中所有模块的交互方式，让游戏服务、运维服务等都可以使用相同的框架处理，使用相同的交互方式调度资源。

---

## 📋 任务清单

### P0 - 立即处理（框架稳定性）

> 目标：确保核心流程稳定可靠

| # | 任务 | 描述 | 状态 |
|---|------|------|------|
| P0-1 | 资源释放规范统一 | 明确 Envelope/Job/Pool 对象的所有权和释放职责 | ✅ 已完成 |
| P0-2 | Mailbox 流程稳定 | 确保 mailbox 的 submit/drain/stop 流程无泄漏 | ✅ 已完成 |
| P0-3 | RPC 调用链完善 | 确保 Call/AsyncCall/Send 三种调用方式正确释放资源 | ✅ 已完成 |
| P0-4 | Actor 目录重整 | Mailbox 架构优化、RW 分离、中间件链、panicRateLimiter、配置补齐 | ✅ 已完成 |
| P0-5 | 配置基线回归 | 模板和全部示例配置可通过 Config.Load 验证，自动化测试已覆盖 | ✅ 已完成 |

> 独立开发文档：[P0_STABILITY_DEV_PLAN.md](P0_STABILITY_DEV_PLAN.md)

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
| P1-1 | errorx 库完善 | 实现结构化错误码体系，支持错误链、堆栈追踪 | 无 | ✅ 已完成 |
| P1-2 | Pool Metrics 暴露 | PoolMetrics 导出 + Prometheus text 最小路径 | 无 | ✅ 已完成 |
| P1-3 | 核心模块单元测试 | core/rpc/event/services 高价值路径 +23 tests | P0 完成 | ✅ 已完成 |
| P1-4 | 优雅关闭完善 | Node/Pool/Service 全局停止顺序 + 幂等测试 +12 tests | P0 完成 | ✅ 已完成 |

> P1-1 errorx 设计要点：统一错误码格式（模块+类型+序号），支持 Wrap/Unwrap 链、堆栈追踪、结构化字段，兼容 errors.Is/As。
>
> P1-2 Pool Metrics 实现要点：封装 prometheus.GaugeVec/CounterVec，暂露 pool_current_size、pool_hit_total、pool_miss_total 三组指标。
>
> 独立开发文档：[P1_FOUNDATION_DEV_PLAN.md](P1_FOUNDATION_DEV_PLAN.md)

---

### P2 - 中期任务（可观测性 MVP）✅ 已完成

> 目标：建立完整的可观测性体系
> 独立开发文档：[P2_OBSERVABILITY_DEV_PLAN.md](P2_OBSERVABILITY_DEV_PLAN.md)

| # | 任务 | 描述 | 依赖 | 状态 |
|---|------|------|------|------|
| P2-1 | Prometheus Metrics 基础层 | 统一 sample model、MetricDesc、Prometheus text 输出 | P1-2 | ✅ 已完成 |
| P2-2 | Health/Ready/Metrics 端点 | HealthService 提供 /health、/ready、/metrics HTTP 端点 | P2-1 | ✅ 已完成 |
| P2-3 | RPC 指标埋点 | MessageBus Call/AsyncCall/Send total/errors/in-flight/duration | P2-1 | ✅ 已完成 |
| P2-4 | Mailbox/Event 指标埋点 | 队列投递/拒绝/丢弃、事件发布/投递/限流 atomic counter | P2-1 | ✅ 已完成 |
| P2-5 | TraceID 贯通验证 | traceId 全链路不丢失验证 + tracing.ITracer/ISpan 接口预留 | 无 | ✅ 已完成 |
| P2-6 | 文档与门禁回填 | ROADMAP/NEXT_GOALS 回填、剩余风险记录 | P2-1~5 | ✅ 已完成 |

> P2 核心 Metrics 清单：RPC（requests_total/duration/in_flight/errors）、Mailbox（post_total/suspended/rejected/dispatch_failed）、Event（published/delivered/throttled/batched）、Node（uptime/services/goroutines）、Pool（current_size/hit/miss × N pools）。
>
> P2 TraceID 验证结论：xcontext → Envelope ContextHeaders → 远程/本地/Event 全链路不丢失。OTel SDK 接入留 P3。

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

> P3-2 热更新方案思考：无状态服务→滚动更新，有状态服务→AB 模式+状态迁移，利用现有 etcd 服务发现 + StopGraceTimeout + DrainPolicy 实现平滑过渡。

---

## 🔄 当前推进计划（2026Q2-Q3）

> 基于 2026-05 Actor 重整后的系统分析，按"稳定性证据 → 可观测性底座 → 分布式韧性 → 文档产品化 → 安全生产化"顺序推进。

### Phase 0：建立基线与验收门禁 ✅ 已完成

- [x] 盘点验证命令：`go test ./...`、`go vet ./...`、`go test -race -count=2 ./engine/pkg/actor/...`
- [x] 配置基线：模板和全部示例配置通过 `Config.Load` 回归测试
- [x] Actor 契约清单：PostJob 所有权、BeginStop/Wait、StopPolicy、RW mode、Envelope/Job/Pool 释放边界

### Phase 1：P0 稳定性闭环（约 1-2 周）✅ 已完成

| 子任务 | 描述 | 关键文件 | 状态 |
|--------|------|----------|------|
| 资源所有权审计 | 沿 IEnvelope→Job→Pool→Sender 全路径梳理最后持有者 | `interfaces/IEnvelope.go`, `IRpcClient.go`, `monitor/call_state.go` | ✅ |
| Mailbox 生命周期压测 | BeginStop/Wait、DrainPolicy、Suspend/Resume、RW in-flight read | `actor/mailbox/worker_pool.go` | ✅ |
| Node/Service 优雅关闭 | P0 已完成 Mailbox/Service 局部关闭验证；全局关闭顺序进入 P1-4 | `node/node.go`, `core/service.go` | ✅ |
| 配置校验深化 | StopPolicy/MailboxConf/EventBus 默认值/非法值测试 | `config/define.go`, `config/config_test.go` | ✅ |

### Phase 1.5：P1 基础能力拆分 ✅ 已完成

> 独立开发文档：[P1_FOUNDATION_DEV_PLAN.md](P1_FOUNDATION_DEV_PLAN.md)

| 子任务 | 描述 | 关键文件 | 状态 |
|--------|------|----------|------|
| errorx 契约收敛 | 固定错误码、错误链、结构化字段和 `errorlib` 兼容策略 | `utils/errorx`, `utils/errorlib`, `def/error.go` | ✅ |
| Pool Metrics MVP | PoolMetrics 导出 + Prometheus text 最小路径 + adapter | `rpc/client/pool`, `metrics/`, `node/diagnostics.go` | ✅ |
| 核心测试扩展 | core/rpc/event/services/node +23 tests，-race 全绿 | `core/`, `node/`, `rpc/`, `event/`, `services/` | ✅ |
| 优雅关闭完善 | 10步逆序清理已固定 + Node/Pool Stop 幂等 +12 tests | `node/node.go`, `core/service.go`, `rpc/client/pool/` | ✅ |

### Phase 2：可观测性 MVP（约 2-3 周）✅ 已完成

> 独立开发文档：[P2_OBSERVABILITY_DEV_PLAN.md](P2_OBSERVABILITY_DEV_PLAN.md)

| 子任务 | 描述 | 关键文件 | 状态 |
|--------|------|----------|------|
| Metrics 基础层 + 导出 | 统一 sample model、Node/RPC/Mailbox/Event/Pool → Prometheus text | `metrics/`, `node/diagnostics.go` | ✅ |
| /health + /ready + /metrics | HealthService sysService，幂等关闭、并发安全 | `sysService/healthservice/` | ✅ |
| RPC 指标埋点 | Call/AsyncCall/Send total/errors/in-flight/duration | `rpc/message/msgbus/bus.go` | ✅ |
| Mailbox/Event 指标埋点 | atomic counter + snapshot pull | `actor/mailbox/`, `event/` | ✅ |
| TraceID 贯通验证 | ContextHeaders 全链路不丢失 + tracing 接口预留 | `utils/xcontext/`, `tracing/` | ✅ |
| 文档与门禁回填 | ROADMAP/NEXT_GOALS 回填 P2 状态 | `docs/` | ✅ |

### Phase 3：RPC/Cluster 韧性增强 ✅ 已完成

> 独立开发文档：[P3_RESILIENCE_DEV_PLAN.md](P3_RESILIENCE_DEV_PLAN.md)
> 完成时间：2026-05-14

| 子任务 | 描述 | 状态 |
|--------|------|------|
| RPC 调用链回归 | Call/AsyncCall/Send 正常+超时+错误路径（13 tests） | ✅ |
| Remote Handler 回归 | 去重/回复匹配/错误解析（8 tests） | ✅ |
| Router 单元测试 | nil 安全验证（7 tests） | ✅ |
| errorx wire error 收敛 | wire round-trip 验证完成，sentinel 迁移为可选增强 | ✅ |
| Graceful shutdown 集成 | RpcMonitor AddAfterStop/StopIdempotent/ClearPending（3 tests） | ✅ |
| 全量构建+文档回填 | build/vet/test 全绿，P3 包 race clean | ✅ |

### Phase 4：示例、模板、文档产品化 ✅ 已完成

> 独立开发文档：[P4_DOCS_DEV_PLAN.md](P4_DOCS_DEV_PLAN.md)
> 完成时间：2026-05-14

| 子任务 | 描述 | 状态 |
|--------|------|------|
| 配置说明表 | CONFIG_REFERENCE.md — 22 节完整配置参数参考 | ✅ |
| 快速开始指南 | QUICK_START.md — 从零到运行第一个 Service | ✅ |
| Service 开发指南 | SERVICE_DEV_GUIDE.md — RPC/事件/定时器/Module/RW 分离 | ✅ |
| 压测场景固化 | node_concurrency/README.md — 环境变量/指标/pprof 说明 | ✅ |
| 集群场景固化 | example/ReadMe.md — 8 个示例总览 + 4 个场景说明 | ✅ |

### Phase 5：生产化能力（Phase 1-3 完成后启动）

> 独立开发文档：[P5_SECURITY_DEV_PLAN.md](P5_SECURITY_DEV_PLAN.md) / [P5_POLICY_DISTRIBUTION_DEV_PLAN.md](P5_POLICY_DISTRIBUTION_DEV_PLAN.md)

| 子任务 | 描述 | 状态 |
|--------|------|------|
| mTLS 安全底座 | gRPC server/client mTLS + NATS client TLS + tlsx 工具包 | ✅ |
| RBAC 授权引擎 | Principal 身份模型 + 内存角色/权限策略 + RPC Handler 拦截 | ✅ |
| 策略存储与分发 | 本地策略文件 + etcd 策略快照/watch + Authorizer 原子热更新 | 📋 已规划 |
| 审计日志 | 高权限操作记录、审计事件广播 | ⏳ |
| 灰度与运维 | Endpoint metadata、灰度路由、健康权重、ready 接入 router | ⏳ |
| 插件系统 | 等生命周期和扩展点稳定后再实装 PluginManager | ⏳ |

---

## ✅ 已完成任务归档

| # | 任务 | 完成时间 | 说明 |
|---|------|----------|------|
| - | RpcJob.Release 泄漏修复 | 2026-02-03 | 添加 payload.Release() |
| - | sender_local 回复泄漏修复 | 2026-02-03 | 本地回复场景添加 envelope.Release() |
| - | Node 自包含改造 (Phase 1-4) | 2026-03 | 全局变量清零，INodeContext 窄接口，单进程多 Node 隔离 |
| - | panic/fatal → error 透传 | 2026-03 | 运行时代码仅保留白名单 panic |
| - | Phase 3 修复清单 (P3-01~P3-17) | 2026-03 | 17 项全部完成 |
| - | 架构审查修复 (NEW-01~NEW-14) | 2026-03 | profilerAdapter、eventBus 拆分、配置化参数、泛型 GetModule 等 |
| - | Actor 目录重整（2026Q2 审计） | 2026-05-12 | Mailbox RW 分离、洋葱中间件、panicRateLimiter、死代码清理、CPU spin 修复、race 修复 |
| - | 配置基线回归测试 | 2026-05-12 | 全部 template/config + example/configs 通过 Config.Load 自动化验证 |
| - | VirtualWorkerRate 废弃标记 | 2026-05-12 | jump consistent hash 取代一致性哈希虚拟节点 |
| - | P1-1 errorx 结构化错误体系 | 2026-05-12 | 错误码分段、错误链、Is/As/Unwrap、errorlib 兼容策略 |
| - | P1-2 Pool Metrics MVP | 2026-05-13 | PoolMetrics 导出 + Prometheus text adapter + 17 tests |
| - | P1-3 核心模块单元测试 | 2026-05-13 | core/rpc/event/services +23 tests，-race 全绿 |
| - | P1-4 优雅关闭完善 | 2026-05-13 | Node/Pool Stop 幂等/goroutine退出/全局停止顺序 +12 tests |
| - | P2-1 Prometheus Metrics 基础层 | 2026-05-13 | 统一 sample model、MetricDesc、Prometheus text 输出 |
| - | P2-2 Health/Ready/Metrics 端点 | 2026-05-13 | HealthService sysService，/health+/ready+/metrics HTTP 端点 |
| - | P2-3 RPC 指标埋点 | 2026-05-13 | MessageBus Call/AsyncCall/Send total/errors/in-flight/duration |
| - | P2-4 Mailbox/Event 指标埋点 | 2026-05-13 | Mailbox post/suspended/rejected/dispatch_failed + Event published/delivered/throttled/batched |
| - | P2-5 TraceID 贯通验证 | 2026-05-13 | xcontext→Envelope→远程/本地/Event 全链路验证 + tracing.ITracer/ISpan 接口预留 |
| - | P2-6 文档与门禁回填 | 2026-05-13 | ROADMAP/NEXT_GOALS 状态回填、P3 剩余风险记录 |
| - | P3-1 RPC 调用链回归测试 | 2026-05-14 | Call/AsyncCall/Send 13 tests，race clean |
| - | P3-2 Remote Handler 回归测试 | 2026-05-14 | 去重/回复匹配/错误解析 8 tests，race clean |
| - | P3-3 Router 单元测试 | 2026-05-14 | nil 安全 7 tests |
| - | P3-4 errorx wire error 收敛 | 2026-05-14 | 验证确认已有测试全面覆盖，sentinel 迁移为可选增强 |
| - | P3-5 Graceful shutdown 集成验证 | 2026-05-14 | RpcMonitor shutdown 3 tests，race clean |
| - | P3-6 全量构建+文档回填 | 2026-05-14 | build/vet/test 全绿，P3 包 race clean |
| - | P4 配置说明表 | 2026-05-14 | CONFIG_REFERENCE.md — 22 节完整配置参数参考 |
| - | P4 快速开始指南 | 2026-05-14 | QUICK_START.md — 从零到运行第一个 Service |
| - | P4 Service 开发指南 | 2026-05-14 | SERVICE_DEV_GUIDE.md — RPC/事件/定时器/Module/RW 分离 |
| - | P4 压测场景固化 | 2026-05-14 | node_concurrency/README.md — 环境变量/指标/pprof |
| - | P4 集群场景固化 | 2026-05-14 | example/ReadMe.md — 8 个示例总览 + 4 个启动场景 |
| - | P5 mTLS 安全底座 | 2026-05-14 | tlsx 工具包(12 tests) + gRPC server/client mTLS + NATS client TLS |
| - | P5 RBAC 授权引擎 | 2026-05-14 | authz 包(24 tests) + Principal/Role/Permission + RPC Handler 拦截集成 |

---

## 📝 备注

### 设计合理的现有模块
- ✅ Actor Mailbox（双队列/优先级队列、自适应空闲控制、RW 读写分离、洋葱中间件、限流/熔断/统计）
- ✅ 对象池（SyncPool/PerPPool、泄漏检测统计）
- ✅ RPC 通信（多协议、本地远程透明）
- ✅ 事件系统（三级事件、限流、NATS 集成）
- ✅ 工具库（TimingWheel、ShardedLock、CircuitBreaker、Dedup）

### 文档待补充清单
- [x] 配置项说明文档（CONFIG_REFERENCE.md — 22 节完整配置参数参考）
- [x] 快速开始指南（QUICK_START.md）
- [x] Service 开发指南（SERVICE_DEV_GUIDE.md）
- [x] 示例说明（example/ReadMe.md + node_concurrency/README.md）
- [ ] 架构设计图
- [ ] 服务开发指南
- [ ] API 参考文档
- [ ] 部署运维手册
- [ ] 性能调优指南

### 当前推进决策
- 优先级：P0 ✅ → P1 ✅ → P2 ✅ → Phase 3 ✅ → Phase 4 ✅ → Phase 5 mTLS/RBAC ✅ → 下一步 P5 策略存储与分发
- Actor 后续不做大重构；重点是跨模块契约和验证闭环
- Metrics 首版复用已有 snapshot/metrics 数据源，Prometheus text 输出已建立，OTel 留 P3
- IService 瘦身、PluginManager 实装、安全认证属于后续生产化阶段
- 示例和模板要成为回归资产，不只是手动演示配置

---

*保持稳扎稳打，每个阶段确保质量后再进入下一阶段* 🚀
