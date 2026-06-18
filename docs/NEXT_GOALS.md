# EmberEngine 下一步目标规划

> **编制时间**: 2026-03-13<br>
> **最后更新**: 2026-06-12<br>
> **基准分支**: `v2-dev-node-fix`<br>
> **编制依据**: 全项目源码分析、GitNexus FTS 修复后复评、`ARCHITECTURE_REVIEW.md`、`ROADMAP.md`、`DESIGN_MULTI_NODE*.md`、`DESIGN_ISSUES_FIXLIST.md`、`TODO_SERVICE_CONTAINER.md`

---

## 一、当前复评结论（2026-06-12）

### 1.1 GitNexus 复评状态

| 项 | 结论 |
|---|---|
| GitNexus 仓库 | `emberengine` |
| 索引状态 | latest |
| 索引时间 | `2026/6/12 15:21:08` |
| 索引 commit | `c7a2c5f` |
| FTS 状态 | 已修复，`gitnexus query -r emberengine ...` 不再出现 FTS 缺失警告 |
| 复评方法 | FTS query + context + impact + 关键文件复核 |

### 1.2 总体判断

EmberEngine 已经不是原型项目，而是一个接近生产框架形态的 Go 分布式 Actor/RPC 服务运行时。当前优势是核心模型完整、模块分层清晰、性能路径有设计、可观测性和安全体系已有基础；主要挑战集中在核心路径复杂度、生产安全闭环、OpenTelemetry 标准化接入、证书运维与长期维护成本。

综合成熟度：**7.8 / 10**。

| 维度 | 评分 | 当前状态 | 主要短板 |
|------|------:|----------|----------|
| 核心模型（Actor/Service/Module） | 8.5 / 10 | `Node → Service → Module` 模型清晰，Service/Mailbox/WorkerPool/RPC 主链路完整 | `Service` 影响面为 CRITICAL，需保持稳定 API |
| Node 自包含改造 | 9 / 10 | Phase 1-4 完成，全局变量清理、依赖注入和单进程多 Node 隔离已完成 | `Node.Start` 编排较长，后续可阶段化拆分 |
| RPC 通信层 | 8 / 10 | gRPC/NATS/rpcx 三协议、连接池、MessageBus、Call/AsyncCall/Send、CallState 已具备 | OTel trace、请求级诊断语义、复杂故障测试仍需加强 |
| Mailbox/Actor 并发模型 | 8 / 10 | Worker、优先级、RW 分离、Suspend/Drain/Stop 策略较完整 | job release、stop drain、panic recover、backpressure 仍是 P0 稳定性重点 |
| 集群/服务发现 | 8 / 10 | etcd Watch、健康检查、指数退避重连、主从选举守卫已具备 | 灰度路由、节点级运维控制面待完善 |
| 事件系统 | 8 / 10 | Global/Server/Specific 三级事件、NATS 跨节点、限流批处理 | 事件 metrics 和生产排障语义仍可增强 |
| 错误处理 | 8.5 / 10 | panic→error 改造、errorx、RPC wire error、errorlib 移除已完成 | `def` sentinel 错误码化需按模块继续推进 |
| 配置体系 | 8 / 10 | 结构化配置、binding tags、validator、配置参考文档已具备 | 边界值测试、敏感配置、热加载待增强 |
| 可观测性 | 7 / 10 | Prometheus text、Node/RPC/Mailbox/Event/Pool metrics、/health、/ready、/metrics、TraceID 骨架已具备 | OTel SDK adapter、dashboard、alert rule、trace/log/metric 关联待完成 |
| 测试覆盖 | 7.5 / 10 | 70+ 测试文件，核心 RPC/Handler/Router/Monitor/race 门禁基础较好 | `sysModule`、`sysService`、`cluster`、故障注入覆盖仍需提升 |
| 安全能力 | 7 / 10 | gRPC/NATS mTLS、tlsx、RBAC、PolicyWatcher、RPC Handler 拦截、JWT 工具已具备 | 审计日志、证书轮转、策略灰度/回滚、授权路径一致性验证待补齐 |
| 文档体系 | 7 / 10 | 设计文档、QUICK_START、SERVICE_DEV_GUIDE、CONFIG_REFERENCE 已完成 | 架构图、API 参考、部署运维指南、性能调优指南待补齐 |

