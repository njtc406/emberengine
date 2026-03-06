# EmberEngine 架构分析报告

> **分析时间**: 2026-07  
> **分析范围**: 全项目源码、设计文档、Roadmap  
> **分析分支**: `v2-dev-node-fix` (HEAD: ef08ca6)  
> **构建状态**: `go build ./...` ✅ | `go vet ./...` ✅ | `go test ./...` ✅ | `go test -race ./engine/pkg/monitor/...` ✅

> **修复更新（2026-03-06）**  
> 已按架构反馈完成并入代码：P3-16、P3-17、NEW-01、NEW-02、NEW-03、NEW-04、NEW-05、NEW-06、NEW-07、NEW-08、NEW-09、NEW-10、NEW-11、NEW-13、NEW-14。  
> 对应实现文件：`engine/pkg/monitor/monitor.go`、`engine/pkg/services/services.go`、`engine/pkg/services/diagnostics.go`、`engine/pkg/profiler/adapter.go`、`engine/pkg/core/service.go`、`engine/pkg/core/module.go`、`engine/pkg/core/module_lookup.go`、`engine/pkg/node/node.go`、`engine/pkg/node/diagnostics.go`、`engine/pkg/cluster/endpoints/endpoints.go`、`engine/pkg/event/eventBus.go`、`engine/pkg/cluster/cluster.go`、`engine/pkg/rpc/client/pool/factory.go`、`engine/pkg/config/define.go`、`engine/pkg/config/config.go`。  
> 已验证：`go build ./...` ✅，`go test ./...` ✅。

---

## 目录

