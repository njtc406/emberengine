# 🔍 EmberEngine 全量代码评估报告

> **评估日期**: 2026-05-16  
> **评估范围**: `engine/pkg/` 下全部 22 个包、367 个 Go 源文件  
> **评估方法**: 四维度并行审查（核心框架 / 网络RPC / 安全工具 / 系统服务）  

---

## 一、项目价值评估

### 1.1 项目定位

EmberEngine 是一个**面向高性能、高并发场景的工业级微服务 Actor 框架**，基于 Go 语言实现。其核心能力覆盖：

| 能力领域 | 成熟度 | 说明 |
|:---|:---|:---|
| Actor 模型 | ⭐⭐⭐⭐⭐ | 完整的 PID/Mailbox/WorkerPool 体系，支持多优先级队列、RW 读写分离 |
| RPC 通信 | ⭐⭐⭐⭐ | 支持 gRPC/NATS/rpcx 三后端，含连接池、负载均衡、熔断 |
| 集群管理 | ⭐⭐⭐⭐ | Etcd 服务发现、选主、端点管理 |
| 安全机制 | ⭐⭐⭐⭐ | RBAC 授权引擎、JWT 认证、TLS 加固 |
| 可观测性 | ⭐⭐⭐ | Prometheus 指标、Zap 日志、分布式追踪接口 |
| 插件系统 | ⭐⭐ | 基础框架已定义，热加载/热更新未实现 |

### 1.2 技术亮点

1. **RW 读写分离模式**：Mailbox 支持读操作异步批量派发，极大提升 I/O 密集型服务吞吐量——这是业界 Actor 框架中少见的特性。
2. **泛型 Job 注册**：利用 Go 1.18+ 泛型消除反射和运行时类型断言，编译期即可确定 payload 类型。
3. **多层防护体系**：Sentinel 中间件（限流/熔断）、Panic 捕获（safeExec）、日志防爆（RateLimit）构成纵深防御。
4. **零全局变量设计**：通过 `Node` 容器收归所有依赖，消除全局单例，可测试性强。
5. **资源契约文档**：`IMailboxChannel.PostJob` 等关键接口附带详尽的资源释放契约，降低使用者出错概率。

### 1.3 综合评价

| 维度 | 评分 | 评语 |
|:---|:---|:---|
| 架构设计 | **9.0/10** | 接口导向、分层清晰、依赖倒置 |
| 代码质量 | **8.5/10** | 注释详尽、命名规范、并发处理成熟 |
| 安全性 | **8.0/10** | 核心防护到位，存在提升空间 |
| 完整性 | **7.5/10** | 核心功能完备，部分模块有 TODO |
| 文档质量 | **8.0/10** | 设计文档丰富，但代码注释有少量过时 |

**总体评级**: 🟢 **内部网络生产可用（Internal Production-Ready）**。框架默认面向受控内网/VPC/安全组隔离环境；跨不可信网络或多租户网络部署时，应开启增强安全模式（RBAC + mTLS 或 PID 签名）。

---

## 二、代码质量总览

### 2.1 各包评分汇总

| 包 | 评分 | 亮点 | 主要问题 |
|:---|:---|:---|:---|
| `actor/mailbox` | 9.5 | RW 模式、COW 快照、中间件池 | 并发路径深，调试难度大 |
| `node` | 9.5 | 栈式清理、Hook 系统、诊断 | Stop panic 防护已修复；按设计不设置超时 |
| `core` | 8.5 | 泛型 Handler、生命周期管理 | Service 结构体过大 |
| `authz` | 9.0 | RBAC 引擎、ETCD 策略同步 | failOpen 已加固，PID 真实性待设计 |
| `interfaces` | 9.0 | 接口隔离、资源契约文档 | IRWModeJob 类型断言频繁 |
| `def` | 9.0 | 优先级设计、常量化 | 系统常量和业务默认值混放 |
| `rpc` | 8.5 | 多后端、连接池、熔断 | 锁竞争、连接泄露风险 |
| `router` | 9.0 | 策略分离、灵活性高 | 依赖 Repository 索引性能 |
| `cluster` | 8.0 | 选主、服务发现 | sharded worker 已落地，仍需压测背压策略 |
| `services` | 9.0 | 依赖注入、优雅停机 | daemon 大量 TODO |
| `sysModule` | 8.5 | 多协议适配、模块化 | Gate Supervisor 核心已落地，配置/Health/metrics 待接入 |
| `sysService` | 8.0 | 运维端点解耦 | 配置加载模式不统一 |
| `log` | 7.5 | Zap 封装、Node 级隔离 | 自定义 TraceLevel 兼容性 |
| `metrics` | 8.0 | Prometheus 导出 | 缺少 Histogram/Summary |
| `tracing` | 9.0 | 接口先行、Noop 实现 | 待集成真实 SDK |
| `monitor` | 9.0 | 分段锁、TimingWheel | epoch ID 碰撞风险 |
| `profiler` | **6.0** | 轻量耗时分析 | pushRecordLog Bug 已修复，建议复评 |
| `event` | 8.5 | NATS 总线、限流批处理 | NATS 凭据未体现 |
| `plugins` | 4.0 | 基础定义存在 | 热加载未实现 |
| `utils/errorx` | 10.0 | 错误码、调用栈、结构化 | 无 |
| `utils/xcontext` | 9.0 | TraceID 流转、工厂模式 | — |
| `utils/tlsx` | 9.5 | 安全护栏、TLS≥1.2 | — |
| `utils/network` | 8.5 | 读限制、连接数限制 | WS Origin 默认过宽 |
| `utils/jwtx` | 8.0 | HS256 校验 | 全局 Secret 可变 |
| `utils/httpx` | 8.0 | 敏感信息脱敏 | 缺少安全响应头 |
| `systemd` | N/A | 占位符 | 仅有 README |