### 1.3 关键影响面与风险重排

| 优先级 | 对象 | GitNexus 复核结果 | 处理原则 |
|--------|------|-------------------|----------|
| P0 | `engine/pkg/core/service.go` / `Service` | upstream risk: **CRITICAL**；impactedCount: 17；direct: 7；affected processes: 5 | 视为稳定核心 API，任何改动必须先做 impact analysis、补测试、分阶段迁移 |
| P0 | `engine/pkg/actor/mailbox/worker.go` / `Worker.run` | upstream risk: LOW，但连接多个 Worker 执行流 | 运行时风险高，重点验证 job release、drain、panic recover、RW 分离、backpressure |
| P0 | `engine/pkg/rpc/message/msgbus/bus.go` / `MessageBus.call` | upstream risk: LOW；direct callers: `Call`、`CallWithOpt`、`callInternal`、`Handler.HandleRequest` | 同步 RPC 核心路径，需覆盖 timeout、CallState、envelope、trace、返回值赋值 |
| P1 | `engine/pkg/node/node.go` / `Node.Start` | upstream risk: LOW，但架构认知复杂度高 | 后续按启动阶段拆分，避免一次性重构 |
| P1 | `engine/pkg/authz/authz.go` / `Authorizer.ApplySnapshot` | upstream risk: LOW | 强化审计、策略版本化、回滚与授权路径一致性测试 |

---

## 二、已完成能力归档

本节只保留已经落地的能力，避免与后续计划混杂。

### 2.1 架构与运行时

| 里程碑 | 状态 | 核心成果 |
|--------|------|----------|
| Node 自包含改造（Phase 1-4） | ✅ | 全局变量清零，`INodeContext` 窄接口，单进程多 Node 隔离 |
| panic/fatal → error 透传 | ✅ | 运行时代码仅保留白名单 panic（deque/worker_pool/log） |
| Logger 接口化（`ILoggerX`） | ✅ | 全链路日志走接口注入，无包级依赖 |
| RPC 包级全局状态清理（P3-01） | ✅ | 6 个 RPC 子包的 `Set*` 全部改为构造参数注入 |
| 设计漏洞修复清单 | ✅ | idle.Controller、leadership.Guard、MultiBus CallMode、CircuitBreaker |
| Phase 3 修复清单（P3-01~P3-17） | ✅ | 17 项全部完成 |
| 架构审查修复（NEW-01~NEW-14） | ✅ | profilerAdapter 统一、eventBus 拆分、配置化参数、泛型 GetModule 等 |
| Actor 目录重整（2026Q2 审计） | ✅ | Mailbox RW 分离、洋葱中间件、panicRateLimiter、死代码清理、CPU spin 修复、race 修复 |
| TimingWheel 稳定性复核 | ✅ | 修复 Stop/send race、Stop/Flush 锁顺序、bucket.Flush 锁内 reinsert；补齐 add/cancel、overflow、SetTimeOffset、dispatch benchmark |

### 2.2 可观测性基础

| 能力 | 状态 | 说明 |
|------|------|------|
| Prometheus metrics 基础层 | ✅ | 统一 sample model、MetricDesc、text 输出 |
| HealthService HTTP 端点 | ✅ | `/health`、`/ready`、`/metrics` |
| RPC metrics | ✅ | Call/AsyncCall/Send total/errors/in-flight/duration |
| Mailbox/Event/Pool metrics | ✅ | atomic counter 与 snapshot 基础已具备 |
| RuntimeSnapshot | ✅ | Node 诊断信息与 metrics 文本测试已存在 |
| TraceID 骨架 | ✅ | TraceID 贯通验证与 `tracing.ITracer` / `ISpan` 接口预留 |
| OTel SDK 边界设计 | ✅ | 已规划，实装放入后续优先级 |

