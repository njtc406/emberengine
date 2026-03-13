# EmberEngine 下一步目标规划

> **编制时间**: 2026-03-13  
> **基准分支**: `v2-dev-node-fix`  
> **编制依据**: 全项目源码分析、ARCHITECTURE_REVIEW.md、ROADMAP.md、DESIGN_MULTI_NODE*.md、DESIGN_ISSUES_FIXLIST.md、TODO_SERVICE_CONTAINER.md

---

## 一、框架现状总评

### 1.1 架构成熟度打分

| 维度 | 评分 | 当前状态 | 差距与短板 |
|------|------|----------|-----------|
| **核心模型 (Actor/Service/Module)** | ⭐⭐⭐⭐⭐ | Node 自包含完成、Mailbox/WorkerPool 设计精良、RW 分离、洋葱中间件 | 无重大短板 |
| **Node 自包含改造** | ⭐⭐⭐⭐⭐ | Phase 1-4 完成，全局变量清零，错误透传改造 100% | 无重大短板 |
| **RPC 通信层** | ⭐⭐⭐⭐ | 三协议透明支持 (gRPC/NATS/rpcx)、连接池完整、MessageBus 统一 | 缺少链路追踪、缺少请求级 metrics |
| **集群/服务发现** | ⭐⭐⭐⭐ | etcd Watch + 健康检查 + 指数退避重连 + 主从选举守卫 | 缺少灰度路由、缺少节点级健康端点 |
| **事件系统** | ⭐⭐⭐⭐ | 三级事件 (Global/Server/Specific)、NATS 跨节点、限流批处理 | 已拆分完成，缺少 metrics |
| **错误处理** | ⭐⭐⭐⭐ | panic→error 全面改造、errorlib 基础能力 | errorlib.Is 签名问题（P3-03）待确认清理 |
| **配置体系** | ⭐⭐⭐ | 结构化配置树完整、硬编码已配置化 | 缺少配置校验、缺少完整配置文档 |
| **可观测性** | ⭐⭐ | 基础 Profiler + 诊断 Snapshot | **最大短板**：无 Prometheus metrics、无分布式追踪、无 health endpoint |
| **测试覆盖** | ⭐⭐ | 45 个测试文件，核心路径已覆盖 | core/node/rpc-remote 等模块覆盖率仍低 |
| **安全能力** | ⭐ | TLS 支持初步、JWT 工具存在 | **第二大短板**：无服务间认证、无授权体系、无审计 |
| **文档体系** | ⭐⭐ | 设计文档详尽 (6 份)、README 基础 | 严重缺少 API 文档、使用指南、配置说明 |

### 1.2 已完成里程碑回顾

| 里程碑 | 状态 | 核心成果 |
|--------|------|----------|
| Node 自包含改造 (Phase 1-4) | ✅ | 全局变量清零，INodeContext 窄接口，单进程多 Node 隔离 |
| panic/fatal → error 透传 | ✅ | 运行时代码仅保留白名单 panic（deque/worker_pool/log） |
| Logger 接口化 (ILoggerX) | ✅ | 全链路日志走接口注入，无包级依赖 |
| RPC 包级全局状态清理 (P3-01) | ✅ | 6 个 RPC 子包的 Set* 全部改为构造参数注入 |
| 设计漏洞修复清单 | ✅ | idle.Controller、leadership.Guard、MultiBus CallMode、CircuitBreaker |
| Phase 3 修复清单 (P3-01~P3-17) | ✅ | 17 项全部完成 |
| 架构审查修复 (NEW-01~NEW-14) | ✅ | profilerAdapter 统一、eventBus 拆分、配置化参数、泛型 GetModule 等 |

### 1.3 当前技术债残余

