# EmberEngine 下一步目标规划

> **编制时间**: 2026-03-13  
> **最后更新**: 2026-05-13  
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
| **配置体系** | ⭐⭐⭐⭐ | 结构化配置树完整、硬编码已配置化，binding tags + validator 校验已就位 | 配置校验边界值待深化、缺少热加载 |
| **可观测性** | ⭐⭐⭐⭐ | Prometheus text 全链路指标（Node/RPC/Mailbox/Event/Pool）、/health+/ready+/metrics 端点、TraceID 贯通验证 | OTel SDK 接入待 P3 |
| 测试覆盖 | ⭐⭐⭐⭐½ | 70+ 测试文件，P0-P3 全链路覆盖（RPC/Handler/Router/Monitor），race 门禁全绿 | sysModule/sysService/cluster 等模块覆盖率仍可提升 |
| **安全能力** | ⭐⭐⭐ | gRPC/NATS mTLS 已落地、tlsx 工具包(12 tests)、RBAC 授权引擎(24 tests)、RPC Handler 拦截集成、JWT 工具存在 | 缺少审计日志、etcd 策略存储、证书生成工具 |
| **文档体系** | ⭐⭐⭐½ | 设计文档详尽、QUICK_START + SERVICE_DEV_GUIDE + CONFIG_REFERENCE 已完成 | 缺少架构图、API 参考、部署运维指南 |

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
| Actor 目录重整（2026Q2 审计） | ✅ | Mailbox RW 分离、洋葱中间件、panicRateLimiter、死代码清理、CPU spin 修复、race 修复 |
| 配置基线回归测试 | ✅ | 全部 template/config + example/configs 通过 Config.Load 自动化验证；补齐 NodeType、StopPolicy、OutputFormat、RW Mode、MiddlewareConf |
| P1 基础能力建设 | ✅ | errorx 契约收敛 + PoolMetrics MVP + 核心测试 +35 tests + 优雅关闭顺序固定 |

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

#### A-1 可观测性基础建设 ✅ 已完成

**背景**: 当前框架几乎“裸跑”，生产出问题后排查极其困难。可观测性是从“能用”到“能运维”的关键跨越。

**完成概要**:
1. ✅ Prometheus metrics 基础层：统一 sample model + MetricDesc + text 输出
2. ✅ /health、/ready、/metrics HTTP 端点（HealthService）
3. ✅ RPC Call/AsyncCall/Send 指标埋点（total/errors/in-flight/duration）
4. ✅ Mailbox/Event atomic counter 指标埋点
5. ✅ TraceID 全链路贯通验证 + tracing.ITracer/ISpan 接口预留
6. ✅ OTel SDK 接入边界已规划，留 P3 实装

> 独立开发文档：[P2_OBSERVABILITY_DEV_PLAN.md](P2_OBSERVABILITY_DEV_PLAN.md)

---

#### A-2 测试覆盖提升

**目标**: 核心链路 package-level 覆盖率 ≥ 60%

**优先覆盖模块**:

| 优先级 | 包 | 当前状态 | 目标 |
|--------|----|----------|------|
| P0 | `core/service.go` | 有基础测试 | ✅ P1-3 已补齐 Init失败回滚、Start/Stop 生命周期、并发停止 |
| P0 | `rpc/message/msgbus/` | 有 bench 测试 | ✅ P1-3 已补齐 MultiBus AsyncCall/Send 空值/错误聚合 |
| P0 | `node/node.go` | 仅有 diagnostics 测试 | ✅ P1-4 已新增 Node Stop 幂等/并发/逆序清理 7 tests |
| P1 | `cluster/endpoints/` | 有基础测试 | 补齐并发 Add/Remove、临时连接 TTL 清理 |
| P1 | `actor/mailbox/worker_pool.go` | 有 bench 测试 | 补齐扩缩容、Drain、Suspend/Resume 路径 |
| P1 | `event/` | 无测试 | ✅ P1-3 已新增 handler error/Destroy/并发/重复名/nil trigger 6 tests |
| P2 | `services/services.go` | 有基础测试 | ✅ P1-3 已补齐 StopAll 幂等/空列表/Start 空列表 3 tests |
| P2 | `rpc/remote/` | 无测试 | ✅ P3-2 已新增 Remote Handler 去重/回复匹配/错误解析 8 tests |