参考文档：`docs/P2_OBSERVABILITY_DEV_PLAN.md`、`docs/OTEL_OBSERVABILITY_PLAN.md`。

### 2.3 安全与权限

| 任务 | 状态 | 说明 |
|------|------|------|
| mTLS 基座 | ✅ | tlsx 工具包 + gRPC server/client TLS + NATS TLS 集成 |
| 身份提取 | ✅ | `PrincipalFromPID(ServiceType/ServiceName/NodeUid)` |
| RBAC 授权引擎 | ✅ | `authz` 包，角色、权限、策略匹配、拒绝/允许决策 |
| 策略存储与分发 | ✅ | `PolicySnapshot`、LocalStore、EtcdStore、Watcher、本地缓存与 watch 更新 |
| RPC Handler 拦截集成 | ✅ | `core/rpc/handler.go` 在方法分发前执行授权检查 |
| HTTP 安全头 | ✅ | `engine/pkg/utils/httpx/security_headers.go` |
| JWT 工具 | ✅ | 已纳入安全能力基线 |

### 2.4 错误处理与配置

| 能力 | 状态 | 说明 |
|------|------|------|
| errorx 基础能力 | ✅ | Error 结构体、错误码、错误链、结构化字段、`errors.Is/As` 兼容 |
| RPC wire error | ✅ | `actor.ErrorDetail` + `MarshalToBytes/UnmarshalFromBytes` + Envelope Err bytes |
| legacy errorlib 移除 | ✅ | `errorlib` 包已删除，`msgbus` 聚合错误统一使用 `errorx.CombineErrors` |
| 配置基线回归测试 | ✅ | template/config 与 example/configs 通过 `Config.Load` 自动化验证 |
| 配置参考文档 | ✅ | `docs/CONFIG_REFERENCE.md` 已完成 22 节配置参数参考 |
| go vet 零告警 | ✅ | `go vet ./...` 零告警（含 example 目录） |
| race 基线 | ✅ | actor/core/rpc/event/services/node/pool 已通过 `-race -count=2` |

### 2.5 文档与示例

| 文档/示例 | 状态 | 说明 |
|----------|------|------|
| `docs/QUICK_START.md` | ✅ | 快速入门指南 |
| `docs/SERVICE_DEV_GUIDE.md` | ✅ | Service 开发指南 |
| `docs/CONFIG_REFERENCE.md` | ✅ | 配置完整说明 |
| `example/ReadMe.md` | ✅ | 示例总览重写 |
| `example/node_concurrency/README.md` | ✅ | 压测说明 |

---

## 三、未完成与即将开始的任务（按先后顺序）

### 3.1 当前迭代优先级（Top 10）

| 顺序 | 任务 ID | 任务名称 | 状态 | 先决条件 | 目标产出 |
|------|---------|----------|------|----------|----------|
| 1 | B-1-6 | 审计日志 | ⏳ 即将开始 | mTLS、RBAC、PolicyWatcher 已完成 | 高权限操作、授权拒绝、策略更新、远端 RPC 调用审计事件 |
| 2 | B-1-7 | 开发证书工具/证书轮转辅助 | ⏳ 即将开始 | tlsx 与 gRPC/NATS TLS 已完成 | 开发自签证书脚本、证书校验工具、轮转操作文档 |
| 3 | A-1-8 | OpenTelemetry SDK 适配器 | ⏳ 即将开始 | TraceID 骨架、metrics/health 已完成 | OTel tracer/provider/exporter adapter，trace 与 RPC/Mailbox 关键路径关联 |
| 4 | B-2-2 | `def` sentinel 错误码化 | 🔄 部分完成 | errorx 与 wire error 已完成 | 关键 sentinel 迁移到 errorx code，保留 `errors.Is/As` 兼容 |
| 5 | B-3-1 | 配置边界值测试深化 | 🔄 部分完成 | validator 已接入 | 证书路径、端口、超时、池大小、队列长度、策略配置边界测试 |
| 6 | B-3-3 | 敏感配置环境变量引用 | ⏳ 未开始 | 配置加载与校验稳定 | 密码/Token/证书路径支持 `${ENV}` 引用，避免明文落配置 |
| 7 | C-1-1 | 优雅发布 / DrainPolicy | ⏳ 未开始 | StopGraceTimeout、Service Stop、Mailbox Drain 基础已具备 | 基于服务发现与 drain 的滚动发布流程 |
| 8 | C-2-4 | 标准化 benchmark suite | ⏳ 未开始 | 现有 node_concurrency 与 pprof 任务可复用 | 固定 benchmark 场景、指标输出、性能回归基线 |
| 9 | C-3-5 | 性能调优指南 | ⏳ 未开始 | benchmark suite 与 pprof 结果沉淀 | WorkerPool、Mailbox、连接池、TimingWheel、pprof 调优文档 |
| 10 | C-1-4 | Docker/K8s 部署模板 | ⏳ 未开始 | 配置文档和健康端点已完成 | Dockerfile、compose、K8s Deployment/Service/ConfigMap 示例 |