| 编号 | 类型 | 描述 | 优先级 |
|------|------|------|--------|
| TD-01 | 接口 | `IService` 方法数过多 (20+)，调用侧仅需 2-3 个方法 | 低 |
| TD-02 | 并发 | `Module.rootContains` 是普通 map，理论上非并发安全 | 低 |
| TD-03 | 配置 | `config/define.go` 303 行所有配置结构体集中 | 低 |
| TD-04 | 覆盖 | `sysModule/sysService/utils` 大量包 0% 测试覆盖 | 中 |
| TD-05 | 代码 | `core/service.go` 632 行，Init/Start/Stop 可考虑拆分 | 低 |

---

## 二、下一步目标（分阶段规划）

### Phase A：稳固基座（近期 1-2 月）

> **目标**: 夯实测试、补齐可观测性基础、清理剩余技术债

#### A-1 可观测性基础建设 🔥 **最高优先级**

**背景**: 当前框架几乎"裸跑"，生产出问题后排查极其困难。可观测性是从"能用"到"能运维"的关键跨越。

**目标**:
1. 集成 Prometheus metrics 暴露
2. 提供 /health 和 /ready 端点
3. 基于现有 xcontext.traceId 接入 OpenTelemetry

**Metrics 核心指标清单**:

```
RPC 指标:
├── ember_rpc_requests_total{service, method, status}       # 请求总数
├── ember_rpc_request_duration_seconds{service, method}     # 延迟直方图
├── ember_rpc_in_flight{service}                            # 当前进行中请求
└── ember_rpc_errors_total{service, method, error_code}     # 错误计数

Mailbox 指标:
├── ember_mailbox_queue_size{service, priority}             # 队列深度
├── ember_mailbox_job_duration_seconds{service, job_type}   # Job 处理耗时
├── ember_mailbox_workers_active{service}                   # 活跃 Worker 数
└── ember_mailbox_dropped_total{service, reason}            # 丢弃消息数

连接池指标:
├── ember_pool_connections_active{pool_name}                # 活跃连接
├── ember_pool_hit_total{pool_name}                         # 命中数
└── ember_pool_miss_total{pool_name}                        # 未命中数

事件指标:
├── ember_event_published_total{event_type}                 # 发布事件数
├── ember_event_delivered_total{event_type}                 # 投递事件数
└── ember_event_throttled_total{event_type}                 # 被限流事件数

Node 级指标:
├── ember_node_uptime_seconds                               # 运行时长
├── ember_node_services_total                               # 服务数量
└── ember_node_goroutines                                   # goroutine 数
```

**架构方案**:
- 新建 `engine/pkg/metrics/` 包，封装 `prometheus/client_golang`
- Node 持有 `*metrics.Registry`，通过 INodeContext 注入各子系统
- 各子系统（RPC/Mailbox/Pool/Event）通过 Collector 接口上报
- `sysService/metricsservice` 暴露 `/metrics` HTTP 端点

**Health/Ready 端点**:
- `/health` — Node 存活检查（进程 OK 即返回 200）
- `/ready` — 就绪检查（所有 Service 状态 = Running）
- 可通过 `sysService/healthservice` 实现或直接集成到 pprofservice

**实施任务拆分**:

| 序号 | 任务 | 具体文件/包 |
|------|------|------------|
| A-1-1 | 创建 metrics 包，定义 Collector 接口和 Prometheus Registry 封装 | `engine/pkg/metrics/` |
| A-1-2 | Node 持有 metrics.Registry，通过 INodeContext 注入 | `node/node.go`, `interfaces/` |
| A-1-3 | RPC 调用链埋点（MessageBus.call/asyncCall/send） | `rpc/message/msgbus/bus.go` |
| A-1-4 | Mailbox/WorkerPool 埋点（队列深度、处理耗时、丢弃数） | `actor/mailbox/worker_pool.go`, `worker.go` |
| A-1-5 | 连接池埋点（命中率、活跃连接） | `rpc/client/pool/manager_runtime.go` |
| A-1-6 | 事件系统埋点 | `event/bus_*.go` |
| A-1-7 | 创建 HealthService（/health, /ready, /metrics） | `sysService/healthservice/` |
| A-1-8 | OpenTelemetry Tracer 接入（基于 xcontext.traceId） | `rpc/message/msgbus/`, `utils/xcontext/` |