---

## 三、漏洞与缺陷（按严重程度排序）

### 🔴 严重（必须修复）

#### **B1 — profiler/pushRecordLog 操作了错误的链表** ✅ 已修复

- **文件**: `engine/pkg/profiler/profiler.go:130`
- **问题**: `pushRecordLog` 在 record 列表满时，从 `slf.stack`（调用栈）删除元素，而非从 `slf.record`（记录列表）删除

```go
// 当前（错误）
func (slf *Profiler) pushRecordLog(record *Record) {
    if slf.record.Len() >= DefaultMaxRecordNum {
        front := slf.stack.Front()   // ❌ 应该是 slf.record
        if front != nil {
            slf.stack.Remove(front)  // ❌ 应该是 slf.record
        }
    }
    slf.record.PushBack(record)
}
```

- **影响**: 正在 Profile 中的调用栈元素被意外删除 → `Pop()` 时找不到对应的 `*list.Element` → **Panic**；record 列表无限增长 → **内存泄漏**
- **优先级**: 🔴 P0 — **阻塞合并**
- **修复**: 将 `slf.stack.Front()/Remove` 改为 `slf.record.Front()/Remove`，同时修复 B5。新增 `profiler_test.go` 覆盖测试。

---

### 🟠 高（建议尽快修复）

#### **B2 — authz watcher 的 failOpen 默认策略** ✅ 已修复

- **文件**: `engine/pkg/authz/watcher.go:60`
- **问题**: `failOpen=true` 时，初始策略加载失败会放行所有请求。生产环境应默认 fail-closed
- **风险**: 攻击者可利用策略加载失败的窗口期绕过所有授权检查
- **优先级**: 🟠 P1
- **修复**: 确认默认值已为 fail-closed（Go 零值 `false`），在 `PolicyWatcherConfig.FailOpen` 字段增加安全警告注释。

#### **B3 — RPC handler 层缺少统一的授权中间件** ✅ 已修复

- **文件**: `engine/pkg/rpc/remote/handler/handler.go:80`
- **问题**: RPC 请求反序列化后直接分发给业务处理器，未在框架层统一注入 authz 校验
- **风险**: 业务开发者可能遗漏在 handler 中添加授权检查，导致未授权访问
- **优先级**: 🟠 P1
- **修复**: Handler 新增 `authorizer` 字段和 `SetAuthorizer()` 方法，在 dedup 之后、payload decode 之前调用 `authorizeRequest()` 拦截未授权请求。Node 在远端 RPC 服务启动前创建并注入 Authorizer，避免启动窗口绕过。新增 4 个测试用例。
- **剩余优化**: ✅ `AuthzConf` / `PolicyWatcher` 已接入 Node 启动流程，支持 local/etcd 策略来源，初始加载失败默认 fail-closed。

### 🟡 中（建议在下一迭代修复）

#### **B4 — authz 信任 PID 真实性但无签名验证** ⚠️ 按部署边界处理

- **文件**: `engine/pkg/authz/authz.go:37`
- **问题**: `PrincipalFromPID` 完全信任传入的 PID 对象。如果传输层（如未启用 mTLS）未验证 PID 真实性，攻击者可伪造 PID 绕过授权
- **风险**: 在跨不可信网络、跨租户网络或节点可被非受信客户端直连的场景下存在授权绕过；在受控内网/VPC/安全组隔离环境下风险较低
- **优先级**: 🟡 P2（内部网络默认模式可后置；增强安全模式建议实现）
- **建议**: 不强制默认开启 PID 签名。保留扩展点，按部署模式选择：内部网络模式不启用；跨网络/高安全模式启用 mTLS peer identity 校验或 HMAC 签名。

#### **B5 — profiler record 列表无限增长** ✅ 已修复

- **文件**: `engine/pkg/profiler/profiler.go:130`
- **问题**: 同 B1，但即便 B1 修复后，`DefaultMaxRecordNum=100` 使用硬编码常量而非 `slf.maxRecordNum` 字段
- **优先级**: 🟡 P2
- **修复**: 将 `DefaultMaxRecordNum` 改为 `slf.maxRecordNum`，随 B1 一并修复。