### 3.2 Phase B：生产就绪（当前主线）

> 目标：补齐安全闭环、错误码收敛和配置生产化能力。

#### B-1 服务间认证与授权（剩余任务）

| 顺序 | 任务 | 状态 | 说明 | 建议验证 |
|------|------|------|------|----------|
| 1 | B-1-6 审计日志 | ⏳ 即将开始 | 记录高权限操作、授权拒绝、策略变更、远端调用身份 | 单元测试 + RPC Handler 授权拒绝集成测试 + 审计事件格式快照测试 |
| 2 | B-1-7 开发证书工具/证书轮转辅助 | ⏳ 即将开始 | 生成开发自签 CA/server/client 证书，提供证书校验与轮转文档 | tlsx 测试 + 脚本 dry-run + 示例配置加载测试 |

#### B-2 结构化错误码体系（剩余任务）

| 顺序 | 任务 | 状态 | 说明 | 建议验证 |
|------|------|------|------|----------|
| 3 | B-2-2 `def` sentinel 错误码化 | 🔄 部分完成 | `def/error.go` 已定义错误码分段规范，剩余关键 sentinel 按模块迁移到 errorx | `errors.Is/As` 兼容测试 + RPC wire error 回归测试 |

#### B-3 配置系统增强

| 顺序 | 任务 | 状态 | 说明 | 建议验证 |
|------|------|------|------|----------|
| 4 | B-3-1 配置边界值测试深化 | 🔄 部分完成 | validator 已接入，需补齐证书路径、端口、超时、队列、池大小等边界 | `go test ./engine/pkg/config/...` + template/example 配置回归 |
| 5 | B-3-3 敏感配置环境变量引用 | ⏳ 未开始 | 密码、Token、证书路径支持环境变量引用或加密存储 | 单元测试覆盖存在/缺失/空值 env，确保错误信息不泄露 secret |
| 6 | B-3-4 配置热加载 | ⏳ 未开始 | 优先支持日志级别、限流阈值、采样率等低风险配置 | 热加载单元测试 + 并发读写 race 测试 |

### 3.3 Phase C：生态完善（后续主线）

#### C-1 运维与部署能力

| 顺序 | 任务 | 状态 | 说明 |
|------|------|------|------|
| 7 | C-1-1 优雅发布支持 | ⏳ 未开始 | 基于 etcd 服务发现、StopGraceTimeout、DrainPolicy 实现滚动更新 |
| 8 | C-1-2 灰度路由 | ⏳ 未开始 | 路由选择器支持 Version/Tag/Weight，实现灰度发布 |
| 9 | C-1-3 控制面 CLI | ⏳ 未开始 | 查看集群状态、服务列表、连接池状态、手动切换主从 |
| 10 | C-1-4 Docker/K8s 部署模板 | ⏳ 未开始 | 完善 `template/docker/`，新增 K8s Deployment/Service/ConfigMap 示例 |
| 11 | C-1-5 Systemd 集成 | ⏳ 未开始 | 实现 `engine/pkg/systemd/` 包（当前为空占位） |