---

#### A-2 测试覆盖提升

**目标**: 核心链路 package-level 覆盖率 ≥ 60%

**优先覆盖模块**:

| 优先级 | 包 | 当前状态 | 目标 |
|--------|----|----------|------|
| P0 | `core/service.go` | 有基础测试 | 补齐 Init失败回滚、Start/Stop 生命周期、并发停止 |
| P0 | `rpc/message/msgbus/` | 有 bench 测试 | 补齐 Call/AsyncCall/Send 正常 + 超时 + 错误路径 |
| P0 | `node/node.go` | 仅有 diagnostics 测试 | 新增 smoke test（最小 Node 启动/停止） |
| P1 | `cluster/endpoints/` | 有基础测试 | 补齐并发 Add/Remove、临时连接 TTL 清理 |
| P1 | `actor/mailbox/worker_pool.go` | 有 bench 测试 | 补齐扩缩容、Drain、Suspend/Resume 路径 |
| P1 | `event/` | 无测试 | 新增 EventBus Init/Publish/Subscribe 基础覆盖 |
| P2 | `services/services.go` | 有基础测试 | 补齐 Init 锁行为、StopAll 逆序 |
| P2 | `rpc/remote/` | 无测试 | 新增 handler 请求分发基础覆盖 |

**额外要求**:
- 逐步扩大 `go test -race` 范围至 `core/`, `rpc/`, `cluster/`, `event/`
- 为 CI 增加 `-race` 必过门禁

---

#### A-3 剩余技术债清理

| 编号 | 任务 | 具体内容 |
|------|------|----------|
| A-3-1 | errorlib.Is 签名修正 | 确认 P3-03 是否已修复，若未修复则重命名为 `IsCode(int) bool` |
| A-3-2 | Service 状态机化 | 引入显式状态机替代 atomic int32，统一状态转换规则 |
| A-3-3 | go vet 零告警 | 确保 `go vet ./...` 零告警（含 example 目录） |
| A-3-4 | race 检测通过 | `go test -race ./engine/pkg/...` 全部通过 |

---

### Phase B：生产就绪（中期 2-4 月）

> **目标**: 安全认证体系落地、错误码体系完善、配置系统增强

#### B-1 服务间认证与授权 (AuthN/AuthZ) 🔥

**背景**: 当前集群内服务间通信完全无认证，任何服务可冒充任意身份调用高危 API。这是生产环境的安全红线。

**分阶段方案**:

**Phase B-1a: 节点间 mTLS（底座）**
- 节点间 gRPC/NATS 强制 mTLS
- 从证书 SAN 中提取 caller 身份（serviceName/serviceType）
- 利用现有 `NatsConf.TLS*` + gRPC DialOption 扩展
- 提供证书工具（开发环境自签、生产对接 CA）

**Phase B-1b: RBAC 授权框架**
- 服务 = 用户（Principal），基于 serviceName/serviceType
- 角色 = 权限集合，方法级粒度（`<serviceName>.<method>`）
- 策略存储在 etcd（watch 更新 + 本地缓存）
- 拦截点：RPC Handler 收到请求后、路由到 method 前

**Phase B-1c: 审计日志**
- 高权限操作（admin.*）记录审计日志
- 审计事件通过 EventBus 广播或写入独立存储

**架构方案**:

```
engine/pkg/authz/
├── principal.go            # 身份模型
├── rbac.go                 # RBAC 引擎
├── policy.go               # 策略定义与加载
├── interceptor.go          # RPC 拦截中间件
├── audit.go                # 审计日志
└── tls/
    └── mtls.go             # mTLS 配置与证书管理
```

**实施任务**:

