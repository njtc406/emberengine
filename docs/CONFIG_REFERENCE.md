# EmberEngine 配置参考手册

> 本文档基于 `engine/pkg/config/define.go` 和 `template/config/node.yaml` 自动生成。  
> 配置文件格式：YAML，顶层键为 `NodeConf`、`ClusterConf`、`ServiceConf`、`SystemLogger`。

---

## 目录

- [1. NodeConf — 节点基础配置](#1-nodeconf--节点基础配置)
- [2. RpcMonitorConf — RPC 调用监控](#2-rpcmonitorconf--rpc-调用监控)
- [3. EventBusConf — 事件总线](#3-eventbusconf--事件总线)
- [4. NatsConf — NATS 连接配置](#4-natsconf--nats-连接配置)
- [5. DeDuplicatorConf — 消息去重](#5-deduplicatorconf--消息去重)
- [6. TimingWheelConf — 定时器轮](#6-timingwheelconf--定时器轮)
- [7. ClusterConf — 集群配置](#7-clusterconf--集群配置)
- [8. ETCDConf — etcd 连接](#8-etcdconf--etcd-连接)
- [9. RPCServer — RPC 服务器](#9-rpcserver--rpc-服务器)
- [10. DiscoveryConf — 服务发现](#10-discoveryconf--服务发现)
- [11. ServiceConf — 服务列表](#11-serviceconf--服务列表)
- [12. ServiceInitConf — 单个服务配置](#12-serviceinitconf--单个服务配置)
- [13. StopPolicyConf — 停机策略](#13-stoppolicyconf--停机策略)
- [14. MailboxConf — 邮箱配置](#14-mailboxconf--邮箱配置)
- [15. WorkerSchedulePolicy — 调度策略](#15-workerschedulepolicy--调度策略)
- [16. WorkerIdlerConf — 空闲控制](#16-workeridlerconf--空闲控制)
- [17. MultiLevelQueueConf — 多优先级队列](#17-multilevelqueueconf--多优先级队列)
- [18. MailboxMiddlewareConf — 中间件](#18-mailboxmiddlewareconf--中间件)
- [19. RateLimitConf — 限流](#19-ratelimitconf--限流)
- [20. CircuitBreakerConf — 熔断](#20-circuitbreakerconf--熔断)
- [21. ServiceLogConf — 服务日志](#21-servicelogconf--服务日志)
- [22. SystemLogger — 系统日志](#22-systemlogger--系统日志)

---

## 1. NodeConf — 节点基础配置

YAML 路径：`NodeConf.*`

| 字段 | 类型 | 默认值 | 必填 | 说明 |
|------|------|--------|------|------|
| `NodeId` | string | — | ✅ | 节点 ID，用于 PID 文件路径生成 |
| `NodeType` | string | — | ✅ | 节点类型标识 |
| `SystemStatus` | string | — | ✅ | 系统状态：`debug` / `release` |
| `PVCPath` | string | `./data` | ✅ | 持久化数据目录 |
| `PVPath` | string | `./cache` | ✅ | 缓存目录 |
| `AntsPoolSize` | int | 100 | ✅ | 全局 ants 线程池大小（乘以 CPU 核数） |
| `GrpcSenderConnNum` | int | NumCPU/2 | | gRPC sender 每个远端地址的连接数 |
| `BusPoolSize` | int | 10000 | | 消息总线缓存池大小 |

---

## 2. RpcMonitorConf — RPC 调用监控

YAML 路径：`NodeConf.RpcMonitorConf.*`

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `MonitorTimerSize` | int | 10000 | 监控定时器数量 |
| `MonitorBucketSize` | int | 20 | 定时器桶数量（减少锁冲突） |
| `WaitBucketCount` | int | 256 | 等待表分桶数量（建议 2 的幂，非 2 的幂自动向上取整） |
| `WaitBucketInitCap` | int | 自动推导 | 每个桶内 map 初始容量（≤0 时 = MonitorTimerSize / WaitBucketCount，最小 16） |
| `DefaultRpcTimeout` | duration | 1s | RPC 调用默认超时时间（业务层快速失败） |
| `CheckTimeoutInterval` | duration | 1s | 超时检查间隔（建议不大于 DefaultRpcTimeout） |

---

## 3. EventBusConf — 事件总线

YAML 路径：`NodeConf.EventBusConf.*`

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `GlobalPrefix` | string | `event.global.%d` | 全局事件 NATS 主题前缀 |
| `ServerPrefix` | string | `event.server.%d.%d` | 服务级事件 NATS 主题前缀 |
| `SpecificPrefix` | string | `event.specific.%d` | 指定目标事件前缀 |
| `MasterPrefix` | string | `event.master.%s` | 主服务事件前缀（主从同步） |
| `SlavePrefix` | string | `event.slave.%s` | 从服务事件前缀（主从同步） |
| `NodePrefix` | string | `ember.node.` | 节点事件前缀 |
| `ShardCount` | int | 16 | 分段锁数量 |

---

## 4. NatsConf — NATS 连接配置

YAML 路径：`NodeConf.EventBusConf.NatsConf.*`

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `EndPoints` | []string | — | NATS 服务器地址列表 |
| `UserName` | string | — | 用户名 |
| `Password` | string | — | 密码 |
| `Token` | string | — | Token 认证 |
| `Secure` | string | — | 安全模式 |
| `InsecureSkipVerify` | bool | false | 是否跳过服务端证书校验（生产环境不建议开启） |
| `TLSServerName` | string | — | TLS 服务端证书名称（SNI 校验） |
| `Cert` | string | — | 客户端证书路径 |
| `CertKey` | string | — | 客户端证书密钥路径 |
| `CAs` | string | — | CA 证书路径 |
| `MaxReconnects` | int | 5 | 最大重连次数 |
| `ReconnectWait` | duration | 2s | 重连间隔 |
| `Timeout` | duration | 10s | 连接超时 |
| `PingInterval` | duration | 30s | Ping 间隔 |
| `PingMaxOutstanding` | int | 2 | 最大未响应 Ping 数 |
| `ReconnectBufSize` | int | 8MB | 重连缓冲区大小 |
| `SenderPoolSize` | int | 1 | sender 连接池大小 |
| `SubPendingMsgLimit` | int | 200000 | 订阅 pending 最大消息数 |
| `SubPendingBytesLimit` | int | 256MB | 订阅 pending 最大字节数 |

---

## 5. DeDuplicatorConf — 消息去重

YAML 路径：`NodeConf.DeDuplicatorConf.*`

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `DeDuplicatorType` | string | `ttl` | 去重器类型：`ttl`（基于过期时间）/ `lru`（基于容量淘汰） |
| `DeDuplicatorTTL` | duration | 1s | 消息去重 TTL（ttl 模式下生效） |
| `DeDuplicatorCleanTTL` | duration | 3s | 过期条目清理周期 |
| `DeDuplicatorSize` | int | 10000 | 去重容器容量（lru 模式下为最大条目数） |

---

## 6. TimingWheelConf — 定时器轮

YAML 路径：`NodeConf.TimingWheelConf.*`

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `Interval` | duration | 10ms | 定时器精度（tick 间隔） |
| `WheelSize` | int64 | 1000 | 时间轮槽位数 |

---

## 7. ClusterConf — 集群配置

YAML 路径：`ClusterConf.*`

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `EventChannelSize` | int | 1024 | Cluster 事件通道缓冲区大小 |
| `DiscoveryType` | string | `etcd` | 服务发现类型 |
| `RemoteConfPath` | string | — | 远程配置路径（需配合 etcd） |

---

## 8. ETCDConf — etcd 连接

YAML 路径：`ClusterConf.ETCDConf.*`

| 字段 | 类型 | 默认值 | 必填 | 说明 |
|------|------|--------|------|------|
| `EndPoints` | []string | — | ✅ | etcd 地址列表 |
| `DialTimeout` | duration | 3s | | 连接超时 |
| `UserName` | string | — | | etcd 用户名 |
| `Password` | string | — | | etcd 密码 |
| `NoLogger` | bool | false | | 是否禁用 etcd 日志 |

---

## 9. RPCServer — RPC 服务器

YAML 路径：`ClusterConf.RPCServers[*]`

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `Addr` | string | — | 监听地址（如 `0.0.0.0:6611`、`nats://host:4222`） |
| `Protoc` | string | — | 网络协议（`tcp` 等，nats 类型无需配置） |
| `Type` | string | `grpc` | RPC 类型：`grpc` / `rpcx` / `nats` |
| `Cert` | string | — | TLS 证书路径 |
| `CertKey` | string | — | TLS 证书密钥路径 |
| `CAs` | string | — | CA 证书路径 |
| `ReadDeadline` | duration | 30s | 网络层读超时（仅 TCP 类型） |
| `WriteDeadline` | duration | 30s | 网络层写超时（仅 TCP 类型） |

---

## 10. DiscoveryConf — 服务发现

YAML 路径：`ClusterConf.DiscoveryConf.*`

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `Path` | string | — | 服务发现注册路径（如 `/ember/rpc`） |
| `TTL` | int64 | 3 | 租约 TTL（秒） |
| `MasterPath` | string | — | 主从选举路径（如 `/ember/master`） |

### RecoveryConf — 故障恢复

YAML 路径：`ClusterConf.DiscoveryConf.RecoveryConf.*`

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `BackoffBaseDelay` | duration | 1s | 退避基础延迟 |
| `BackoffMaxDelay` | duration | 30s | 退避最大延迟 |
| `VerboseLogCount` | int | 5 | 前 N 次每次输出详细日志 |
| `LogInterval` | int | 10 | 之后每 N 次输出一次日志 |

---

## 11. ServiceConf — 服务列表

YAML 路径：`ServiceConf.*`

| 字段 | 类型 | 默认值 | 必填 | 说明 |
|------|------|--------|------|------|
| `OpenRemote` | bool | false | | 是否开启远程配置 |
| `RemoteConfPath` | string | — | | 远程配置路径 |
| `StartServices` | []ServiceInitConf | — | ✅ | 启动服务列表（按配置顺序启动） |
| `ServicesConfMap` | map | — | ✅ | 服务配置映射 [服务名] → 配置 |

---

## 12. ServiceInitConf — 单个服务配置

YAML 路径：`ServiceConf.StartServices[*]`

| 字段 | 类型 | 默认值 | 必填 | 说明 |
|------|------|--------|------|------|
| `ClassName` | string | — | ✅ | 服务类名（对应 `services.SetService` 注册的 key） |
| `ServiceId` | string | — | | 服务唯一 ID（全局唯一服务可为空） |
| `ServiceName` | string | — | | 服务名称（RPC 调用时使用） |
| `Type` | string | — | ✅ | 服务类型标识 |
| `Version` | int64 | 0 | | 服务版本号 |
| `Partition` | int32 | — | ✅ | 分区 ID（用于服务路由隔离） |
| `RpcType` | string | `local` | | 远程调用方式：`local` / `grpc` / `rpcx` / `nats` |
| `IsPrimarySecondaryMode` | bool | false | | 是否启用主从模式 |
| `EventChanSize` | int | 100 | | 事件通道大小 |

---

## 13. StopPolicyConf — 停机策略

YAML 路径：`ServiceConf.StartServices[*].StopPolicy.*`

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `GraceTimeout` | duration | 0 | 优雅窗口（期间 mailbox 挂起，仅放行 RPC Reply / 紧急消息） |
| `DrainPolicy` | string | `execute` | 停机时队列处理方式：`execute`（继续执行）/ `discard`（丢弃） |

---

## 14. MailboxConf — 邮箱配置

YAML 路径：`ServiceConf.StartServices[*].Mailbox.*`

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `QueueMode` | string | `dual` | 队列模式：`dual`（双队列）/ `priority`（多优先级队列） |
| `EnableRWMode` | bool | false | 启用读写分离（ReadOnly RPC 并发 RLock，写操作独占 WLock） |
| `MaxConcurrentReads` | int | min(NumCPU×4, 64) | 最大并发读数（仅 RW 模式） |
| `StopTimeout` | duration | 10s | RW 模式 Worker Stop 最大等待时间 |
| `MaxJobExecutionTime` | duration | 30s | 单个 Job 最大执行时长（watchdog 告警，不中断；0 = 不启用） |
| `ReadPoolSize` | int | MaxConcurrentReads | 读 goroutine 池容量（仅 RW 模式） |
| `ReadDispatchChanCap` | int | max(MaxConcurrentReads, 256) | 每个 Worker 的读派发通道容量 |

---

## 15. WorkerSchedulePolicy — 调度策略

YAML 路径：`ServiceConf.StartServices[*].Mailbox.SchedulePolicy.*`

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `InitialWorkerNum` | int32 | 1 | 初始 Worker 数量（=1 时为单线程 Actor 模型） |
| `VirtualWorkerRate` | int | — | **已废弃**：任何值都被忽略 |
| `EnableAutoScaling` | bool | false | 是否启用自动扩缩容 |

### ScalingStrategy — 扩缩容策略

YAML 路径：`...SchedulePolicy.ScalingStrategy.*`

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `Name` | string | — | 策略名称：`max_load` / `cpu` / `composite` |
| `MinWorkerNum` | int32 | 1 | 最小 Worker 数 |
| `MaxWorkerNum` | int32 | 100 | 最大 Worker 数 |
| `GrowthFactor` | float64 | 1.5 | 扩容因子（新数量 = 当前 × 因子） |
| `ShrinkFactor` | float64 | 0.5 | 缩容因子 |
| `ResizeCoolDown` | duration | 1s | 扩缩容冷却时间 |
| `Params` | map | — | 策略参数（composite 需设 `Mode: "all"/"any"`） |
| `Subs` | []ScalingStrategy | — | 子策略列表（仅 composite） |

**max_load 参数**：
| 参数 | 说明 |
|------|------|
| `IdleThreshold` | 空闲 Worker 比例阈值（超过时触发缩容） |
| `MaxLoadThreshold` | 单个 Worker 最大负载阈值（超过时触发扩容） |

**cpu 参数**：
| 参数 | 说明 |
|------|------|
| `MinLoadThreshold` | CPU 低负载阈值（低于时缩容） |
| `MaxLoadThreshold` | CPU 高负载阈值（高于时扩容） |

---

## 16. WorkerIdlerConf — 空闲控制

YAML 路径：`...SchedulePolicy.IdlerConf.*`

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `EnableCond` | bool | false | 是否使用条件变量等待（true: 节省 CPU，唤醒有开销；false: 指数退避睡眠） |
| `BackoffBaseDelay` | duration | 1µs | 退避基础延迟 |
| `BackoffMaxDelay` | duration | 16µs | 退避最大延迟 |
| `BackoffMaxRetries` | int | 3 | 退避最大重试次数 |
| `MaxIdleBeforeBackoff` | int | 1000 | 开始退避前的空闲次数（前 N 次直接重试） |

---

## 17. MultiLevelQueueConf — 多优先级队列

YAML 路径：`...SchedulePolicy.MultiLevelQueueConf.*`

仅在 `QueueMode="priority"` 时生效。

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `Strategy` | string | `absolute` | 调度策略：`absolute`（严格优先）/ `weighted`（加权）/ `fairness`（公平） |
| `TotalBatchLimit` | int | 32 | 单次循环最多处理的消息总数 |

### PriorityBatches — 各优先级配置

| 优先级 | 数值 | 建议 BatchSize | 建议 Weight |
|--------|------|----------------|-------------|
| PrioritySys | -3 | 32 | 20 |
| PriorityUrgent | -2 | 16 | 10 |
| PriorityHigh | -1 | 12 | 5 |
| PriorityNormal | 0 | 8 | 3 |
| PriorityLow | 1 | 4 | 2 |
| PriorityBatch | 2 | 2 | 1 |

---

## 18. MailboxMiddlewareConf — 中间件

YAML 路径：`...Mailbox.MiddlewareConf.*`

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `EnableDispatchKeyStats` | bool | true (debug) | 启用 DispatchKey 统计中间件 |
| `DispatchKeyStatsInterval` | duration | 10s | 统计输出间隔 |
| `DispatchKeyStatsTopN` | int | 10 | 输出 TopN 热点 key |
| `DispatchKeyStatsMaxKeys` | int | 100000 | 最大跟踪 key 数量 |

---

## 19. RateLimitConf — 限流

YAML 路径：`...MiddlewareConf.RateLimitConf.*`

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `Enable` | bool | false | 是否启用限流 |
| `Rate` | float64 | 10000 | 每秒允许的请求数 |
| `Burst` | int | 1000 | 突发流量上限（令牌桶容量） |
| `SkipUrgent` | bool | true | 是否跳过紧急及以上优先级的消息 |

---

## 20. CircuitBreakerConf — 熔断

YAML 路径：`...MiddlewareConf.CircuitBreakerConf.*`

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `Enable` | bool | false | 是否启用熔断 |
| `FailureThreshold` | int | 5 | 触发熔断的连续失败次数 |
| `SuccessThreshold` | int | 3 | 半开状态恢复所需的连续成功次数 |
| `CooldownDuration` | duration | 30s | 熔断冷却时间 |
| `WindowDuration` | duration | 60s | 统计窗口时间 |
| `HalfOpenMaxAllowed` | int | 3 | 半开状态允许的最大探测请求数 |

---

## 21. ServiceLogConf — 服务日志

YAML 路径：`ServiceConf.StartServices[*].LogConf.*`

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `Enable` | bool | false | 是否启用独立日志（否则使用系统日志器） |

当 `Enable=true` 时，`Config` 字段使用与 SystemLogger 相同的日志配置结构。

---

## 22. SystemLogger — 系统日志

YAML 路径：`SystemLogger.*`

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `Dir` | string | — | 日志目录 |
| `PrefixName` | string | `app` | 日志文件前缀名 |
| `Level` | string | `debug` | 日志级别：`trace` / `debug` / `info` / `warn` / `error` / `fatal` / `panic` |
| `OutputFormat` | string | `text` | 输出格式：`text` / `json` |
| `Stdout` | bool | false | 是否输出到标准输出 |
| `Caller` | bool | false | 是否打印调用文件信息 |
| `FullCaller` | bool | false | 是否打印完整路径（true = 绝对路径） |
| `Color` | bool | false | 是否显示颜色 |

### Rotation — 日志切割

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `MaxAge` | duration | 720h (30天) | 日志最大保存时间 |
| `Every` | duration | 24h | 日志切分周期 |
| `Pattern` | string | 自动推导 | 文件切分精度（如 `%Y%m%d`） |

### Routing — 日志路由

| 字段 | 说明 |
|------|------|
| `AsyncMode.Enable` | 是否开启异步写入模式 |
| `AsyncMode.Config.FlushInterval` | 异步刷新间隔（默认 1s） |
| `AsyncMode.Config.BufferSize` | 异步缓冲区大小（默认 1MB） |
| `Routes[*].Name` | 路由文件名（如 `access`、`error`） |
| `Routes[*].Levels` | 该路由包含的日志级别列表 |

---

## 附录：最小配置示例

```yaml
NodeConf:
  NodeId: 1
  NodeType: game
  SystemStatus: debug
  PVCPath: ./data
  PVPath: ./cache
  AntsPoolSize: 1

ServiceConf:
  StartServices:
    - ClassName: MyService
      ServiceName: my-svc
      Type: game
      Partition: 1

SystemLogger:
  Dir: ./data/logs
  PrefixName: app
  Level: debug
  Stdout: true
```

> 以上为无集群模式的最小配置，适用于本地开发和单节点测试。  
> 集群模式需额外配置 `ClusterConf`（etcd + RPCServers）。

---

## 附录：配置文件模板

| 模板文件 | 说明 |
|----------|------|
| `template/config/node.yaml` | 完整节点配置（含所有字段和注释） |
| `template/config/db.yaml` | 数据库配置（MySQL / Redis） |
| `template/config/gate.yaml` | WebSocket 网关配置 |
| `template/config/pprof.yaml` | 性能诊断服务配置 |