1. [项目概览](#1-项目概览)
2. [整体架构评估](#2-整体架构评估)
3. [分层架构分析](#3-分层架构分析)
4. [核心子系统详细分析](#4-核心子系统详细分析)
5. [接口设计分析](#5-接口设计分析)
6. [已知问题与风险](#6-已知问题与风险)
7. [改进建议](#7-改进建议)
8. [ADR 汇总](#8-adr-汇总)
9. [总结](#9-总结)

---

## 1. 项目概览

### 1.1 定位

EmberEngine 是一个面向游戏服务的 **Go Actor 微服务框架**，核心目标是：

- 统一集群中所有模块的交互方式
- 提供 Actor 模型的并发隔离
- 支持多协议 RPC（gRPC / NATS / RPCX）
- 基于 etcd 的服务发现与集群管理

### 1.2 技术栈

| 领域 | 选型 |
|------|------|
| 语言 | Go 1.24 |
| RPC | gRPC + NATS + RPCX |
| 序列化 | Protobuf |
| 服务发现 | etcd v3 |
| 日志 | zap |
| 协程池 | ants/v2 |
| 定时器 | 自实现 TimingWheel（分层时间轮） |
| 数据库 | MySQL (gorm) + Redis + MongoDB |
| 消息队列 | NATS（事件总线） |

### 1.3 核心模块一览

```
pkg/
├── node/           — Node 自包含运行时入口
├── interfaces/     — 抽象接口层（INodeContext, IService 等）
├── config/         — 配置管理
├── log/            — 日志（zap 封装）
├── actor/mailbox/  — Actor 邮箱（WorkerPool + 中间件）
├── core/           — 服务基类（Service/Module/RPC Handler）
├── services/       — 服务管理器（工厂注册 + 运行时管理）
├── event/          — 事件总线（NATS + 本地分发）
├── cluster/        — 集群（etcd Discovery + EndpointManager）
├── rpc/            — RPC 层（MessageBus + Sender + 连接池）
├── monitor/        — RPC 超时监控
├── router/         — 路由选择器
├── profiler/       — 性能 Profiler
├── plugins/        — 插件管理
├── utils/          — 工具库（TimingWheel, ShardedLock, etc.）
├── sysModule/      — 系统模块（MySQL, Redis, Gate）
└── sysService/     — 系统服务（PProf, HTTP）
```

---

## 2. 整体架构评估

### 2.1 评分总表

| 维度 | 评分 | 说明 |
|------|------|------|
| **自包含改造** | ⭐⭐⭐⭐⭐ | Node 自包含目标彻底达成，全局变量清零 |
| **分层设计** | ⭐⭐⭐⭐ | 层次清晰，依赖方向明确 |
| **Actor 模型** | ⭐⭐⭐⭐⭐ | Mailbox/WorkerPool/RW分离设计精良 |
| **接口抽象** | ⭐⭐⭐⭐ | 窄接口模式优秀，少量重复可优化 |
| **生命周期管理** | ⭐⭐⭐⭐ | Start/Stop 回滚机制完善，少许竞态待修 |
| **RPC 层** | ⭐⭐⭐⭐ | 多协议支持好，连接池设计完整 |
| **可观测性** | ⭐⭐ | 基础 profiler 有，缺 metrics/tracing |
| **测试覆盖** | ⭐⭐ | 核心路径缺乏单元测试 |
| **文档** | ⭐⭐⭐ | 设计文档详尽，API/使用文档不足 |

### 2.2 架构亮点

#### ✅ Node 自包含设计（核心亮点）

Node 作为唯一的运行时上下文持有者，彻底消除了所有包级可变全局变量。这是本框架最大的架构优势：

- **可嵌入性**：任何 Go 进程可通过 `node.New().Start(...)` 集成框架
- **多实例隔离**：同一进程可运行多个独立 Node（集成测试、本地集群模拟）
- **干净生命周期**：cleanups 栈 + defer 回滚，启动失败零残留

```go
// Node 持有全部运行时状态 — Phase 1~4 完整
type Node struct {
    Config           *config.Config           // Phase 1
    *log.Logger                               // Phase 1
    AntsPool         *asynclib.Pool           // Phase 1
    TimingWheel      *timingwheel.TimingWheel // Phase 1
    DeDuplicator     inf.IDeDuplicator        // Phase 1
    RpcMonitor       *monitor.RpcMonitor      // Phase 2
    EventBus         *event.Bus               // Phase 2
    Cluster          *cluster.Cluster         // Phase 2
    ServiceMgr       *services.ServiceManager // Phase 2
    PoolManager      *pool.PoolManager        // Phase 3
    SenderMgr        *client.SenderManager    // Phase 3
    MethodIndex      *rpc.MethodIndex         // Phase 3
    BusFactory       *msgbus.MessageBusFactory// Phase 3
    ProfilerRegistry *profiler.Registry       // Phase 4
    PluginManager    *plugins.PluginManager   // Phase 4
    Router           *router.Router           // Phase 4
}
```

#### ✅ 窄接口模式

`INodeContext` 通过窄接口（INodeEndpointManager / INodeEventBus / INodeRouter 等）解决循环依赖，同时限制组件间的访问面：

```go
// interfaces 包不 import 具体实现包，通过窄接口隐式满足
type INodeEndpointManager interface {
    CreatePid(...)
    AddService(svc IService)
    RemoveService(svc IService)
    ToPrivateService(svc IService)
}
```

这比 `interface{}` + 类型断言或 `Get*Any()` 的早期方案优雅得多。

#### ✅ Mailbox / WorkerPool 设计

- **一致性哈希路由**：同一 key 的 Job 总是分配到同一 Worker，保证有序性
- **RW 读写分离**：ReadOnly 方法并发执行，Write 方法独占执行
- **洋葱模型中间件**：限流、熔断、统计可插拔
- **DrainPolicy**：优雅停机策略可配置（执行/丢弃）
- **自动扩缩容**：WorkerPool 可根据负载动态调整 Worker 数

#### ✅ 错误透传改造

全面完成了 `panic/fatal → error` 的改造，运行时代码不会因为初始化失败而 panic（仅保留白名单语义的 panic：deque 边界断言、worker 参数断言、log 显式语义入口）。

---

## 3. 分层架构分析

### 3.1 依赖层次图

```
Layer 5 (应用层)     example/          用户代码
                        ↓
Layer 4 (服务管理)    services/         ServiceManager
                        ↓
Layer 3 (核心层)     core/             Service / Module / Handler
                        ↓
Layer 2 (基础设施)   node/ cluster/ rpc/ event/ monitor/ router/
                        ↓
Layer 1 (抽象层)     interfaces/ def/ dto/ config/ log/
                        ↓
Layer 0 (工具层)     utils/ actor/mailbox/ profiler/ plugins/
```

**评价**：层次基本合理，依赖方向自上而下。但存在以下耦合：

### 3.2 依赖耦合问题

| 问题 | 严重度 | 位置 | 说明 |
|------|--------|------|------|
| core/service 直接依赖 cluster + endpoints 具体类型 | 🟡中 | `service.go` | 通过 `SetRuntimeDeps` 注入具体类型，未完全走窄接口 |
| event 包同时承载本地事件和 NATS 全局事件 | 🟡中 | `eventBus.go` | 800+ 行，职责过重 |
| config 包定义过于集中 | 🟢低 | `define.go` 438行 | 所有配置结构体集中在一个文件 |
| services.SetService 包级全局锁 | 🟢低 | `services.go` | init() 阶段注册，运行时只读 — 可接受 |

---

## 4. 核心子系统详细分析

### 4.1 Node 生命周期

#### 启动流程（`Node.Start()`）

```
1. Config.Load()          — 配置解析
2. Logger                 — 日志初始化
3. AntsPool              — 协程池
4. TimingWheel           — 时间轮
5. DeDuplicator          — 去重器
6. RpcMonitor            — RPC 超时监控
7. PoolManager           — 连接池管理
8. SenderMgr             — 发送器管理
9. MethodIndex           — 方法索引
10. BusFactory           — MessageBus 工厂
11. ProfilerRegistry     — Profiler 注册中心
12. PluginManager        — 插件管理
13. Cluster + Router     — 集群 + 路由
14. EventBus             — 事件总线
15. Hooks                — 用户钩子
16. ServiceMgr.Init/Start — 服务启动
```

**设计合理性：✅ 好**
- 初始化顺序正确（配置 → 日志 → 基础设施 → 核心组件 → 服务）
- cleanups 栈保证启动失败时的逆序回滚
- 每一步失败都会返回带上下文的 error

#### 停止流程（`Node.Stop()`）

```
1. Services.StopAll()    — 服务（逆序）
2. EventBus.Stop()       — 事件总线
3. Cluster.Close()       — 集群
4. SenderMgr.Close()     — 发送器
5. PoolManager.Close()   — 连接池
6. RpcMonitor.Stop()     — RPC 监控
7. DeDuplicator.Close()  — 去重器
8. TimingWheel.Stop()    — 时间轮
9. AntsPool.Release()    — 协程池
10. Logger.Close()       — 日志（最后）
```

**设计合理性：✅ 好**
- 停止顺序正确：先停服务→再停基础设施→最后关日志
- `stopped` 字段使用 `atomic.Bool` + CAS 防止重复调用

**⚠️ 问题**：Start() 中的 cleanups 栈和 Stop() 中的显式顺序是**两套独立逻辑**。如果未来新增组件，需要同步修改两处——存在"遗忘"风险。

> **建议**：考虑统一为一套注册式管理，例如将 cleanups 栈作为 Node 字段保留，Stop() 直接逆序执行该栈。

---

### 4.2 Service / Module / RPC Handler

#### Service 生命周期

```
Init:  config → logger → timer → mailbox → module → event → PID → method → RPC handler → OnInit
Start: mailbox.Start → startListenCallback → OnStart → AddService → OnStarted
Stop:  CAS → suspend → timers → concurrent → release → removeService → logger
```

**设计合理性：✅ 好**
- `rollbackInitResources()` 在 Init 失败时清理已分配资源
- `initErr` 字段阻止半初始化状态的 Service 被启动
- 停止流程使用 CAS + `stopRequested` 双重保护

#### Module 层级体系

Module 提供了服务内部的模块化组织能力：
- 父子层级关系（tree 结构）
- 模块自动分配 ID（种子自增）
- 模块注册时自动扫描 RPC 方法
- 释放时递归清理子模块 + 从 MethodMgr 注销方法

**⚠️ 问题**：

1. **大量 `GetBaseModule().(*Module)` 类型断言**：✅ **已修复（2026-03-06）**。`module.go` 已改为安全断言与降级处理：
    - 新增 `asCoreModule()` / `rootCoreModule()` 统一执行类型检查
    - `AddModule()` 在非 `*Module` 场景返回 error，不再 panic
    - `ReleaseModule()` / `GetModule()` / `newModuleID()` 改为安全分支，避免 nil+断言链路崩溃

```go
// module.go 中多处出现
pModule := module.GetBaseModule().(*Module)
m.GetRoot().GetBaseModule().(*Module).rootContains[...]
```

2. **rootContains 非并发安全**：`rootContains map[uint32]inf.IModule` 是普通 map，如果 AddModule/ReleaseModule 在不同 goroutine 调用（虽然当前设计可能保证串行），存在理论风险。

#### RPC Handler（反射方法注册）

```go
// 方法签名扫描规则
方法前缀：Api*/Rpc*/ApiRo*/RpcRo*
参数：(context.Context, *proto.Message) → (proto.Message, error) 或多返回值
```

**设计合理性：✅ 好**
- `compileCallFunc` 在启动阶段预编译调用闭包，运行期无反射开销
- `MethodMgr` 在启动阶段写入后即为只读 static table，运行期 GetMethodFunc 无锁
- `IReadOnlyDeclarer` 接口允许手动声明只读方法，与前缀自动检测互补

---

### 4.3 RPC / MessageBus 系统

#### 调用链路

```
用户代码
  → Router.Select()            // 选择目标服务
  → MessageBus.Call()          // 创建 Envelope + 超时监控
  → Dispatcher.DeliverRequest() // 路由到 sender
  → LocalSender / RemoteSender  // 本地投递或远程发送
     → 本地: mailbox.PostJob()
     → 远程: gRPC/NATS/RPCX 发送
```

**设计合理性：✅ 好**
- MessageBus 统一了 Call/AsyncCall/Send 三种模式
- MessageBusFactory 对象池化 MessageBus，避免频繁分配
- Sender 三协议透明切换

**⚠️ 问题**：

1. **MessageBus.call() 中 reflect 校验**：每次 Call 都用 `reflect.TypeOf(out).Kind()` 校验出参类型。这是热路径，虽然代价不大，但可以在注册阶段通过预编译消除。

2. **gRPC Sender 连接数硬编码为 `NumCPU/2`**：

```go
cpuNum := runtime.NumCPU()
connNum := cpuNum / 2
if connNum < 1 { connNum = 1 }
```

应改为可配置参数。

3. **NATS Sender 连接池大小通过环境变量 `EMBER_NATS_SENDER_POOL` 控制**：生产环境不应依赖环境变量作为核心配置，应纳入统一配置体系。

#### 连接池管理 (`pool/manager.go`)

连接池设计完整：
- 动态扩缩容（负载阈值触发）
- 健康检查
- 熔断器
- 多种负载均衡策略（round_robin / least_connections / fastest_response）

**⚠️ 问题**：706 行的 manager.go 职责偏重，建议拆分健康检查、扩缩容、指标收集为独立文件。

---

### 4.4 事件系统 (Event Bus)

#### 三级事件模型

```
全局事件 (Global)    — 所有节点收到
服务器事件 (Server)  — 同 partition 收到
特定事件 (Specific)  — 指定 serviceUid 收到
```

**设计合理性：✅ 好**
- 三级事件覆盖了大多数游戏业务场景
- NATS 作为跨节点传输，本地直接内存分发
- 限流管理器 + 批处理定时器（100ms）

**⚠️ 问题**：

1. **eventBus.go 职责过重**：✅ **已修复（2026-03-06）**，已按职责拆分为：
    - `bus_global.go` — 全局事件
    - `bus_server.go` — 服务器事件
    - `bus_specific.go` — 特定事件
    - `bus_nats.go` — NATS/TLS 与订阅管理
    - `bus_batch.go` — 批处理刷新逻辑

2. **批处理定时器在非 NATS 模式下也启动**（`Init()` 中无条件 `go eb.processBatchedEvents()`）：单机模式下浪费资源。

3. **NATS TLS 配置 `InsecureSkipVerify: true`** 注释着 "TODO 有风险"——✅ **已修复（2026-03-06）**：
    - 移除硬编码 `InsecureSkipVerify: true`
    - 新增 `NatsConf.InsecureSkipVerify`（默认 `false`）与 `NatsConf.TLSServerName`
    - TLS 最低版本设为 `TLS1.2`，默认启用证书校验

---

### 4.5 集群系统 (Cluster / Endpoints / Discovery)

#### 服务发现流程

```
etcd Watch → PushEvent → Cluster.run() → eventProcessor.Trigger()
  → EndpointManager.updateServiceInfo() → Repository.Add()
  → Router.Select() 可以路由到新服务
```

**设计合理性：✅ 好**
- Watch + 健康检查 + 自动重连 + 指数退避
- Repository 使用 `sync.Map` + 分片锁索引
- 临时连接 5min TTL 自动清理

**⚠️ 问题**：

1. **Cluster.eventChannel 固定 1024 缓冲**：高峰期如果事件处理慢，channel 会阻塞 PushEvent 调用者。应可配置。✅ **已修复（2026-03-06）**：新增 `ClusterConf.EventChannelSize`，默认 `1024`。

2. **EndpointManager.stopped 字段是普通 bool**（非 atomic），多 goroutine 访问时不安全。✅ **已修复（2026-03-06）**：改为 `atomic.Bool`。

3. **Repository 的两级索引 `mapSvcBySNameAndSUid` / `mapSvcBySTpAndSName` 使用普通 map + ShardedRWLock**，锁粒度与索引键不匹配（锁是分片的，但 indexAdd/indexRemove 拿的是同一个锁分片吗？需要验证）。

---

### 4.6 Monitor（RPC 超时监控）

**设计合理性：✅ 好**
- 分桶设计（默认 256 桶，2 的幂 + 掩码）减少锁竞争
- epoch + seq 生成唯一 ID，无需外部依赖
- TimingWheel 调度器处理超时回调

**✅ 已修复数据竞态（P3-16，2026-03-06）**：

```go
// Stop() 中
rm.sd = nil      // 写

// listen() 中
rm.sd.GetTimerCbChannel()  // 读（在另一个 goroutine）
```

历史问题：`Stop()` 不等待 `listen()` goroutine 退出就将 `rm.sd` 置 nil，导致 data race。

**已落地修复**：
1. listen() 在循环前缓存 channel：`ch := rm.sd.GetTimerCbChannel()`
2. Stop() 移除 `rm.sd = nil`，改由 GC 回收
3. Stop() 增加 `rm.wg.Wait()` 等待 listen() 退出

---

## 5. 接口设计分析

### 5.1 INodeContext 设计 ⭐⭐⭐⭐⭐

```go
type INodeContext interface {
    GetConfig() INodeConfig
    GetLogger() log.ILoggerX
    GetAntsPool() INodePool
    GetTimingWheel() INodeTimingWheel
    GetDeDuplicator() IDeDuplicator
    GetNodeId() string
    GetNodeType() string
    GetNodeUid() string
    IsClusterMode() bool
    GetEndpointManager() INodeEndpointManager
    GetEventBus() INodeEventBus
    GetRouter() INodeRouter
    GetProfilerRegistry() INodeProfilerRegistry
    GetMethodIndex() INodeMethodIndex
}
```

**设计优雅**：
- 窄接口隐式满足（Go duck typing），无需显式 implements
- 清晰的循环依赖边界文档（注释中列出了哪些包不能 import）
- 基础设施层返回安全的窄接口而非具体类型

### 5.2 IService 设计 ⭐⭐⭐⭐

组合多个窄接口：
```go
type IService interface {
    ILifecycle          // Init/Start/Stop
    IIdentifiable       // GetName/GetPid/GetPartition
    IServiceHandler     // PostJob/GetMailbox/GetRpcHandler
    // ...
}
```

**问题**：接口方法数偏多（20+），部分下游只需要其中 2-3 个方法。建议在调用侧使用更窄的内部接口。

### 5.3 ProfilerAdapter 重复 ⭐⭐

`profilerAdapter` 和 `profilerRegistryAdapter` 在 `node.go` 和 `core/service.go` 中**各实现了一份**。`service.go` 中的版本还少了 mutex 保护（与 node.go 中的 `sync.Mutex` 版本不一致）。

✅ **已修复（NEW-01，2026-03-06）**：已抽取共享实现到 `engine/pkg/profiler/adapter.go`，`node.go` 与 `service.go` 统一复用线程安全版本。

| 位置 | 版本 | mutex |
|------|------|-------|
| `node.go` | 有 `sync.Mutex` 保护 stack | ✅ |
| `service.go` | 无锁保护 stack | ❌ |

应抽取为共享实现，统一使用带锁版本。

---

## 6. 已知问题与风险

### 6.1 阻断级（必须修复）

| 编号 | 问题 | 位置 | 影响 |
|------|------|------|------|
| **P3-16** | monitor.go 数据竞态（已修复） | `monitor.go` | 历史阻断项，已于 2026-03-06 修复 |

### 6.2 中等级别

| 编号 | 问题 | 位置 | 影响 |
|------|------|------|------|
| **P3-17** | services.Init() RLock 持锁范围过大（已修复） | `services.go` | 历史中风险项，已于 2026-03-06 修复 |
| **NEW-01** | profilerAdapter 重复实现且不一致（已修复） | `profiler/adapter.go`、`node.go`、`service.go` | 已统一共享实现并补齐并发保护 |
| **NEW-02** | EndpointManager.stopped 非 atomic（已修复） | `endpoints.go` | 已改为 atomic.Bool |
| **NEW-03** | eventBus.go 职责过重 (800+ 行)（已修复） | `eventBus.go`、`bus_global.go`、`bus_server.go`、`bus_specific.go`、`bus_nats.go`、`bus_batch.go` | 已完成职责拆分，降低单文件复杂度 |
| **NEW-04** | gRPC 连接数 / NATS 池大小硬编码（已修复） | `sender_remote_grpc.go`, `sender_remote_nats.go`, `config/define.go` | 已支持配置化调优 |
| **NEW-05** | Node.Start() cleanups 栈与 Stop() 逻辑重复（已修复） | `node.go` | 已统一为注册式清理步骤，Stop 复用同一清理栈逆序执行 |

### 6.3 低级别 / 建议

| 编号 | 问题 | 位置 | 影响 |
|------|------|------|------|
| **NEW-06** | Module 中大量 `(*Module)` 类型断言（已修复） | `module.go` | 已引入内部 `coreModuleCarrier`（`CoreModule() *Module`）桥接，外部 `IModule` 合约不变；并替换为安全断言与错误返回，避免 panic |
| **NEW-07** | Cluster.eventChannel 缓冲大小固定 1024（已修复） | `cluster.go`、`config/define.go` | 已支持通过配置调整缓冲区大小 |
| **NEW-08** | NATS TLS `InsecureSkipVerify: true`（已修复） | `eventBus.go`、`config/define.go` | 已改为配置化，默认安全校验证书 |
| **NEW-09** | MessageBus.call() 热路径 reflect（已修复） | `bus.go` | 已移除发送前反射校验热路径，改为响应赋值阶段统一校验并补充多返回值长度检查 |
| **NEW-10** | pool/manager.go 706 行单文件（已修复） | `pool/manager.go`、`pool/manager_types.go`、`pool/manager_runtime.go` | 已按职责拆分为结构定义/类型定义/运行逻辑三个文件，降低单文件复杂度 |
| **NEW-11** | 批处理定时器非 NATS 模式下无条件启动（已修复） | `eventBus.go` | 非 NATS 模式不再启动批处理 ticker/goroutine |
| **NEW-12** | 测试覆盖率不足（阶段性完成，暂缓继续） | 全项目 | 核心高风险路径已补充：`core/module_lookup_test.go`、`rpc/client/pool/manager_runtime_test.go`、`rpc/client/pool/factory_test.go`、`services/services_test.go`、`rpc/message/msgbus/bus_test.go`（含 MultiBus 模式覆盖）、`cluster/endpoints/repository/repository_test.go`、`cluster/endpoints/endpoints_test.go`；其余低优先级模块后续按需补齐 |
| **NEW-13** | 模块间调用需类型断言（已修复） | `core/module_lookup.go` | 已提供泛型辅助 `GetModule[T](hierarchy, id) (T, bool)`，统一处理不存在/类型不匹配场景并返回 `(zero, false)` |
| **NEW-14** | PoolManager 在无 logger 场景下 panic（已修复） | `rpc/client/pool/factory.go` | 已为 `GetOrCreatePool/Close/RemovePool` 的日志输出增加 nil 保护，避免空指针 panic |

---

## 7. 改进建议

### 7.1 短期（1-2 周）

#### 7.1.1 修复 monitor.go 数据竞态 (P3-16) ✅ 已完成（2026-03-06）

```go
// 修复方案
func (rm *RpcMonitor) Stop() {
    if !rm.closed.CompareAndSwap(false, true) { return }
    rm.cancel()
    if rm.sd != nil {
        rm.sd.Stop()
        // 不再 rm.sd = nil，避免 listen() 中的竞态
    }
    rm.wg.Wait() // 等待 listen() 退出
    for _, bucket := range rm.buckets {
        bucket.Clear()
    }
}

func (rm *RpcMonitor) listen() {
    defer rm.wg.Done()
    ch := rm.sd.GetTimerCbChannel() // 启动时缓存 channel
    for {
        select {
        case t, ok := <-ch: // 使用缓存的 channel
            // ...
        case <-rm.ctx.Done():
            return
        }
    }
}
```

#### 7.1.2 修复 services.Init() 锁范围 (P3-17) ✅ 已完成（2026-03-06）

```go
func (sm *ServiceManager) Init(serviceConf *config.ServiceConf) error {
    // 在锁内复制工厂，锁外执行 Init
    lock.RLock()
    entries := make([]struct{ name string; builder func() inf.IService }, 0)
    for _, initConf := range serviceConf.StartServices {
        if builder, ok := serviceMap[initConf.ClassName]; ok {
            entries = append(entries, struct{ name string; builder func() inf.IService }{initConf.ClassName, builder})
        }
    }
    lock.RUnlock()

    for _, entry := range entries {
        svc := entry.builder()
        // ... Init 在锁外执行
    }
    return nil
}
```

#### 7.1.3 合并重复的 profilerAdapter ✅ 已完成（2026-03-06）

将 `profilerAdapter` / `profilerRegistryAdapter` 抽取到 `profiler` 包或 `interfaces` 辅助包中，统一使用带 `sync.Mutex` 的版本。

#### 7.1.4 EndpointManager.stopped 改为 atomic.Bool ✅ 已完成（2026-03-06）

```go
type EndpointManager struct {
    stopped atomic.Bool // 替代 bool
}
```

### 7.2 中期（1-2 月）

#### 7.2.1 拆分 EventBus

将 800+ 行的 `eventBus.go` 按职责拆分：

```
event/
├── bus.go                 — Bus 结构体 + Init/Stop
├── bus_global.go          — 全局事件订阅/分发
├── bus_server.go          — 服务器事件
├── bus_specific.go        — 特定事件
├── bus_nats.go            — NATS 连接管理
├── bus_throttle.go        — 限流 + 批处理
└── bus_metrics.go         — 事件指标
```

#### 7.2.2 统一 Node 生命周期管理

引入 `Component` 接口 + 注册表模式，避免 Start/Stop 两套逻辑：

```go
type Component interface {
    Name() string
    Start() error
    Stop()
}

type Node struct {
    components []Component // 按启动顺序注册
}

func (n *Node) Stop() {
    for i := len(n.components) - 1; i >= 0; i-- {
        n.components[i].Stop()
    }
}
```

#### 7.2.3 配置化 RPC 参数

将 gRPC 连接数、NATS 连接池大小、Cluster eventChannel 缓冲等纳入统一配置体系：

```yaml
NodeConf:
    GrpcSenderConnNum: 4           # gRPC sender 每个远端地址连接数（<=0 回退 NumCPU/2）
    EventBusConf:
        NatsConf:
            SenderPoolSize: 4          # NATS sender 连接池大小（<=0 回退默认1）
ClusterConf:
    EventChannelSize: 4096         # 替代硬编码 1024
```

#### 7.2.4 增加核心路径单元测试

优先覆盖以下模块：
- `core/service.go` — Init/Start/Stop 生命周期
- `rpc/message/msgbus/bus.go` — Call/AsyncCall/Send
- `cluster/endpoints/` — AddService/RemoveService
- `monitor/monitor.go` — Add/Remove/超时回调
- `services/services.go` — ServiceManager Init/Start/StopAll

### 7.3 长期（3+ 月）

#### 7.3.1 可观测性体系

当前仅有基础 Profiler 和 EventMetrics。建议分阶段接入：

已完成第一步（2026-03-06）：
- 新增 `services.GetRuntimeSummary()`：输出服务数量与服务名列表。
- 新增 `node.GetRuntimeSnapshot()`：输出 Node UID、集群模式、运行时长、服务摘要、连接池指标快照。

1. **Prometheus Metrics**：RPC QPS/延迟/错误率、Mailbox 队列长度、Pool 命中率
2. **OpenTelemetry Tracing**：基于现有 `xcontext.traceId` 接入 Jaeger
3. **Health Check**：提供 `/health` `/ready` 端点

#### 7.3.2 减少 Module 层的类型断言

引入 `IModuleInternal` 内部接口，将 `rootContains` / `moduleIdSeed` 等需要跨模块访问的字段方法化：

```go
type IModuleInternal interface {
    GetRootContains() map[uint32]inf.IModule
    AllocModuleID() uint32
    // ...
}
```

#### 7.3.3 文档体系建设

- API 参考文档（GoDoc）
- 服务开发快速入门
- 配置项完整说明
- 架构设计图（C4 模型）
- 性能调优指南

---

## 8. ADR 汇总

### ADR-001: Node 自包含设计

| 项 | 内容 |
|---|------|
| **上下文** | 框架各模块使用包级全局变量，无法在同一进程运行多个 Node |
| **决策** | 所有运行时状态收归 Node 结构体，通过 INodeContext 接口注入 |
| **正面** | 可嵌入、多实例隔离、干净生命周期 |
| **负面** | 初始化链较长，组件间通过 INodeContext 间接访问增加了一层抽象 |
| **状态** | ✅ 已实施（Phase 1-4 完成） |

### ADR-002: 窄接口解决循环依赖

| 项 | 内容 |
|---|------|
| **上下文** | interfaces 包不能 import 依赖它的实现包 |
| **决策** | 定义 INodeEndpointManager / INodeEventBus 等窄接口，由具体类型隐式满足 |
| **替代方案** | `Get*Any() → interface{}` + 类型断言（已废弃，不安全） |
| **正面** | 编译期类型安全，接口面窄 |
| **负面** | 窄接口与实际类型可能漂移（需 CI 编译检查） |
| **状态** | ✅ 已实施 |

### ADR-003: RW 读写分离在 Mailbox 层实现

| 项 | 内容 |
|---|------|
| **上下文** | 游戏服务中读多写少，需要提升读并发 |
| **决策** | WorkerPool 内置 RW 模式（RLock/WLock），ReadOnly 方法并发执行 |
| **替代方案** | 在业务层手动加锁（侵入性强） |
| **正面** | 对用户透明，前缀自动识别 + 手动声明两种方式 |
| **负面** | 实现复杂，RW 切换时需要 drain 所有读操作 |
| **状态** | ✅ 已实施 |

### ADR-004: 多协议 RPC 透明支持

| 项 | 内容 |
|---|------|
| **上下文** | 不同场景需要不同 RPC 协议（gRPC 跨语言、NATS 高吞吐、RPCX Go 原生） |
| **决策** | SenderManager 统一管理，通过 IRpcSender 接口抽象，按 rpcType 路由 |
| **正面** | 用户透明，一行配置切换协议 |
| **负面** | 维护三套 Sender 实现，协议特性差异需要额外处理 |
| **状态** | ✅ 已实施 |

---

## 9. 总结

### 9.1 项目整体评价

EmberEngine 经过 Node 自包含改造后，架构质量显著提升。**核心设计合理**，主要亮点在于：

1. **Node 自包含**：彻底消除全局变量，这是框架领域少见的高质量改造
2. **Actor/Mailbox 设计**：一致性哈希 + RW 分离 + 洋葱中间件，设计精良
3. **错误透传改造**：全面 panic→error，大幅提升鲁棒性
4. **窄接口模式**：优雅解决循环依赖，编译期安全

### 9.2 最需关注的改进方向

| 优先级 | 方向 | 原因 |
|--------|------|------|
| **P0** | 增加单元测试覆盖（阶段二） | 核心高风险路径已补齐，但 `core/node/rpc-remote` 等模块覆盖率仍低 |
| **P1** | 可观测性 | 当前几乎裸跑，出问题后排查困难 |
| **P2** | 继续补齐低优先级测试 | `sysModule/sysService/config/utils` 大量包仍为 0% 覆盖 |
| **P2** | 跑通更大范围 race 检查 | 当前已验证 `monitor` 包，建议逐步扩大到核心链路包 |
| **P3** | 文档体系 | 有良好的设计文档，缺使用指南 |

### 9.3 扩展性评估

| 规模 | 评估 |
|------|------|
| **10K 并发** | 当前架构完全支持 |
| **100K 并发** | 需要：Redis 集群缓存、CDN、配置化连接池 |
| **1M 并发** | 需要：微服务拆分、读写分离数据库、多 Region |

---

*本文档基于源码静态分析生成，建议定期更新。*