| 序号 | 任务 | 说明 |
|------|------|------|
| B-1-1 | mTLS 基座：gRPC channel 强制 TLS，NATS TLS 完善 | 扩展 config，补齐证书配置 |
| B-1-2 | 身份提取中间件：从 TLS peer cert 提取 principal | gRPC interceptor / NATS handler |
| B-1-3 | RBAC 引擎实现：角色定义、策略匹配、拒绝/允许决策 | `authz/rbac.go` |
| B-1-4 | 策略存储与分发：etcd 存储 + watch 更新 + 本地缓存 | `authz/policy.go` |
| B-1-5 | RPC Handler 拦截集成：在 handler.go 方法分发前执行授权检查 | `rpc/remote/handler/` |
| B-1-6 | 审计日志：高权限操作记录 | `authz/audit.go` |
| B-1-7 | 开发工具：自签证书生成脚本 | `tools/cert/` |

---

#### B-2 结构化错误码体系（errorx）

**背景**: 现有 `errorlib` 存在签名问题（Is(int)），且缺少错误链、堆栈追踪、结构化字段。RPC 调用的错误信息在跨节点传播后丢失上下文。

**目标**:
- 统一业务错误码格式：模块(2位) + 类型(2位) + 序号(4位)
- 支持错误链（Wrap/Unwrap）与堆栈追踪
- RPC 传输层自动序列化/反序列化错误码
- 兼容 `errors.Is()` / `errors.As()` 标准协议

**架构方案**:

```go
// engine/pkg/utils/errorx/error.go
type Error struct {
    Code    int32          // 错误码
    Message string         // 用户可见消息
    Cause   error          // 原始错误（支持 Unwrap）
    Stack   []uintptr      // 堆栈（可选，仅 Debug 模式采集）
    Fields  map[string]any // 附加上下文字段
}

func (e *Error) Is(target error) bool { /* 按码匹配 */ }
func (e *Error) Unwrap() error        { return e.Cause }
```

**实施任务**:

| 序号 | 任务 |
|------|------|
| B-2-1 | 设计 errorx 包，实现 Error 结构体 + Is/As/Unwrap |
| B-2-2 | 定义框架内置错误码（RPC 超时、服务不可达、方法不存在等） |
| B-2-3 | RPC Envelope 支持 errorx 序列化/反序列化 |
| B-2-4 | 迁移现有 errorlib 调用侧到 errorx |

---

#### B-3 配置系统增强

| 序号 | 任务 | 说明 |
|------|------|------|
| B-3-1 | 配置校验层 | 在 Config.Load() 后执行结构化校验（必填字段、范围检查、枚举校验） |
| B-3-2 | 完整配置文档 | 生成配置项说明文档（字段名、类型、默认值、示例、约束） |
| B-3-3 | 敏感配置支持 | 密码/Token 支持环境变量引用或加密存储 |
| B-3-4 | 配置热加载 | 部分配置项支持运行时更新（如日志级别、限流阈值） |

---

### Phase C：生态完善（长期 4-8 月）

> **目标**: 运维能力、性能优化、文档生态

#### C-1 运维与部署能力

| 序号 | 任务 | 说明 |
|------|------|------|
| C-1-1 | 优雅发布支持 | 基于 etcd 服务发现 + StopGraceTimeout + DrainPolicy 实现滚动更新 |
| C-1-2 | 灰度路由 | 路由选择器支持 Version/Tag/Weight 属性，实现灰度发布 |
| C-1-3 | 控制面 CLI | 命令行工具查看集群状态、服务列表、连接池状态、手动切换主从 |
| C-1-4 | Docker/K8s 部署模板 | 完善 template/docker/，新增 K8s Deployment/Service YAML |
| C-1-5 | Systemd 集成 | 实现 `engine/pkg/systemd/` 包（当前为空占位） |

#### C-2 性能优化

| 序号 | 任务 | 说明 |
|------|------|------|
| C-2-1 | RPC 零拷贝优化 | Envelope 序列化/反序列化路径减少内存分配 |
| C-2-2 | Mailbox 批量提交 | 支持批量 PostJob，减少 channel 操作次数 |
| C-2-3 | 连接池预热 | 启动时预建立指定数量的 RPC 连接 |
| C-2-4 | 基准测试体系 | 建立标准化 benchmark suite，跟踪版本间性能回归 |