#### **B6 — node Stop 过程 panic 防护** ✅ 已修复

- **文件**: `engine/pkg/node/node.go:143`
- **问题**: `stopCleanups` 中某个清理函数 panic 会导致后续步骤全部跳过
- **影响**: 残留资源未释放
- **优先级**: 🟡 P2
- **修复**: 每个清理步骤在独立闭包中执行，配合 per-step panic recover。设计上 Node Stop 必须等待所有 goroutine 退出，不设超时。

#### **B7 — cluster 事件处理器单协程瓶颈** ✅ 已修复

- **文件**: `engine/pkg/cluster/cluster.go:53`
- **问题**: 事件处理器 `run` 使用单 goroutine，若某个订阅者处理缓慢，会拖慢整个集群状态同步
- **优先级**: 🟡 P2
- **修复**: 引入 sharded worker pool，`PushEvent` 按事件 key（etcd Key）哈希直接写入对应 shard channel，N 个 worker 并行处理。新增 `EventWorkerCount` 配置（默认 1 向后兼容）。关闭路径加入 `closeOnce` + shard 读写锁，避免并发 Close/PushEvent 触发 send-on-closed-channel。
- **剩余优化**: 仍需压测 shard 队列背压、热点 key 倾斜和 worker 数配置建议。

#### **B8 — Gate 模块的 Listen Goroutine 无健康检查** ✅ 已修复

- **文件**: `engine/pkg/sysModule/gate/`（Gate.Start 方法）
- **问题**: 网关监听 goroutine 异常退出后无自动重启机制，可能导致服务静默失联
- **优先级**: 🟡 P2
- **修复**: 引入 `superviseServe` 循环，支持错误分类（永久/临时/正常关闭）、指数退避重启、`ListenAndServe` panic 捕获，并暴露 `IsServing()`/`LastServeError()`/`RestartCount()` 健康状态。`RestartPolicy` 已接入 `GateService` 配置。新增 11 个测试用例。
- **剩余优化**: HealthService `/ready` 汇总和 metrics 指标尚未落地。

#### **B9 — MsgEnvelope 的 RWMutex 在高频路径竞争**

- **文件**: `engine/pkg/rpc/message/msgenvelope/envelope.go:64`
- **问题**: 信封对象通过 sync.Pool 复用，但内部使用 RWMutex，在高并发下仍有锁竞争
- **优先级**: 🟡 P2

#### **B10 — PID 序列化时 Clone 带来内存压力**

- **文件**: `engine/pkg/actor/pid.go:63`
- **问题**: `PrepareForMarshal` 为了规避 race 而 Clone PID，在大规模 RPC 场景下产生大量临时对象
- **优先级**: 🟡 P2

---

### 🟢 低（可排入后续迭代）

#### **B11 — Sentinel 中间件测试中 typed nil 已修复待验证**

- **文件**: `engine/pkg/actor/mailbox/sentinel_middleware_test.go`
- **说明**: 已修复，需要持续关注类似模式
- **优先级**: 🟢 P3

#### **B12 — httpx 缺少默认安全响应头** ✅ 已修复

- **文件**: `engine/pkg/utils/httpx/gin.go`, `engine/pkg/utils/httpx/security_headers.go`
- **问题**: 未默认设置 HSTS、CSP、X-Content-Type-Options 等安全响应头
- **优先级**: 🟢 P3
- **修复**: 新增 `SecurityHeaders` 中间件，默认设置 X-Content-Type-Options/X-XSS-Protection/X-Frame-Options/Referrer-Policy/CSP，HSTS 可配置开启。支持通过 `SecurityHeadersConf` 配置开关/覆盖/关闭。GinServer.Init 默认注入。新增 5 个测试用例。

#### **B13 — jwtx 全局 Secret 可运行时修改** ✅ 已修复

- **文件**: `engine/pkg/utils/jwtx/jwt.go`, `engine/pkg/utils/jwtx/keyring.go`, `engine/pkg/utils/jwtx/keyring_provider.go`
- **问题**: `SetDefaultSecret` 可在运行期随意更改，可能导致已签发 Token 全部失效
- **优先级**: 🟢 P3
- **修复**: 引入 `KeyRing` + `KeyRingProvider`，签发 token 写入 kid，校验按 kid 查找 key，支持平滑轮换。secret 最小长度 16 bytes，防御性拷贝。`SetDefaultSecret`/`SetDefaultProvider` 标记为 Deprecated。新增 12 个测试用例。

#### **B14 — Service 结构体过大**

- **文件**: `engine/pkg/core/service.go`
- **问题**: Service 聚合了 logger、mailbox、workerPool、rpcHandler、monitor、sysCtl 等过多职责
- **优先级**: 🟢 P3（重构建议，非缺陷）

#### **B15 — Plugin 系统仅为基础定义**

- **文件**: `engine/pkg/plugins/plugin.go`
- **问题**: 未实现热加载、热更新、跨进程插件等核心功能
- **优先级**: 🟢 P3（功能缺失，非缺陷）

---

## 四、架构改进建议