**额外要求**:
- 逐步扩大 `go test -race` 范围至 `core/`, `rpc/`, `cluster/`, `event/`
- 为 CI 增加 `-race` 必过门禁

---

#### A-3 剩余技术债清理

| 编号 | 任务 | 具体内容 |
|------|------|----------|
| A-3-1 | errorlib.Is 签名修正 | 确认 P3-03 是否已修复，若未修复则重命名为 `IsCode(int) bool` |
| A-3-2 | Service 状态机化 | 引入显式状态机替代 atomic int32，统一状态转换规则 |
| A-3-3 | go vet 零告警 | ✅ 已完成——`go vet ./...` 零告警（含 example 目录） |
| A-3-4 | race 检测通过 | ✅ 已完成——actor/core/rpc/event/services/node/pool 全部 `-race -count=2` 通过 |

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
| B-1-1 | mTLS 基座：gRPC channel 强制 TLS，NATS TLS 完善 | ✅ 已完成 — tlsx 工具包 + server/client TLS 集成 |
| B-1-2 | 身份提取中间件：从 PID 提取 principal | ✅ 已完成 — PrincipalFromPID(ServiceType/ServiceName/NodeUid) |
| B-1-3 | RBAC 引擎实现：角色定义、策略匹配、拒绝/允许决策 | ✅ 已完成 — authz/authz.go (24 tests) |
| B-1-4 | 策略存储与分发：etcd 存储 + watch 更新 + 本地缓存 | 📋 已规划 — 见 P5_POLICY_DISTRIBUTION_DEV_PLAN.md |
| B-1-5 | RPC Handler 拦截集成：在 handler.go 方法分发前执行授权检查 | ✅ 已完成 — core/rpc/handler.go HandleRequest |
| B-1-6 | 审计日志：高权限操作记录 | ⏳ 待实施 |
| B-1-7 | 开发工具：自签证书生成脚本 | ⏳ 待实施 |

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
| B-3-1 | 配置校验层 | 🔄 部分完成——binding tags + go-playground/validator 已就位，需深化边界值测试 |
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
| C-3-1 | **快速入门指南** | ✅ 已完成 — docs/QUICK_START.md |
| C-3-2 | **API 参考文档** | GoDoc + 补充示例和使用注意事项 |
| C-3-3 | **架构设计指南** | C4 模型图 + 数据流图 + 时序图 |
| C-3-4 | **配置完整说明** | ✅ 已完成 — docs/CONFIG_REFERENCE.md（22 节） |
| C-3-5 | **性能调优指南** | WorkerPool 参数调优、连接池配置、pprof 使用 |
| C-3-6 | **Service 开发指南** | ✅ 已完成 — docs/SERVICE_DEV_GUIDE.md |
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
| **状态** | ✅ mTLS 已完成，✅ RBAC 已完成，📋 策略存储已规划，⏳ 审计日志待实施 |

### 3.3 架构风险提示

| 风险 | 级别 | 说明 | 缓解措施 |
|------|------|------|----------|
| **无认证的生产暴露** | � 中 | 内网不等于安全，横向移动可调用任意 RPC | mTLS ✅ + RBAC ✅，剩余审计日志待实施 |
| **可观测性缺失** | 🔴 高 | 问题定位只能靠日志 grep，线上事故恢复时间长 | Phase A-1 metrics + tracing |
| **测试覆盖不足** | 🟡 中 | 核心路径改动可能引入回归 | Phase A-2 补齐测试 + race 检测 |
| **单点 etcd 依赖** | 🟡 中 | etcd 不可用则集群服务发现失效 | 本地缓存兜底 + 多 etcd 节点 |
| **文档缺失** | 🟡 中 | 新人上手成本高，推广困难 | Phase C-3 文档体系 |