#### C-3 文档体系建设

| 序号 | 文档 | 说明 |
|------|------|------|
| C-3-1 | **快速入门指南** | 从零创建 Node + Service + Module，5 分钟跑通 |
| C-3-2 | **API 参考文档** | GoDoc + 补充示例和使用注意事项 |
| C-3-3 | **架构设计指南** | C4 模型图 + 数据流图 + 时序图 |
| C-3-4 | **配置完整说明** | 每个配置项的作用、默认值、示例 |
| C-3-5 | **性能调优指南** | WorkerPool 参数调优、连接池配置、pprof 使用 |
| C-3-6 | **Service 开发指南** | 如何编写 Service、挂载 Module、注册 RPC 方法、处理事件 |
| C-3-7 | **部署运维指南** | 单机/集群部署、主从配置、监控接入、日志管理 |

#### C-4 生态扩展

| 序号 | 任务 | 说明 |
|------|------|------|
| C-4-1 | Service 容器文档化 | 完成 TODO_SERVICE_CONTAINER.md 中的注释级梳理 |
| C-4-2 | 示例体系重组 | 按场景分类：基础/并发/集群/HTTP混合/实战业务 |
| C-4-3 | 插件机制完善 | PluginManager 扩展为完整的 hook 链 + 生命周期管理 |
| C-4-4 | 多语言 SDK（探索） | 基于 gRPC proto 定义生成其他语言客户端 |

---

## 三、架构演进方向建议

### 3.1 ADR-005: 可观测性集成方案

| 项 | 内容 |
|---|------|
| **背景** | 框架"裸跑"，生产排查困难，是当前最大短板 |
| **决策** | 采用 Prometheus + OpenTelemetry 双轨方案 |
| **Metrics** | Prometheus client_golang，Node 持有 Registry，各子系统 Collector |
| **Tracing** | OpenTelemetry SDK，复用 xcontext.traceId，按需采样 |
| **Health** | 内建 /health + /ready HTTP 端点 |
| **正面** | 业界标准方案，生态丰富，对接 Grafana/Jaeger 零成本 |
| **负面** | 增加依赖，metrics 采集有微小性能开销 |
| **替代方案** | 自研 metrics（维护成本高，生态差） |
| **状态** | 📋 待实施 |

### 3.2 ADR-006: 服务间安全认证方案

| 项 | 内容 |
|---|------|
| **背景** | 集群内无认证，任意服务可冒充身份调用高危 API |
| **决策** | mTLS（底座） + RBAC（授权） + 审计日志（追责） |
| **认证** | 节点间强制 mTLS，从证书 SAN 提取 caller 身份 |
| **授权** | RBAC 方法级粒度，策略存 etcd，本地缓存 + watch 更新 |
| **正面** | 身份不可伪造、运维可配置、审计可追溯 |
| **负面** | 证书管理增加运维复杂度、RBAC 策略需要管理平台 |
| **替代方案** | Token 签名（适合 NATS 异步场景，可作为 mTLS 的补充） |
| **状态** | 📋 待实施 |

### 3.3 架构风险提示

| 风险 | 级别 | 说明 | 缓解措施 |
|------|------|------|----------|
| **无认证的生产暴露** | 🔴 高 | 内网不等于安全，横向移动可调用任意 RPC | Phase B-1 mTLS + RBAC |
| **可观测性缺失** | 🔴 高 | 问题定位只能靠日志 grep，线上事故恢复时间长 | Phase A-1 metrics + tracing |
| **测试覆盖不足** | 🟡 中 | 核心路径改动可能引入回归 | Phase A-2 补齐测试 + race 检测 |
| **单点 etcd 依赖** | 🟡 中 | etcd 不可用则集群服务发现失效 | 本地缓存兜底 + 多 etcd 节点 |
| **文档缺失** | 🟡 中 | 新人上手成本高，推广困难 | Phase C-3 文档体系 |