### 4.1 短期（1-2 个迭代）

| 建议 | 涉及文件 | 预期收益 | 状态 |
|:---|:---|:---|:---|
| 修复 B1 profiler bug | `profiler/profiler.go` | 消除 Panic 风险和内存泄漏 | ✅ 已修复 |
| RPC 层注入统一授权中间件 | `rpc/remote/handler/` | 杜绝授权遗漏 | ✅ 核心已修复，策略加载已接入 |
| authz 默认 fail-closed | `authz/watcher.go` | 安全加固 | ✅ 已修复 |
| Node Stop panic 防护 | `node/node.go` | 优雅停机可靠性 | ✅ 已修复（无超时设计） |
| Gate goroutine 健康检查 | `sysModule/gate/` | 网关可用性 | ✅ 已修复（含配置化，Health/metrics 待完善） |

### 4.2 中期（3-6 个月）

| 建议 | 涉及文件 | 预期收益 |
|:---|:---|:---|
| Repository 迁移 go-memdb | `cluster/endpoints/repository/` | 路由查询性能提升 |
| Cluster 事件处理器异步化 | `cluster/cluster.go` | 状态同步性能（核心已落地，需压测调参） |
| PID 零拷贝序列化 | `actor/pid.go` | 大规模 RPC 吞吐提升 |
| MsgEnvelope 锁优化 | `rpc/message/msgenvelope/` | 高频场景性能 |

### 4.3 长期（6 个月以上）

| 建议 | 涉及文件 | 预期收益 |
|:---|:---|:---|
| Plugin 热加载实现 | `plugins/` + `services/daemon.go` | 不停机更新 |
| Tracing 集成 OpenTelemetry | `tracing/` | 分布式链路追踪 |
| Metrics 增加 Histogram/Summary | `metrics/` | 更精细的可观测性 |
| Service 职责拆分 | `core/service.go` | 可维护性 |

---

## 五、修复计划

### 阶段 1：紧急修复（立即执行） ✅ 已完成

```
1. B1: profiler/pushRecordLog 错误操作链表 ✅
   - 文件: engine/pkg/profiler/profiler.go
   - 改动: 将 slf.stack.Front()/Remove 改为 slf.record.Front()/Remove
   - 同时: 将 DefaultMaxRecordNum 改为 slf.maxRecordNum
   - 风险: 低
   - 测试: 已新增 profiler_test.go（3 个测试用例）
```

### 阶段 2：安全加固（本周） ✅ 内部网络模式已满足，增强安全待选做

```
2. B3: RPC handler 注入统一授权中间件 ✅
   - 文件: engine/pkg/rpc/remote/handler/handler.go
   - 改动: 新增 authorizer 字段 + SetAuthorizer + authorizeRequest，在 dedup 后 decode 前拦截
   - 注入: node.go 在远端 RPC 服务启动前创建 Authorizer 并调用 remoteMsgHandler.SetAuthorizer()
   - 测试: 新增 4 个测试用例

3. B2: authz failOpen 默认值审查 ✅
   - 文件: engine/pkg/authz/watcher.go
   - 改动: 确认默认 fail-closed，增加安全警告注释

4. B4: PID 真实性验证（增强安全模式，可选）
   - 文件: engine/pkg/authz/authz.go
   - 改动: 如果跨不可信网络部署，增加 mTLS peer identity 校验或 PID HMAC 签名
   - 状态: 内部网络默认模式暂不强制；跨网络/高安全部署前再实现
```

### 阶段 3：可靠性提升（本迭代） ✅ 核心已完成

```
5. B6: Node Stop panic 防护 ✅
   - 改动: 每个清理步骤在闭包中执行 + per-step recover
   - 设计: 必须等待所有 goroutine 退出，不设超时

6. B7: Cluster 事件 sharded worker pool ✅
   - 改动: PushEvent 按 key 哈希直接写入 shard channel，N 个 worker 并行处理
   - 配置: EventWorkerCount（默认 1 向后兼容）

7. B8: Gate goroutine supervisor ✅
   - 改动: superviseServe 循环 + 错误分类 + 指数退避重启 + 健康状态暴露
   - 测试: 新增 11 个测试用例
   - 配置: RestartPolicy 已接入 GateService 配置
   - 待完善: HealthService ready 汇总、metrics 指标
```

---

## 六、成功标准

- [x] B1 修复且新增 profiler 单元测试通过
- [x] B2 failOpen 策略已确认/修改
- [x] B3 RPC 授权中间件已注入（4 个测试通过）
- [x] B6 Node Stop per-step panic recover
- [x] B7 Cluster 事件 sharded worker pool
- [x] B8 Gate supervisor（11 个测试通过）
- [x] B12 httpx 安全响应头（5 个测试通过）
- [x] B13 jwtx KeyRing + kid（12 个测试通过）
- [x] B9/B10 Benchmark 已补充（7 个 benchmark）
- [x] 所有 `go build ./...` 和 `go vet ./...` 通过
- [x] 所有 `go test ./...` 通过
- [ ] `go test -race ./...` 通过（当前环境缺少 GCC，无法启用 CGO/-race；已通过 `go build/vet/test ./...` 全量验证）