---

## 四、里程碑时间线

```
2026-03 ─── 开始 ───────────────────────

Phase A: 稳固基座
├── A-1  可观测性基础 (metrics + health + tracing 骨架)
├── A-2  测试覆盖提升 (核心链路 ≥ 60%) — P1 已完成大部分
└── A-3  技术债清理 (go vet 零告警 ✅, race 全通过 ✅)

2026-05-13 ── P0+P1 完成 ───────────────

Phase B: 生产就绪
├── B-1  服务间认证授权 (mTLS + RBAC + 审计)
├── B-2  结构化错误码 (errorx) — P1-1 已完成基础，待 RPC wire error 扩展
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
| 4 | A-3-3~A-3-4 | go vet 零告警 + race 检测通过 | CI 基线质量保障（A-3-3 ✅，A-3-4 🔄） |
| 5 | A-1-4~A-1-6 | Mailbox/Pool/Event 指标埋点 | 完整可观测性闭环 |
| 6 | B-1-1~B-1-2 | mTLS 底座 + 身份提取 | ✅ 已完成 |
| 7 | B-2 | errorx 结构化错误码 | RPC 跨节点错误传播的基础 |
| 8 | A-1-8 | OpenTelemetry 接入 | 分布式调用链追踪 |
| 9 | B-1-3~B-1-5 | RBAC 引擎 + 策略分发 + RPC 拦截 | ✅ B-1-3/B-1-5 已完成，📋 B-1-4 策略存储已规划 |
| 10 | C-3-1 | 快速入门文档 | 降低上手门槛，推广框架 |

---

## 六、与现有文档的关系

| 现有文档 | 状态 | 与本文档关系 |
|----------|------|-------------|
| ARCHITECTURE_REVIEW.md | 已完成的全面分析 | 本文档的分析基础 |
| ROADMAP.md | P0-P3 规划 | 本文档继承并细化其 P1-P3 任务 |
| P2_OBSERVABILITY_DEV_PLAN.md | P2 待实施 | 可观测性 MVP 的具体实施计划 |
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

*2026-05-12 更新：Actor 目录重整和配置基线回归已完成，配置体系评分升至 ⭐⭐⭐⭐，测试覆盖升至 ⭐⭐⭐，时间线已调整。*

*2026-05-14 更新：P3 RPC/Cluster 韧性增强全部完成，测试覆盖升至 ⭐⭐⭐⭐½（新增 31 tests：RPC 调用链 13 + Handler 8 + Router 7 + Monitor shutdown 3），rpc/remote 从 0 测试提升至 8 tests。*

*2026-05-14 更新：P4 文档产品化完成，新增 CONFIG_REFERENCE.md（22 节配置参数参考）、QUICK_START.md（快速开始指南）、SERVICE_DEV_GUIDE.md（Service 开发指南）、node_concurrency/README.md（压测说明）、example/ReadMe.md（示例总览重写）。*

*2026-05-14 更新：P5 mTLS 安全底座完成。新增 tlsx 工具包（LoadServerTLS/LoadClientTLS，12 tests），gRPC server/client 支持基于配置的 mTLS，NATS client sender 集成 TLS。安全能力从 ⭐ 升至 ⭐⭐。*

*2026-05-14 更新：P5 RBAC 授权引擎完成（authz 包 24 tests + Principal 身份模型 + RPC Handler 拦截集成），安全能力升至 ⭐⭐⭐。B-1 任务进度：mTLS(B-1-1 ✅) + 身份提取(B-1-2 ✅) + RBAC 引擎(B-1-3 ✅) + RPC 拦截(B-1-5 ✅)，剩余：策略存储(B-1-4)、审计日志(B-1-6)、证书工具(B-1-7)。*

*2026-05-14 更新：P5 策略存储与分发完成拆分规划，新增 P5_POLICY_DISTRIBUTION_DEV_PLAN.md，拆为 P5-10~P5-15：PolicySnapshot、LocalPolicyStore、PolicyWatcher、EtcdPolicyStore、配置模板与全量验证。*