---

## 四、里程碑时间线

```
2026-03 ─── 当前 ───────────────────────────────────

Phase A: 稳固基座
├── A-1  可观测性基础 (metrics + health + tracing 骨架)
├── A-2  测试覆盖提升 (核心链路 ≥ 60%)
└── A-3  技术债清理 (go vet 零告警, race 全通过)

2026-05 ─── Phase A 完成 ──────────────────────────

Phase B: 生产就绪
├── B-1  服务间认证授权 (mTLS + RBAC + 审计)
├── B-2  结构化错误码 (errorx)
└── B-3  配置系统增强 (校验 + 文档 + 热加载)

2026-07 ─── Phase B 完成 ──────────────────────────

Phase C: 生态完善
├── C-1  运维部署能力 (灰度 + CLI + K8s)
├── C-2  性能优化 (零拷贝 + 批量 + 预热)
├── C-3  文档体系 (7 份核心文档)
└── C-4  生态扩展 (插件 + 示例 + SDK探索)

2026-11 ─── Phase C 完成 ──────────────────────────
```

---

## 五、实施优先级排序（Top 10 Action Items）

| 排名 | 任务ID | 任务名称 | 理由 |
|------|--------|----------|------|
| 1 | A-1-1~A-1-3 | Metrics 包创建 + RPC 埋点 | 可观测性零到一，投入产出比最高 |
| 2 | A-1-7 | Health/Ready/Metrics HTTP 端点 | 运维基础能力，K8s 探针必需 |
| 3 | A-2 (P0) | core/service + msgbus + node smoke test | 防止核心链路回归 |
| 4 | A-3-3~A-3-4 | go vet 零告警 + race 检测通过 | CI 基线质量保障 |
| 5 | A-1-4~A-1-6 | Mailbox/Pool/Event 指标埋点 | 完整可观测性闭环 |
| 6 | B-1-1~B-1-2 | mTLS 底座 + 身份提取 | 安全基座，后续 RBAC 依赖 |
| 7 | B-2 | errorx 结构化错误码 | RPC 跨节点错误传播的基础 |
| 8 | A-1-8 | OpenTelemetry 接入 | 分布式调用链追踪 |
| 9 | B-1-3~B-1-5 | RBAC 引擎 + 策略分发 + RPC 拦截 | 安全闭环 |
| 10 | C-3-1 | 快速入门文档 | 降低上手门槛，推广框架 |

---

## 六、与现有文档的关系

| 现有文档 | 状态 | 与本文档关系 |
|----------|------|-------------|
| ARCHITECTURE_REVIEW.md | 已完成的全面分析 | 本文档的分析基础 |
| ROADMAP.md | P0-P3 规划 | 本文档继承并细化其 P1-P3 任务 |
| DESIGN_MULTI_NODE.md | Phase 1-4 已完成 | 历史里程碑，已归档 |
| DESIGN_MULTI_NODE_PHASE3_FIXLIST.md | 17 项全部完成 | 历史修复清单，已归档 |
| DESIGN_ISSUES_FIXLIST.md | 主要项已修复 | 历史修复清单，已归档 |
| DESIGN_SERVICE_AUTHZ.md | 方向性思考 | 本文档 Phase B-1 的设计输入 |
| TODO_SERVICE_CONTAINER.md | 容器视角 TODO | 本文档 Phase C-4 整合 |

**建议**:
- 已完成的修复清单（DESIGN_MULTI_NODE_PHASE3_FIXLIST.md、DESIGN_ISSUES_FIXLIST.md）移入 `docs/archive/` 归档
- 本文档（NEXT_GOALS.md）作为后续迭代的主跟踪文档
- ROADMAP.md 保持不变，作为高层愿景参考

---

*本文档基于 2026-03-13 全项目源码分析与历史设计文档综合编制。建议每次 Phase 完成后回顾更新。*