---

## 七、部署安全模式建议

EmberEngine 当前主要面向内部网络部署，不建议默认套用公网零信任模型。推荐把安全能力做成分层开关：框架提供统一入口和增强能力，默认配置服务于受控内网的易用性。

| 模式 | 适用场景 | 默认策略 |
|:---|:---|:---|
| 内部网络默认模式 | 同一 VPC/内网、安全组隔离、节点由同一团队控制 | Authz 默认关闭；保留统一授权入口；不强制 PID 签名；不强制 mTLS |
| 增强安全模式 | 跨团队共享集群、跨机房、重要业务服务 | Authz 可配置开启；策略 fail-closed；建议启用 mTLS peer identity 校验 |
| 跨不可信网络模式 | 节点可能被非受信客户端直连、多租户或公网边界 | Authz 必须开启；mTLS 或 PID HMAC 签名至少开启一种；策略加载失败拒绝启动或拒绝请求 |

**结论**：严格 auth 机制需要保留为框架能力，但不必默认全开。B3 的统一授权入口属于基础护栏，应保留；B4 的 PID 真实性验证属于增强安全能力，可按部署边界选择实现。

---

## 八、复核后的剩余问题与优化建议

### 8.1 仍未修复 / 未完全落地

1. **B4：PID 真实性验证未实现（P2，增强安全模式）**
   - 现状：`authz.PrincipalFromPID` 仍信任 wire message 中的 `SenderPid`。
   - 判断：框架主要用于受控内部网络，默认不强制 PID 签名是合理取舍。
   - 建议：保留为增强安全能力；跨不可信网络、跨团队共享集群或节点可被非受信客户端直连时，再接入 mTLS peer identity 校验或 message-level HMAC 签名。

2. **Authz 策略加载已接入 Node（P2）** ✅ 已修复
   - 现状：Node 启动流程在创建 Authorizer 后，根据 `ClusterConf.AuthzConf` 配置决定是否 `Enable(true)` 并启动 `PolicyWatcher`。
   - 实现：支持 `local`（本地 YAML 文件）和 `etcd`（etcd KV watch）两种策略来源；etcd 模式创建独立客户端避免与 Discovery 耦合；初始加载失败默认 fail-closed；PolicyWatcher 注册到 stopCleanups 保证优雅停机。
   - 新增文件：`engine/pkg/authz/etcd_kv_adapter.go`（EtcdKVAdapter + NewEtcdClientForAuthz）。

3. **B8 Gate Supervisor 配置化已完成（P2）** ✅ 已修复
   - 现状：`GateService` 配置已增加 `RestartPolicy` 字段（Enable/MaxRestart/InitialBackoff/MaxBackoff）；Gate.Start 从配置读取并应用。`isPermanentError` 补充了 `net.AddrError` 识别；新增 `isNormalShutdown` 识别 `net.ErrClosed` 和 "use of closed network connection"（避免正常关闭触发重启）。新增 4 个测试用例（isPermanentError/isNormalShutdown/NormalShutdownError/RestartPolicyFromConfig），Gate 测试总计 11 个。
   - 剩余：HealthService `/ready` 汇总和 metrics 指标（gate_serving/gate_restart_total）尚未落地。

4. **B9/B10 Benchmark 已补充，当前性能可接受（P2）** ✅ Benchmark 已落地
   - B9：`MsgEnvelope` RWMutex 开销经 benchmark 验证：SetGet 约 57ns/op（单线程），并发读约 112ns/op，零内存分配。当前开销在合理范围内，不急于优化。
   - B10：`PID` SnapshotForWire 约 329ns/op，单次分配 192B；并发约 91ns/op。对比完整 Marshal 路径 938ns/op，Clone 占约 35%。
   - 建议：当 RPC QPS 达到百万级时再考虑手写轻量 copy 替代 proto.Clone；当前并发场景扩展性良好。
   - 新增文件：`engine/pkg/rpc/message/msgenvelope/benchmark_test.go`（7 个 benchmark）。

5. **B12/B13 已修复（P3）** ✅
   - B12：httpx 已默认注入安全响应头中间件（X-Content-Type-Options/X-XSS-Protection/X-Frame-Options/Referrer-Policy/CSP），HSTS 可配置，业务可覆盖。
   - B13：jwtx 新增 KeyRing + KeyRingProvider，支持多 key 平滑轮换，旧接口标记 Deprecated。

### 8.2 当前实现的优化建议

- **Cluster**：补充慢 handler / 同 key 顺序 / 热点 key 倾斜 / Close 并发的测试与 benchmark；生产默认 `EventWorkerCount` 可从 1 逐步调到 4/8。
- **RPC Handler**：授权拒绝目前通过返回 error 传回 transport，后续可统一封装为框架级 error code，便于调用方区分业务错误和授权错误。
- **Gate**：`isPermanentError` 已补充 `net.AddrError` 识别；新增 `isNormalShutdown` 识别 `net.ErrClosed` 和 `use of closed network connection`，降低误报。✅
- **文档/CI**：把 `go test -race ./...` 或至少关键包 race 测试加入发布前检查；安全说明应明确区分“内部网络默认模式”和“跨不可信网络增强模式”。