#### C-2 性能优化

| 顺序 | 任务 | 状态 | 说明 |
|------|------|------|------|
| 12 | C-2-4 标准化 benchmark suite | ⏳ 未开始 | 先沉淀可重复基线，再做优化 |
| 13 | C-2-1 RPC 零拷贝优化 | ⏳ 未开始 | Envelope 序列化/反序列化路径减少内存分配 |
| 14 | C-2-2 Mailbox 批量提交 | ⏳ 未开始 | 支持批量 `PostJob`，减少 channel 操作次数 |
| 15 | C-2-3 连接池预热 | ⏳ 未开始 | 启动时预建立指定数量的 RPC 连接 |

#### C-3 文档体系建设

| 顺序 | 文档 | 状态 | 说明 |
|------|------|------|------|
| 16 | C-3-2 API 参考文档 | ⏳ 未开始 | GoDoc + 补充示例和使用注意事项 |
| 17 | C-3-3 架构设计指南 | ⏳ 未开始 | C4 模型图 + 数据流图 + 时序图 |
| 18 | C-3-5 性能调优指南 | ⏳ 未开始 | WorkerPool、连接池、TimingWheel、Mailbox、pprof 使用 |
| 19 | C-3-7 部署运维指南 | ⏳ 未开始 | 单机/集群部署、主从配置、监控接入、日志管理 |

#### C-4 生态扩展

| 顺序 | 任务 | 状态 | 说明 |
|------|------|------|------|
| 20 | C-4-1 Service 容器文档化 | ⏳ 未开始 | 完成 `TODO_SERVICE_CONTAINER.md` 中的注释级梳理 |
| 21 | C-4-2 示例体系重组 | ⏳ 未开始 | 按基础/并发/集群/HTTP 混合/实战业务分类 |
| 22 | C-4-3 插件机制完善 | ⏳ 未开始 | PluginManager 扩展为完整 hook 链 + 生命周期管理 |
| 23 | C-4-4 多语言 SDK（探索） | ⏳ 未开始 | 基于 gRPC proto 定义生成其他语言客户端 |

---

## 四、当前技术债与处理顺序

| 顺序 | 编号 | 类型 | 描述 | 优先级 | 建议处理窗口 |
|------|------|------|------|--------|----------------|
| 1 | TD-04 | 覆盖 | `sysModule`、`sysService`、`utils` 部分包覆盖率低 | 中 | Phase B/C 穿插补齐 |
| 2 | TD-02 | 并发 | `Module.rootContains` 是普通 map，理论上非并发安全 | 低 | 触碰 Module 生命周期时处理 |
| 3 | TD-05 | 代码 | `core/service.go` 体量较大，Init/Start/Stop 可考虑拆分 | 低 | 仅在补生命周期测试后渐进拆分 |
| 4 | TD-01 | 接口 | `IService` 方法数过多，调用侧多只需 2-3 个方法 | 低 | 新增窄接口，不破坏现有 API |
| 5 | TD-03 | 配置 | `config/define.go` 集中存放配置结构体 | 低 | 配置热加载或敏感配置改造时顺手拆分 |

---

## 五、架构演进方向建议

### 5.1 ADR-005：可观测性集成方案

| 项 | 内容 |
|---|------|
| 背景 | Metrics/Health/TraceID 骨架已完成，但分布式 trace 未接 OTel SDK，生产排障仍不完整 |
| 决策 | 保持 Prometheus + OpenTelemetry 双轨方案 |
| Metrics | 继续使用 Prometheus text 输出，统一 Node/RPC/Mailbox/Event/Pool 指标语义 |
| Tracing | 接入 OpenTelemetry SDK，复用 `xcontext.traceId`，按需采样 |
| Health | 保持内建 `/health` + `/ready` + `/metrics` |
| 下一步 | A-1-8：实现 OTel SDK adapter、exporter 配置、RPC/Mailbox span 边界 |

### 5.2 ADR-006：服务间安全认证方案