---

## 九、待细化设计方案

本节把已识别但不宜仓促改动的架构项拆成可实施设计。总体原则：安全项默认 fail-closed，可靠性项默认不丢关键状态，性能项先压测再替换热路径实现。

### D1 — B3：RPC Handler 统一授权中间件设计（核心已实现）

**目标**：把服务间授权从业务 handler 下沉到 RPC 框架入口，保证所有远端请求在投递到目标服务前都经过一致的 RBAC 校验。

**涉及文件**：

- `engine/pkg/rpc/remote/handler/handler.go`
- `engine/pkg/node/node.go`
- `engine/pkg/authz/authz.go`
- `engine/pkg/interfaces/IRpcClient.go`（如需抽象授权 hook）

**现状入口**：

`Handler.RpcMessageHandler` 在非 reply 请求路径中完成 dedup、payload decode、context 构建、envelope 构建，然后调用：

```go
err := sf.GetDispatcher(req.ReceiverPid).DeliverRequest(ctx, envelope)
```

授权应插入在 `DeliverRequest` 之前，并尽量放在 payload decode 之前，避免未授权请求消耗反序列化成本。

**推荐方案**：

1. 在 `handler.Handler` 增加可选字段：

```go
authorizer *authz.Authorizer
```

2. 增加注入方法，避免破坏 `NewHandler` 现有调用方：

```go
func (h *Handler) SetAuthorizer(a *authz.Authorizer) {
   h.authorizer = a
}
```

3. 在 `node.Start` 中 RPC 服务启动前创建 `n.Authorizer` 并注入 remote handler，避免启动窗口绕过。历史设计有两种方案：

- 方案 A：提前创建 `n.Authorizer`，再创建 `remoteMsgHandler` 并注入；
- 方案 B：保留创建顺序，在创建 Authorizer 后调用 `remoteMsgHandler.SetAuthorizer(n.Authorizer)`。

当前实现采用方案 A 的思路：提前创建 Authorizer，并在 Cluster/RPC 监听启动前注入。

4. 授权逻辑：

```go
func (h *Handler) authorizeRequest(req *actor.Message) error {
   if h.authorizer == nil || !h.authorizer.IsEnabled() {
      return nil
   }
   sender := req.GetSenderPid()
   receiver := req.GetReceiverPid()
   if sender == nil || receiver == nil {
      return fmt.Errorf("authz: missing sender or receiver pid")
   }
   principal := authz.PrincipalFromPID(sender)
   return h.authorizer.Authorize(principal, receiver.GetName(), req.GetMethod())
}
```

5. reply 消息不做业务授权：reply 只是完成本地 monitor state，应按 `ReqId` 匹配；普通请求在 Authz 启用后必须授权。

6. 拒绝策略：

- `NeedResp=true`：返回标准错误响应给调用方，避免调用方直到超时才感知拒绝；
- `NeedResp=false`：返回错误并记录 warn/error，调用方无需 reply；
- 日志必须包含 `sender serviceType/serviceName/nodeUid`、`receiver serviceName`、`method`、`reqId`，不要打印完整 payload。

**测试策略**：

- 授权关闭：旧行为不变，所有请求可投递；
- 授权开启且允许：请求正常投递；
- 授权开启且拒绝：dispatcher 不被调用；
- sender/receiver nil：fail-closed；
- reply 消息：不触发 authz；
- NeedResp=true 拒绝路径：调用方收到明确 error。

**风险与缓解**：

- 风险：handler 初始化顺序调整影响 cluster init。缓解：使用 `SetAuthorizer` 后置注入，保持构造函数兼容。
- 风险：错误响应格式不统一。缓解：复用 `errorx.Marshal` / 现有 RPC error path。

### D2 — B4：PID 真实性验证设计（增强安全模式，可选）

**目标**：在跨不可信网络、跨团队共享集群或高安全业务场景下，避免仅凭 wire message 中的 `SenderPid` 建立 Principal，降低 PID 伪造导致的授权绕过风险。内部网络默认模式下不强制启用。

**推荐分层**：

1. **内部网络默认模式：不强制 PID 签名**
   - 依赖 VPC/安全组/内网边界隔离；
   - Authz 默认关闭，可按业务配置开启；
   - 保留统一授权入口，避免业务层遗漏授权检查；
   - 文档中明确：该模式不适合跨不可信网络。

2. **增强安全模式：mTLS 绑定节点身份**
   - gRPC/rpcx/NATS 连接必须支持 TLS；
   - 证书 SAN/CN 映射到 `NodeUid` 或受信任节点名；
   - RPC handler 从 transport context 获取 peer identity，与 `SenderPid.NodeUid` 对比。

3. **跨协议增强：PID wire 签名**
   - 新增 `PIDSignature` 或 message-level `AuthSignature`；
   - 签名内容建议包含：`sender.ServiceUid`、`sender.NodeUid`、`receiver.ServiceUid`、`method`、`reqId`、`deadline`；
   - 使用 HMAC-SHA256 或节点私钥签名；密钥从配置/环境注入，不进入代码仓库；
   - `deadline` 参与签名，防止长期重放。

4. **防重放协同**
   - 现有 dedup 使用 `senderServiceUid + reqId`；
   - 签名校验应在 dedup 前后都可工作，推荐顺序：基础字段校验 → 签名校验 → dedup → authz → decode → dispatch。

**接口设计**：

```go
type PrincipalVerifier interface {
   VerifyRPC(ctx context.Context, msg *actor.Message) (authz.Principal, error)
}
```

默认实现：

- `NoopVerifier`：仅用于兼容或单机模式；
- `MTLSPeerVerifier`：校验证书身份与 SenderPid；
- `HMACVerifier`：校验 message 签名。

**配置建议**：

```yaml
Authz:
  Enable: true
   PrincipalVerifyMode: none # none | mtls | hmac | mtls+hmac
  FailOpen: false
```

默认值建议为 `none`，匹配内部网络部署；当 `Enable=true` 且部署模式声明为 `untrusted` 时，应要求 `PrincipalVerifyMode != none`。

**成功标准**：

- 内部网络默认模式下，不引入额外签名开销；
- 增强安全模式下，远端请求不能只靠伪造 `SenderPid.ServiceType` 获得权限；
- 本地/测试模式仍可显式使用 `none`；跨不可信网络模式若未配置 mTLS/HMAC，应给出警告或拒绝启动。

### D3 — B7：Cluster 事件处理器异步化设计（核心已实现）

**目标**：避免单个慢订阅者阻塞集群状态同步，同时保持服务上下线事件的顺序语义。

**现状**：

`Cluster.run()` 单 goroutine 从 `eventChannel` 读取，然后同步调用：

```go
c.eventProcessor.Trigger(evt.GetContext(), evt.GetEventType(), evt.GetData())
```

**推荐方案**：按事件 key 分片的 worker 模型。

1. 增加配置：

```go
ClusterEventWorkerCount int // 默认 1，向后兼容；建议生产 4/8/16
ClusterEventQueueSize   int // 默认复用 EventChannelSize
```

2. 事件分片 key：

- 服务级事件：`ServiceUid` 或 `NodeUid + ServiceName`；
- 节点级事件：`NodeUid`；
- 无法提取 key：固定 shard 0，保证保守顺序。

3. 数据流（最终实现简化为直接分片）：

```text
discovery -> Cluster.PushEvent -> shard queue -> worker -> eventProcessor.Trigger
```

4. 背压策略：

- 集群状态事件默认不能静默丢弃；
- shard queue 满时应阻塞或返回明确错误；
- 可为非关键事件增加 `DropIfFull` 标记，但服务上下线不允许丢。

5. 关闭流程：

- `Close()` 通过 `closeOnce` 保证幂等；
- 先关闭 `closed`，让新事件快速返回错误；
- 停止 discovery/endpoints，减少事件来源；
- 通过 shard 锁关闭 shard queues；
- 等待 workers 退出。

**测试策略**：

- 慢订阅者不阻塞其他 shard；
- 同一 service key 的事件保持顺序；
- Close 时无 goroutine 泄漏；
- 队列满时行为符合配置。

### D4 — B8：Gate Listen Goroutine 健康检查与重启设计（核心已实现）

**目标**：避免 `ListenAndServe` 异常退出后 Gate 静默失联。

**现状**：

`Gate.Start` 直接启动 goroutine：

```go
go func() {
   if err := g.adapter.ListenAndServe(g, sConf); err != nil {
      g.GetLogger().Warnf("listen and serve error: %v", err)
   }
}()
```

**推荐方案**：引入 gate supervisor。

1. Gate 增加运行状态字段：

```go
ctx context.Context
cancel context.CancelFunc
serveDone chan error
restartCount atomic.Int64
```

2. Start 入口改为 `superviseServe`：

```text
Start -> supervise loop -> ListenAndServe -> err classify -> backoff -> restart or stop
```

3. 错误分类：

- 正常 Shutdown 返回：不重启；
- 端口占用/配置错误：不重启，标记模块启动失败；
- 临时网络错误：指数退避重启；
- 连续失败超过 `MaxRestart`：停止并上报健康状态。

4. 配置建议：

```yaml
Gate:
  RestartPolicy:
      Enable: true
      MaxRestart: 5
      InitialBackoff: 500ms
      MaxBackoff: 10s
```

5. 健康状态：

- `Gate` 暴露 `IsServing()` / `LastServeError()` / `RestartCount()`；
- HealthService `/ready` 汇总 Gate 状态；（待实现）
- metrics 增加 `gate_serving`、`gate_restart_total`、`gate_last_error_timestamp`。（待实现）