| 项 | 内容 |
|---|------|
| 背景 | mTLS、RBAC、策略存储与分发已完成，安全闭环缺审计与证书运维能力 |
| 决策 | 继续采用 mTLS（底座）+ RBAC（授权）+ 审计日志（追责） |
| 认证 | 节点间 mTLS，从证书 SAN 或 PID 派生 caller 身份 |
| 授权 | RBAC 方法级粒度，策略存 etcd，本地缓存 + watch 更新 |
| 审计 | 记录高权限操作、拒绝访问、策略变更、远端调用身份 |
| 下一步 | B-1-6 审计日志；B-1-7 开发证书工具/证书轮转辅助 |

### 5.3 核心稳定性原则

| 原则 | 说明 |
|------|------|
| 先测后改 | `Service`、Mailbox、MessageBus、Node lifecycle 变更必须先补失败测试 |
| 先 impact 后编辑 | 触碰函数、方法、结构体前先用 GitNexus impact 复核影响面 |
| 小步迁移 | 对 `Service`、`Node.Start` 这类核心抽象只做渐进式改造，避免一次性重写 |
| 保留兼容 | 对 public API 和示例服务保持兼容，新增窄接口优先于破坏性删改 |
| 指标先行 | 性能优化前先建立 benchmark 与 pprof 基线，避免无证据优化 |

---

## 六、架构风险与缓解措施

| 风险 | 级别 | 当前状态 | 缓解措施 |
|------|------|----------|----------|
| `Service` 核心抽象影响面大 | 🔴 高 | GitNexus impact 为 CRITICAL | 改动前 impact；补生命周期测试；优先新增扩展点，避免破坏字段/方法语义 |
| Mailbox 并发语义复杂 | 🔴 高 | 图谱直接影响低，但运行时风险高 | 增加 drain、panic recover、RW 分离、backpressure、stop deadline 测试 |
| RPC 同步调用路径复杂 | 🟠 中高 | `MessageBus.call` 连接 CallState、Envelope、Handler | 增加 timeout、nil context、错误返回、响应释放、trace 传播测试 |
| 审计缺口 | 🟠 中 | mTLS/RBAC/策略已完成，审计未完成 | 优先实施 B-1-6，记录授权拒绝和高权限操作 |
| OTel 缺口 | 🟠 中 | Metrics/Health 已具备，trace SDK 未完成 | 实施 A-1-8，提供 exporter 配置和 span 边界规范 |
| 证书运维复杂 | 🟠 中 | TLS 能力已落地，工具链不足 | 实施 B-1-7，提供自签脚本、校验工具和轮转文档 |
| 单点 etcd 依赖 | 🟡 中 | 服务发现与策略分发依赖 etcd | 本地缓存兜底，多 etcd 节点，故障注入测试 |
| 文档缺口 | 🟡 中 | 入门和配置文档已完成，运维/调优/API 文档不足 | Phase C-3 按顺序补齐 |

---

## 七、里程碑时间线

```text
2026-03 ─── 开始 ─────────────────────────────

Phase A: 稳固基座
├── A-1  可观测性基础：metrics + health + tracing 骨架 ✅
├── A-2  测试覆盖提升：核心链路大部分完成，剩余 cluster/sysService/utils
└── A-3  技术债清理：go vet 零告警 ✅，race 基线 ✅

2026-05-13 ── P0+P1 完成 ─────────────────────

Phase B: 生产就绪
├── B-1  服务间认证授权：mTLS ✅ + RBAC ✅ + 策略分发 ✅ + 审计 ⏳
├── B-2  结构化错误码：errorx ✅ + RPC wire error ✅ + def sentinel 迁移 🔄
└── B-3  配置系统增强：validator ✅ + 边界测试/敏感配置/热加载 ⏳

2026-06-12 ── GitNexus FTS 修复后复评完成 ─────

2026-07 ─── Phase B 目标完成窗口 ───────────────

Phase C: 生态完善
├── C-1  运维部署：Drain/灰度/CLI/Docker/K8s/Systemd
├── C-2  性能优化：benchmark suite → 零拷贝/批量/预热
├── C-3  文档体系：API/架构/性能调优/部署运维
└── C-4  生态扩展：Service 容器/示例/插件/SDK 探索

2026-11 ─── Phase C 目标完成窗口 ───────────────
```