**测试策略**：

- adapter 立即返回临时错误时按 backoff 重启；
- adapter 返回永久错误时不重启；
- `OnRelease` 能取消 supervise loop 并调用 `Shutdown`；
- 超过最大重启次数后 ready=false。

### D5 — B9：MsgEnvelope 锁优化设计

**目标**：降低高频 RPC envelope get/set 的锁开销，但不破坏当前资源引用契约。

**推荐分阶段实施**：

1. **阶段 1：压测与观测**
   - 增加 benchmark：`SetMeta/SetData/GetMeta/GetData/ToProtoMsg`；
   - 在真实 RPC 压测中打开 mutex profile，确认锁竞争是否是瓶颈。

2. **阶段 2：不可变构建器**
   - envelope 在投递前由单 goroutine 构建；
   - 投递后视为 immutable；
   - 删除大部分 setter/getter 锁，仅保留 ref-count 的并发安全。

3. **阶段 3：兼容迁移**
   - 保留旧接口，内部增加 `sealed` 状态；
   - debug 模式下若投递后再 Set，直接 panic 或记录错误。

**风险**：如果业务或中间件在异步路径修改 envelope，去锁会暴露数据竞争。必须先用 debug 断言确认契约。

### D6 — B10：PID 序列化 Clone 压力优化设计

**目标**：在保持 data race 安全的前提下降低 `proto.Clone(pid)` 的分配成本。

**推荐方案**：

1. 保留 `MarshalPID` / `SnapshotForWire` 作为唯一安全出口；
2. 为 `PID` 增加手写轻量 wire copy：

```go
func (pid *PID) SnapshotForWireFast() *PID {
   return &PID{
      Address: pid.GetAddress(),
      Name: pid.GetName(),
      ServiceType: pid.GetServiceType(),
      ServiceId: pid.GetServiceId(),
      State: atomic.LoadInt32(&pid.State),
      Partition: pid.GetPartition(),
      Version: pid.GetVersion(),
      RpcType: pid.GetRpcType(),
      NodeUid: pid.GetNodeUid(),
      ServiceUid: pid.GetServiceUid(),
      IsMaster: pid.IsMasterNode(),
   }
}
```

3. benchmark 对比：`proto.Clone` vs 手写 copy；只有收益明显时替换热路径。

**注意**：不能为了省分配重新直接写原 PID 的 `IsMaster` 字段，否则会重新引入 data race。

### D7 — B12：HTTP 安全响应头设计

**目标**：为 `httpx`/Gin 服务提供默认安全头能力，同时允许业务覆盖。

**建议默认头**：

- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY` 或 `SAMEORIGIN`
- `Referrer-Policy: no-referrer`
- `Content-Security-Policy` 默认保守配置，允许业务关闭或覆盖
- HTTPS 环境下启用 `Strict-Transport-Security`

**配置建议**：

```yaml
HttpSecurityHeaders:
  Enable: true
  HSTS: true
  FrameOptions: DENY
  CSP: "default-src 'self'"
```

**测试策略**：默认启用、业务覆盖、关闭开关、HTTPS/HSTS 条件分支。

### D8 — B13：JWT Secret 管理设计

**目标**：避免运行期任意修改全局 JWT secret，同时支持平滑轮换。

**推荐方案**：

1. 弃用直接修改全局 secret 的运行期接口，保留兼容但标记 Deprecated；
2. 引入 key ring：

```go
type KeyRing struct {
   ActiveKID string
   Keys map[string][]byte
}
```

3. 签发 token 时写入 `kid`；校验 token 时按 `kid` 查找旧 key；
4. secret 最小长度建议 32 bytes；
5. key 从环境变量、配置中心或 secret manager 注入，不写入代码。

**测试策略**：短 secret 拒绝、旧 key 可验、新 key 签发、未知 kid 拒绝。

---

## 十、附录：审查方法说明

本次评估采用四维度并行审查：

1. **核心框架审查**（Explore Agent）：`core/` `actor/` `def/` `interfaces/` `dto/` — 关注架构设计、并发模式、类型系统
2. **网络RPC审查**（Explore Agent）：`rpc/` `router/` `cluster/` `node/` — 关注通信安全、连接管理、集群协调
3. **安全工具审查**（Explore Agent）：`authz/` `jwtx/` `tlsx/` `network/` `httpx/` `httplib/` `utils/` — 关注安全漏洞、注入风险
4. **系统服务审查**（Explore Agent）：`sysService/` `sysModule/` `services/` `log/` `metrics/` `tracing/` `monitor/` `profiler/` `event/` `plugins/` — 关注可观测性、生命周期、运维能力

每个代理独立完成代码阅读和分析，最终汇总为本报告。人工复查了 B1 等关键发现的代码行。

---

> 📋 关联文档: [ACTOR_CODE_REVIEW_2026Q2.md](ACTOR_CODE_REVIEW_2026Q2.md) | [R2_R4_FIX_PLAN.md](R2_R4_FIX_PLAN.md)