---

## 八、与现有文档的关系

| 现有文档 | 状态 | 与本文档关系 |
|----------|------|-------------|
| `ARCHITECTURE_REVIEW.md` | 已完成 | 全面架构分析基础 |
| `ROADMAP.md` | 已完成 | 高层愿景参考，本文档负责阶段化跟踪 |
| `P2_OBSERVABILITY_DEV_PLAN.md` | 已完成大部分 | 可观测性基础建设输入，OTel 留待 A-1-8 |
| `OTEL_OBSERVABILITY_PLAN.md` | 待实施 | A-1-8 的设计输入 |
| `P5_SECURITY_DEV_PLAN.md` | 部分完成 | mTLS/RBAC/策略已完成，审计和证书工具继续跟踪 |
| `P5_POLICY_DISTRIBUTION_DEV_PLAN.md` | 已完成 | 策略存储与分发已归入完成能力 |
| `ERRORX_LEGACY_MIGRATION_PLAN.md` | 已完成大部分 | errorlib 已移除，剩余 `def` sentinel 错误码化 |
| `TODO_SERVICE_CONTAINER.md` | 待整合 | C-4-1 Service 容器文档化输入 |
| `DESIGN_MULTI_NODE.md` | 历史完成 | Node 自包含里程碑记录 |
| `DESIGN_MULTI_NODE_PHASE3_FIXLIST.md` | 历史完成 | 建议归档到 `docs/archive/` |
| `DESIGN_ISSUES_FIXLIST.md` | 历史完成 | 建议归档到 `docs/archive/` |

---

## 九、更新日志

- **2026-06-12**：基于 GitNexus FTS 修复后复评重整全文结构；新增当前复评结论、关键影响面、已完成能力归档、未完成任务先后顺序、风险重排与验证原则。确认 `Service` 为 CRITICAL 影响面核心抽象，Mailbox/RPC 为 P0 稳定性重点；下一步优先级调整为审计日志、证书工具、OTel SDK 适配器、`def` sentinel 错误码化与配置生产化增强。
- **2026-06-10**：复核当前源码状态。errorx 基础能力与 RPC wire error 已完成；errorlib legacy 包已由 errorx 完全替代并删除；TimingWheel 完成并发稳定性修复与 benchmark 补齐。
- **2026-05-16**：P5-10~P5-15 策略存储与分发全部完成。新增 `policy.go`、`store.go`、`watcher.go`、`etcd_store.go` + 20 tests、AuthzConf 配置、模板示例。安全能力升至 ⭐⭐⭐½。
- **2026-05-14**：P5 策略存储与分发完成拆分规划，新增 `P5_POLICY_DISTRIBUTION_DEV_PLAN.md`，拆为 P5-10~P5-15。
- **2026-05-14**：P5 RBAC 授权引擎完成（authz 包 24 tests + Principal 身份模型 + RPC Handler 拦截集成），安全能力升至 ⭐⭐⭐。
- **2026-05-14**：P5 mTLS 安全底座完成。新增 tlsx 工具包（LoadServerTLS/LoadClientTLS，12 tests），gRPC server/client 支持基于配置的 mTLS，NATS client sender 集成 TLS。
- **2026-05-14**：P4 文档产品化完成，新增 `CONFIG_REFERENCE.md`、`QUICK_START.md`、`SERVICE_DEV_GUIDE.md`、`node_concurrency/README.md`、`example/ReadMe.md`。
- **2026-05-14**：P3 RPC/Cluster 韧性增强全部完成，测试覆盖升至 ⭐⭐⭐⭐½。
- **2026-05-12**：Actor 目录重整和配置基线回归已完成，配置体系评分升至 ⭐⭐⭐⭐，测试覆盖升至 ⭐⭐⭐。

---

*本文档作为后续迭代的主跟踪文档。每次 Phase 完成、GitNexus 复评或关键架构变更后应同步更新。*
