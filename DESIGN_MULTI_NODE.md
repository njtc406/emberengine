# EmberEngine Node 环境自包含改造设计文档

> **目标**: 将当前分散在各个包中的全局/单例组件**全部收归到 `Node` 结构体**中管理，使 `Node` 成为完全自包含的运行时环境。
>
> **核心价值**:
> 1. **可嵌入任意项目**: 框架不再通过包级全局变量污染宿主进程。任何 Go 项目只需 `node.New().Start(...)` 即可集成 EmberEngine，框架的配置、日志、协程池、时间轮、RPC 等所有运行时状态均封装在 `Node` 实例内部，不会与宿主项目的日志系统、协程模型、全局状态产生任何冲突。
> 2. **单进程多 Node**: 同一进程可启动多个独立 `Node` 实例（如模拟分布式集群、跑集成测试），各 Node 的资源、生命周期、服务路由完全隔离。
> 3. **干净的生命周期**: `Node.Start()` 创建一切，`Node.Stop()` 销毁一切。启动失败自动回滚，停止后零残留（无泄漏的 goroutine、连接、文件句柄）。

## 一、现状分析

### 1.1 当前 Node 结构体

```go
type Node struct {
    version   string
    confPath  string
    hooks     []HookFun
    extra     map[any]any
    startTime time.Time
}
```

当前 `Node` 只是一个轻量启动器，几乎不持有任何运行时状态。所有核心组件均以 **包级全局变量 + sync.Once/nil 检查** 的单例模式存在，整个进程共享同一份实例。

> **改造目标**: 去除所有包级可变全局变量，不保留任何兼容写法，彻底完成一步到位的改造。使 `Node` 成为唯一的运行时上下文持有者——**框架的一切可变状态都在 `Node` 内部，外部无感知**。

### 1.2 启动流程中的全局组件调用链

```
Node.Start()
  ├── config.Init()                    → 全局 Conf 单例
  ├── log.Init()                       → 全局 SysLogger 单例
  ├── asynclib.InitAntsPool()          → 全局 antsPool 单例
  ├── timingwheel.Start()              → 全局 globTW 单例
  ├── monitor.GetRpcMonitor().Init()   → 全局 rpcMonitor (sync.Once)
  ├── dedup.Init()                     → 全局 duplicator 单例
  ├── monitor.GetRpcMonitor().Start()
  ├── cluster.GetCluster().Init()      → 全局 cluster 值变量
  │   └── endpoints.GetEndpointManager().Init()  → 全局 endMgr 指针
  ├── event.GetEventBus().Init()       → 全局 bus (sync.Once)
  ├── services.Init()                  → 全局 serviceMap + runServices
  └── services.Start()
```

### 1.3 全局状态的危害

当框架被嵌入第三方项目时，包级全局状态会产生以下冲突：

| 冲突类型 | 说明 |
|----------|------|
| **宿主污染** | 框架的全局 logger、协程池、时间轮等在 `import` 时即占用进程资源，即使尚未调用 `Start()` |
| **配置覆盖** | 所有 Node 共享 `config.Conf`，NodeId/端口等节点级参数无法独立；宿主项目若也使用 viper 则可能键名冲突 |
| **资源共享** | 时间轮、协程池、RPC 监控器、事件总线等为全局唯一，无法按 Node 独立配置 |
| **关闭级联** | 任一 Node Stop 会销毁全局资源，导致其他 Node 崩溃；宿主进程的 graceful shutdown 也可能被干扰 |
| **服务路由污染** | 所有 Node 的服务注册到同一个 Repository，PID 路由完全混乱 |
| **日志混杂** | 所有 Node 共享一个 SysLogger，日志无法按 Node 区分，且可能与宿主项目自身的日志系统冲突 |
| **init() 副作用** | `sysService`、`discovery` 等包的 `init()` 在 import 时自动执行，宿主项目仅 import 框架包就会触发注册逻辑 |

---

## 二、改进总览

### 2.1 新的 Node 结构体（目标形态）

```go
type Node struct {
    // 基本信息
    version   string
    confPath  string
    hooks     []HookFun
    extra     map[any]any
    startTime time.Time
    
    // ====== 以下为从全局收归的组件 ======
    
    // 日志（原 log.SysLogger）— 嵌入式
    *log.Logger
    
    // 配置（原 config.Conf）
    Config       *config.Config
    
    // 协程池（原 asynclib.antsPool）
    AntsPool     *asynclib.Pool
    
    // 时间轮（原 timingwheel.globTW）
    TimingWheel  *timingwheel.TimingWheel
    
    // RPC 监控器（原 monitor.rpcMonitor）
    RpcMonitor   *monitor.RpcMonitor
    
    // 去重器（原 dedup.duplicator）
    DeDuplicator inf.IDeDuplicator
    
    // 集群管理器（原 cluster.cluster）
    Cluster      *cluster.Cluster
    
    // 端点管理器（原 endpoints.endMgr）
    // 已被 Cluster 内部持有，通过 Cluster.GetEndpointManager() 访问
    
    // 事件总线（原 event.bus）
    EventBus     *event.Bus
    
    // 服务管理器（原 services 包级变量）
    ServiceMgr   *services.ServiceManager
    
    // RPC 连接池管理器（原 pool.globalPoolManager）
    PoolManager  *pool.PoolManager
    
    // RPC Sender 管理器（原 client.senderHandlerMap）
    SenderMgr    *client.SenderManager
    
    // 路由器（原 router 包级函数）
    Router       *router.Router
    
    // Profiler 注册表（原 profiler.mapProfiler）
    Profiler     *profiler.Registry
    
    // 插件注册表（原 plugins.pluginMap）
    PluginMgr    *plugins.PluginManager
    
    // 时间偏移量（原 timelib.timeOffset）
    TimeOffset   time.Duration
    
    // 连接池统计（原 pool.poolStates）
    PoolStats    *pool.PoolStats
    
    // 停止标志（防止 Stop() 重复调用）
    stopped      atomic.Bool
}
```

### 2.2 核心设计原则

1. **Node 即完整运行时**: `Node` 是框架面向外部的唯一入口。创建一个 `Node` = 创建一个完全独立的 EmberEngine 运行时环境，不依赖任何包级可变状态，不对宿主进程产生任何副作用
2. **NodeContext 模式**: 引入 `NodeContext` 接口/结构体，作为所有组件访问 Node 级资源的统一入口
3. **依赖注入**: 各组件不再通过包级函数获取依赖，而是通过构造函数或 Init 方法接收 `NodeContext`
4. **生命周期绑定**: 每个组件的创建和销毁都跟随其所属的 Node
5. **彻底去除包级类型**: 删除所有包级全局变量、`sync.Once` 单例、`GetXxx()` 全局 getter、`init()` 中的注册逻辑。不保留任何兼容写法，一步到位完成改造
6. **错误透传，禁止 panic/fatal**: 原来包级初始化函数出错时多采用 `log.Fatal()` 或 `panic()` 直接终止进程。改造后所有组件的 `New*()`/`Init()`/`Start()` **必须返回 `error`**，由调用方（`Node.Start()`）统一决定是否终止。`Node.Start()` 内部通过 `defer` 回滚机制保证：任一组件初始化失败时，已成功创建的组件按逆序安全关闭，不留泄漏

### 2.3 改造后的 Node.Start() / Node.Stop() 流程

**Node.Start()** — 改造后的完整创建与启动顺序:

> **关键改动**: 所有组件的 `New*()`/`Init()`/`Start()` 均返回 `error`（不再 panic/fatal）。
> `Start()` 使用 `cleanups` 栈 + `defer` 实现**启动失败自动回滚**：任一步骤出错时，
> 已成功初始化的组件按逆序安全关闭，不留 goroutine/连接/文件泄漏。

```go
func (n *Node) Start(opts ...StartOption) (retNode *Node, retErr error) {
    // ── cleanups 栈：记录已完成的初始化步骤，失败时逆序回滚 ──
    var cleanups []func()
    defer func() {
        if retErr != nil {
            for i := len(cleanups) - 1; i >= 0; i-- {
                cleanups[i]()
            }
        }
    }()

    // 0. 应用选项
    param := &StartParam{}
    for _, opt := range opts {
        opt(param)
    }
    n.confPath = param.ConfPath
    n.version = param.Version
    n.hooks = param.Hooks
    n.extra = param.Extra

    // 0.1 语言设置（全局共享，仅第一个 Node 的设置生效）
    if param.Language > 0 {
        translate.SetLanguage(param.Language)
    }

    // 0.2 打印版本信息（多 Node 场景下仅首次调用有意义，重复调用无害）
    title.EchoTitle(n.version)

    // 1. 配置（最先初始化 — 其他一切依赖配置）
    n.Config = config.NewConfig()
    if err := n.Config.Load(n.confPath); err != nil {
        return nil, fmt.Errorf("config load: %w", err)
    }

    // 2. 日志（第二个初始化 — 后续组件需要日志）
    var err error
    n.Logger, err = log.NewLogger(n.Config.SystemLogger, n.Config.IsDebug())
    if err != nil {
        return nil, fmt.Errorf("logger init: %w", err)
    }
    cleanups = append(cleanups, func() { n.Logger.Close() })

    // 3. 基础设施层
    n.AntsPool, err = asynclib.NewPool(n.Config.NodeConf.AntsPoolSize)
    if err != nil {
        return nil, fmt.Errorf("ants pool: %w", err)
    }
    cleanups = append(cleanups, func() { n.AntsPool.Release() })

    n.TimingWheel, err = timingwheel.NewTimingWheel(
        time.Duration(n.Config.NodeConf.TimerWheelInterval) * time.Millisecond,
        int64(n.Config.NodeConf.TimerWheelSize),
        n.Logger,
    )
    if err != nil {
        return nil, fmt.Errorf("timing wheel: %w", err)
    }
    n.TimingWheel.Start()
    cleanups = append(cleanups, func() { n.TimingWheel.Stop() })

    n.DeDuplicator, err = dedup.NewDeDuplicator(n.Config.NodeConf.DeDuplicatorConf)
    if err != nil {
        return nil, fmt.Errorf("dedup: %w", err)
    }
    cleanups = append(cleanups, func() { n.DeDuplicator.Close() })

    // 3.5 连接池统计 & 时间偏移
    n.PoolStats = pool.NewPoolStats()
    n.TimeOffset = n.Config.NodeConf.TimeOffset   // 默认 0；运行时可通过 Node.SetTimeOffset() 动态调整

    // 4. RPC 层
    n.RpcMonitor = monitor.NewRpcMonitor()
    if err = n.RpcMonitor.Init(n.Config.NodeConf.RpcMonitorConf, n.TimingWheel, n.Logger); err != nil {
        return nil, fmt.Errorf("rpc monitor init: %w", err)
    }
    n.PoolManager = pool.NewPoolManager()
    n.SenderMgr, err = client.NewSenderManager(n.PoolManager, n.Logger)
    if err != nil {
        return nil, fmt.Errorf("sender manager: %w", err)
    }
    cleanups = append(cleanups, func() { n.SenderMgr.Close(); n.PoolManager.Close() })
    n.RpcMonitor.Start()
    cleanups = append(cleanups, func() { n.RpcMonitor.Stop() })

    // 5. PID 文件
    utils.RecordPID(n.Config.NodeConf.PvPath, n.Config.NodeConf.NodeId, n.Config.NodeConf.NodeType)
    cleanups = append(cleanups, func() {
        utils.DeletePID(n.Config.NodeConf.PvPath, n.Config.NodeConf.NodeId, n.Config.NodeConf.NodeType)
    })

    // 6. 事件总线 & 集群层
    n.EventBus = event.NewEventBus()
    if err = n.EventBus.Init(n.Config.NodeConf.EventBusConf); err != nil {
        return nil, fmt.Errorf("event bus init: %w", err)
    }
    cleanups = append(cleanups, func() { n.EventBus.Close() })

    n.Cluster = cluster.NewCluster()
    if err = n.Cluster.Init(n); err != nil {
        return nil, fmt.Errorf("cluster init: %w", err)
    }
    if err = n.Cluster.Start(); err != nil {
        return nil, fmt.Errorf("cluster start: %w", err)
    }
    cleanups = append(cleanups, func() { n.Cluster.Close() })

    // 7. 路由 & Profiler
    n.Router = router.NewRouter(n.Cluster.GetEndpointManager())
    n.Profiler = profiler.NewRegistry()
    n.PluginMgr = plugins.NewPluginManager()

    // 8. 用户钩子（签名改为 func(INodeContext, map[any]any) error）
    for _, hook := range n.hooks {
        if err = hook(n, n.extra); err != nil {
            return nil, fmt.Errorf("hook: %w", err)
        }
    }

    // 9. 服务层（最后启动 — 依赖以上所有组件）
    n.ServiceMgr = services.NewServiceManager(n)
    pprofservice.RegisterPprofService(n.ServiceMgr)
    dbservice.RegisterDBService(n.ServiceMgr)
    if err = n.ServiceMgr.Init(); err != nil {
        return nil, fmt.Errorf("service init: %w", err)
    }
    if err = n.ServiceMgr.Start(); err != nil {
        return nil, fmt.Errorf("service start: %w", err)
    }
    cleanups = append(cleanups, func() { n.ServiceMgr.StopAll() })

    n.startTime = time.Now()
    return n, nil
}
```

**Node.Stop()** — 改造后的完整停止顺序（与启动相反）:

```go
func (n *Node) Stop() {
    // 幂等保护：防止 signal handler + defer + 手动调用 导致重复执行
    if !n.stopped.CompareAndSwap(false, true) {
        return
    }

    defer utils.DeletePID(n.Config.NodeConf.PvPath, n.Config.NodeConf.NodeId, n.Config.NodeConf.NodeType)

    n.Info("==================>>begin stop<<==================")

    // 1. 停止所有服务（逆序）
    n.Info("[1/6] Stopping all services...")
    n.ServiceMgr.StopAll()
    n.Info("[1/6] All services stopped")

    // 2. 关闭集群 & 事件总线
    n.Info("[2/6] Closing cluster & event bus...")
    n.Cluster.Close()
    n.EventBus.Close()
    n.Info("[2/6] Cluster & event bus closed")

    // 3. 停止 RPC 监控
    n.Info("[3/6] Stopping RPC monitor...")
    n.RpcMonitor.Stop()
    n.Info("[3/6] RPC monitor stopped")

    // 4. 关闭 RPC 连接
    n.Info("[4/6] Closing RPC connections...")
    n.SenderMgr.Close()
    n.PoolManager.Close()
    n.Info("[4/6] RPC connections closed")

    // 5. 停止基础设施
    n.Info("[5/6] Stopping timing wheel, dedup & releasing pool...")
    n.DeDuplicator.Close()
    n.TimingWheel.Stop()
    n.AntsPool.Release()
    n.Info("[5/6] Infrastructure stopped")

    // 6. 关闭日志（最后关闭 — 确保以上步骤的日志都能输出）
    n.Info("[6/6] Node stopped, closing logger...")
    n.Logger.Close()

    // 7. 优雅退出标题
    title.GracefulExit(time.Since(n.startTime), n.version)
}
```

> **关键对比**: 改造前所有 `Stop()` 调用的是包级函数（如 `services.StopAll()`、`cluster.Close()`），
> 改造后全部变为 Node 实例方法调用。每个 Node 的 Stop 只影响自己的组件，不会波及其他 Node。
>
> **启动失败回滚**: `Start()` 中任一步骤返回 error，`defer` 中的 `cleanups` 栈会逆序执行所有已注册的清理函数，
> 确保不会出现"日志已打开但时间轮未关闭"之类的半初始化泄漏。

---

## 三、各组件详细改进方案

---

### 3.1 config 包 — 配置系统

#### 当前全局状态

```go
// config/init.go
var (
    runtimeViper = viper.New()
    clusterViper = viper.New()
    Conf         = new(conf)    // ← 导出的全局指针
)

// config/confMap.go
var (
    serviceConfMap = make(map[string]*ServiceConfig)   // 服务配置注册表
    discoveryConf  = make(map[string]interface{})       // 发现配置
)
```

#### 问题

- `Conf` 是导出的全局指针，所有包直接通过 `config.Conf.XXX` 读取配置
- `serviceConfMap` 是全局 map，所有服务配置混在一起
- 多个 Node 需要不同的 NodeId、端口、日志路径等配置

#### 改进方案

**步骤 1**: 将 `conf` 结构体改为可实例化

```go
// config/config.go
type Config struct {
    runtimeViper *viper.Viper
    clusterViper *viper.Viper
    
    NodeConf     *NodeConf
    SystemLogger *log.LoggerConf
    ClusterConf  *ClusterConf
    ServiceConf  *ServiceConf
    
    serviceConfMap map[string]*ServiceConfig
    discoveryConf  map[string]interface{}
}

func NewConfig() *Config {
    return &Config{
        runtimeViper:   viper.New(),
        clusterViper:   viper.New(),
        serviceConfMap: make(map[string]*ServiceConfig),
        discoveryConf:  make(map[string]interface{}),
    }
}

func (c *Config) Load(confPath string) error {
    // 原 Init() 的逻辑迁移至此
}
```

**步骤 2**: 删除 `config/init.go` 中的所有包级 `var` 声明（`Conf`、`runtimeViper`、`clusterViper`）

**步骤 3**: 删除包级函数 `Init()`、`IsDebug()`、`SetStatus()`、`GetDefaultRpcTimeout()` 等，全部改为 `Config` 的实例方法

**步骤 4**: 删除 `confMap.go` 中的包级 `serviceConfMap`、`discoveryConf`，收归到 `Config` 结构体内

**步骤 5**: 所有读取 `config.Conf` 的地方改为从 `NodeContext` 获取

```go
// 改前
cluster.Init() {
    conf := config.Conf.ClusterConf
}

// 改后
cluster.Init(ctx NodeContext) {
    conf := ctx.Config().ClusterConf
}
```

**删除清单**:
- `var Conf = new(conf)` — 删除
- `var runtimeViper = viper.New()` — 删除
- `var clusterViper = viper.New()` — 删除
- `var serviceConfMap = make(...)` — 删除
- `var discoveryConf = make(...)` — 删除
- `func Init(confPath string)` — 删除，改为 `(c *Config) Load(confPath string)`
- `func IsDebug() bool` — 删除，改为 `(c *Config) IsDebug() bool`

**影响范围**: 几乎所有包都引用了 `config.Conf`，这是改动量最大的一项。

**建议优先级**: ⭐⭐⭐⭐⭐（最高 — 所有其他组件都依赖配置）

---

### 3.2 log 包 — 日志系统

#### 当前全局状态

```go
// log/init.go
var SysLogger *Logger   // 导出的全局 logger

func Init(conf *LoggerConf, isDebug bool) {
    if SysLogger != nil { return }  // 只初始化一次
    // ...
    SysLogger = logger
}

func Close() {
    if SysLogger != nil {
        Release(SysLogger)
        SysLogger = nil
    }
}
```

#### 问题

- `SysLogger` 是导出的全局变量，几乎所有包都直接使用 `log.SysLogger.Info(...)`
- `nil` 检查防止重复初始化，第二个 Node 无法创建自己的 logger
- `Close()` 将 `SysLogger` 置 nil，导致其他 Node 的日志调用 panic

#### 改进方案

**步骤 1**: Logger 创建函数化

```go
// log/factory.go
func NewLogger(conf *LoggerConf, isDebug bool) (*Logger, error) {
    conf = fixConf(conf)
    conf.Stdout = conf.Stdout || isDebug
    return NewDefaultLogger(conf)
}
```

**步骤 2**: 每个 Node 持有自己的 Logger

```go
type Node struct {
    Logger *log.Logger
    // ...
}

func (n *Node) Start(opts ...StartOption) (*Node, error) {
    n.Logger, _ = log.NewLogger(n.Config.SystemLogger, n.Config.IsDebug())
    // ...
}
```

**步骤 3**: 删除 `log/init.go` 中的所有包级变量和函数

**删除清单**:
- `var SysLogger *Logger` — 删除
- `func Init(conf *LoggerConf, isDebug bool)` — 删除
- `func Close()` — 删除，改为 `(l *Logger) Close()`

**步骤 4**: 各组件通过 **嵌入 `*log.Logger`** 的方式获得日志能力，使用 `s.Info()` 直接调用

```go
// 改前
log.SysLogger.Info("something happened")

// 改后: Logger 嵌入到结构体中
type Service struct {
    *log.Logger            // 嵌入 Logger
    nodeCtx INodeContext
    // ...
}

// 使用时直接调用
s.Info("something happened")
s.Warnf("timeout: %v", err)
```

**嵌入策略**:

| 组件层级 | 嵌入方式 |
|----------|----------|
| `Node` | 直接持有 `*log.Logger` |
| `Cluster` | 构造时接收并嵌入 `*log.Logger` |
| `Service` | 嵌入 `*log.Logger`，由 `ServiceManager` 在 Init 时注入 |
| `RpcMonitor` | 构造时接收并嵌入 `*log.Logger` |
| 其他组件 | 通过 `NodeContext.Logger()` 获取后嵌入 |

> **注意**: 非结构体的工具函数（如 `timingwheel` 内部）通过构造函数参数传入 `*log.Logger` 并存为字段。

**影响范围**: 极广，几乎每个 .go 文件都引用了 `log.SysLogger`。

**建议优先级**: ⭐⭐⭐⭐⭐（与 config 并列最高）

---

### 3.3 utils/asynclib — 协程池

#### 当前全局状态

```go
// utils/asynclib/asyncgo.go
var antsPool *ants.Pool

func InitAntsPool(size int) {
    if antsPool == nil && size > 0 {
        antsPool = NewAntsPool(size, ants.WithPreAlloc(true))
    }
}

func Go(f func()) error {
    return antsPool.Submit(f)
}

func Release() {
    if antsPool != nil {
        antsPool.Release()
    }
}
```

#### 问题

- 全局唯一协程池，`nil` 检查防重复初始化
- `Release()` 后所有 `Go()` 调用失败
- 不同 Node 无法配置不同的池大小

#### 改进方案

**步骤 1**: 提供工厂函数，返回独立的 Pool 包装器

```go
// utils/asynclib/pool.go
type Pool struct {
    inner *ants.Pool
}

func NewPool(size int) *Pool {
    return &Pool{
        inner: NewAntsPool(size, ants.WithPreAlloc(true)),
    }
}

func (p *Pool) Go(f func()) error {
    return p.inner.Submit(f)
}

func (p *Pool) Release() {
    if p.inner != nil {
        p.inner.Release()
    }
}
```

**步骤 2**: Node 持有独立的 Pool

```go
n.AntsPool = asynclib.NewPool(n.Config.NodeConf.AntsPoolSize)
```

**步骤 3**: 删除包级变量和函数

**删除清单**:
- `var antsPool *ants.Pool` — 删除
- `func InitAntsPool(size int)` — 删除
- `func Go(f func()) error` — 删除
- `func Release()` — 删除

**影响范围**: 所有调用 `asynclib.Go()` 的地方，改为通过 `NodeContext` 获取 `Pool` 实例后调用 `pool.Go()`。

**建议优先级**: ⭐⭐⭐⭐

---

### 3.4 utils/timingwheel — 时间轮

#### 当前全局状态

```go
// utils/timingwheel/init.go
var (
    globTW     *TimingWheel
    twMutex    sync.Mutex
    cronParser Parser
)

func Start(interval time.Duration, wheelSize int64, logger *log.Logger) {
    twMutex.Lock()
    defer twMutex.Unlock()
    if globTW != nil { return }  // 只启动一次
    globTW = NewTimingWheel(interval, wheelSize, ...)
    globTW.Start()
}

func Stop() {
    twMutex.Lock()
    defer twMutex.Unlock()
    if globTW != nil {
        globTW.Stop()
        globTW = nil
    }
}

func GetTimingWheel() *TimingWheel { return globTW }
```

另有 `cronParser` 全局解析器和 `init()` 初始化。

#### 问题

- 全局唯一时间轮，`nil` 检查防重复启动
- `Stop()` 将 `globTW` 置 nil，所有依赖定时器的组件（服务 ticker、RPC 超时等）全部失效
- 所有服务的定时任务注册在同一个时间轮上

#### 改进方案

**步骤 1**: 时间轮实例化

```go
// timingwheel 包已有 NewTimingWheel()，只需暴露
// Node 直接持有实例
n.TimingWheel = timingwheel.NewTimingWheel(interval, wheelSize, logger)
n.TimingWheel.Start()
```

**步骤 2**: 删除所有全局变量和全局函数

**删除清单**:
- `var globTW *TimingWheel` — 删除
- `var twMutex sync.Mutex` — 删除
- `func Start(...)` — 删除
- `func Stop()` — 删除
- `func GetTimingWheel()` — 删除
- `func SetTimeOffset(...)` — 删除，改为 `(tw *TimingWheel) SetTimeOffset(offset time.Duration)`

> `cronParser` 和 `init()` 中的解析器初始化可保留为包级变量（无状态的只读解析器，不影响多 Node）。

**影响范围**: `monitor`（`NewJobScheduler` 使用时间轮）、所有 `Service`（`ITimerScheduler`）、mailbox 调度等。

**建议优先级**: ⭐⭐⭐⭐

---

### 3.5 monitor 包 — RPC 监控器

#### 当前全局状态

```go
// monitor/monitor.go
var rpcMonitor *RpcMonitor
var monitorOnce sync.Once

func GetRpcMonitor() *RpcMonitor {
    monitorOnce.Do(func() {
        rpcMonitor = &RpcMonitor{}
    })
    return rpcMonitor
}
```

`RpcMonitor` 持有:
- `ctx/cancel` — 控制生命周期
- `epoch/seq` — RPC 序列号生成器
- `buckets` — 等待中的 RPC 调用状态
- `sd` — 定时器调度器（依赖时间轮）

#### 问题

- `sync.Once` 保证全进程唯一
- 所有 Node 共享 `seq` 生成器和 `waitBucket`
- `Stop()` 关闭 `ctx`，所有 Node 的 RPC 等待全部超时失败

#### 改进方案

**步骤 1**: 删除 `sync.Once` 单例，改为直接构造

**删除清单**:
- `var rpcMonitor *RpcMonitor` — 删除
- `var monitorOnce sync.Once` — 删除
- `func GetRpcMonitor() *RpcMonitor` — 删除

**新增**:
```go
func NewRpcMonitor() *RpcMonitor {
    return &RpcMonitor{}
}

// Node 中
n.RpcMonitor = monitor.NewRpcMonitor()
n.RpcMonitor.Init(n.Config.NodeConf.RpcMonitorConf, n.TimingWheel)
n.RpcMonitor.Start()
```

**步骤 2**: `RpcMonitor` 嵌入 `*log.Logger`，定时器调度器从 Node 的时间轮获取

```go
type RpcMonitor struct {
    *log.Logger                          // 嵌入 Logger
    // ...
}

func (rm *RpcMonitor) Init(conf *config.RpcMonitorConf, tw *timingwheel.TimingWheel) {
    rm.sd = tw.NewScheduler()
    // ...
}
```

**步骤 3**: 同样处理 `monitor` 包内的 `DumpMonitor` 单例（如有）

**影响范围**: `core/rpc`（大量 `GenSeq`/`Add`/`Remove` 调用）、`node.go`。所有 `monitor.GetRpcMonitor()` 调用改为通过 `NodeContext` 获取。

**建议优先级**: ⭐⭐⭐⭐

---

### 3.6 utils/dedup — 去重器

#### 当前全局状态

```go
// utils/dedup/dedup.go
var duplicator inf.IDeDuplicator

func Init(conf *config.DeDuplicatorConf) {
    if duplicator != nil { return }
    duplicator = newDeDuplicator(conf.DeDuplicatorType, option)
}

func GetDeDuplicator() inf.IDeDuplicator {
    if duplicator == nil {
        duplicator = newDeDuplicator(def.DeDuplicatorTypeTTL, &DeDuplicatorOption{})
    }
    return duplicator
}
```

#### 问题

- `nil` 检查防重复初始化
- 所有 Node 的 RPC 请求共享去重缓存，可能产生跨 Node 误判

#### 改进方案

**删除清单**:
- `var duplicator inf.IDeDuplicator` — 删除
- `func Init(conf *config.DeDuplicatorConf)` — 删除
- `func GetDeDuplicator() inf.IDeDuplicator` — 删除

**新增**:
```go
func NewDeDuplicator(conf *config.DeDuplicatorConf) (inf.IDeDuplicator, error) {
    return newDeDuplicator(conf.DeDuplicatorType, option), nil
}

// Node 中
n.DeDuplicator, err = dedup.NewDeDuplicator(n.Config.NodeConf.DeDuplicatorConf)
```

**IDeDuplicator 接口增加 `Close()` 方法**:

```go
// interfaces/IDeduplicator.go
type IDeDuplicator interface {
    Seen(serviceUid string, id uint64) bool
    Close()   // 释放内部资源（如 go-cache 的 janitor goroutine）
}
```

**各实现补充 `Close()`**:

```go
// TTLDeDuplicator — go-cache 内部会启动 janitor goroutine 做定期清理，
// 必须显式 Flush + 置 nil 让 GC 回收 janitor
func (d *TTLDeDuplicator) Close() {
    if d.reqCache != nil {
        d.reqCache.Flush()
        d.reqCache = nil
    }
}

// LRUDeDuplicator — gcache 无后台 goroutine，Purge 清空即可
func (d *LRUDeDuplicator) Close() {
    d.mu.Lock()
    defer d.mu.Unlock()
    if d.cache != nil {
        d.cache.Purge()
    }
}
```

> **为什么需要 `Close()`**: `go-cache` 的 `cache.New(ttl, cleanTTL)` 在 cleanTTL > 0 时会启动一个后台
> janitor goroutine。如果不释放，Node 停止后该 goroutine 仍会持续运行，造成 goroutine 泄漏。
> `Start()` 中注册 cleanup、`Stop()` 中显式调用 `Close()` 可确保零残留。

**影响范围**: `core/rpc` 中 `CheckDuplicate` 调用、`interfaces/IDeduplicator.go`。所有 `dedup.GetDeDuplicator()` 改为通过 `NodeContext` 获取。

**建议优先级**: ⭐⭐⭐

---

### 3.7 cluster 包 — 集群管理器

#### 当前全局状态

```go
// cluster/cluster.go
var cluster Cluster   // 值类型全局变量

func GetCluster() *Cluster {
    return &cluster
}
```

`Cluster` 持有:
- `discovery` — 服务发现客户端
- `endpoints` — `EndpointManager` 指针
- `eventProcessor` / `eventChannel` — 集群事件处理

#### 问题

- `var cluster Cluster` 是值类型全局变量，`GetCluster()` 返回其指针
- 所有 Node 共享同一个 discovery 和 endpoints
- `Close()` 关闭 channel 后其他 Node 无法使用

#### 改进方案

**步骤 1**: 删除全局变量和 getter

**删除清单**:
- `var cluster Cluster` — 删除
- `func GetCluster() *Cluster` — 删除

**步骤 2**: 改为构造函数，嵌入 Logger

```go
type Cluster struct {
    *log.Logger              // 嵌入 Logger
    closed         chan struct{}
    discovery      inf.IDiscovery
    endpoints      *endpoints.EndpointManager
    eventProcessor *event.Processor
    eventChannel   chan inf.IEvent
}

func NewCluster() *Cluster {
    return &Cluster{}
}

// Node 中
n.Cluster = cluster.NewCluster()
n.Cluster.Init(ctx)  // ctx 为 NodeContext
n.Cluster.Start()
```

**步骤 3**: Cluster 内部创建独立的 EndpointManager

```go
func (c *Cluster) Init(ctx INodeContext) {
    c.Logger = ctx.Logger()
    c.endpoints = endpoints.NewEndpointManager()
    c.discovery = discovery.CreateDiscovery(ctx.Config().ClusterConf.DiscoveryType)
    // ...
}
```

**影响范围**: `node.go`、`router` 包（间接依赖 `endpoints.GetEndpointManager()`）。所有 `cluster.GetCluster()` 调用改为通过 `NodeContext` 获取。

**建议优先级**: ⭐⭐⭐⭐⭐

---

### 3.8 cluster/endpoints — 端点管理器

#### 当前全局状态

```go
// cluster/endpoints/endpoints.go
var endMgr = &EndpointManager{}

func GetEndpointManager() *EndpointManager {
    return endMgr
}
```

`EndpointManager` 持有:
- `repository` — PID 注册表（`mapPID`、`mapSvcBySNameAndSUid` 等）
- `remotes` — 远程连接 map
- `nodeUid`、`isClusterMode` — 节点信息

#### 问题

- 这是 **路由的核心**，所有服务查找（`router.Select`）都经过它
- 全局唯一导致不同 Node 的服务 PID 混在同一个 `Repository` 中
- 跨 Node 的本地调用和远程调用无法区分

#### 改进方案

**删除清单**:
- `var endMgr = &EndpointManager{}` — 删除
- `func GetEndpointManager() *EndpointManager` — 删除

**新增**:
```go
func NewEndpointManager() *EndpointManager {
    return &EndpointManager{}
}

// 由 Cluster 内部创建和管理，通过 Cluster 对外暴露
func (c *Cluster) GetEndpointManager() *EndpointManager {
    return c.endpoints
}
```

**router 包改造**: 不再调用全局 getter，改为从 `NodeContext` 链式获取

```go
// 改前
endpoints.GetEndpointManager().GetRepository().Select(...)

// 改后
ctx.Cluster().GetEndpointManager().GetRepository().Select(...)
```

**影响范围**: `router` 包所有函数、`cluster` 包、`core/service.go`（服务注册时调用 `AddService`）。

**建议优先级**: ⭐⭐⭐⭐⭐

---

### 3.9 cluster/discovery — 服务发现注册表

#### 当前全局状态

```go
// cluster/discovery/discovery.go
var discoveryRegistry sync.Map

func Register(name string, discovery inf.IDiscovery) {
    discoveryRegistry.Store(name, discovery)
}

func CreateDiscovery(name string) inf.IDiscovery {
    v, ok := discoveryRegistry.Load(name)
    if !ok { return nil }
    return v.(inf.IDiscovery)
}
```

子包 `etcd/` 的 `init()` 中自动注册:
```go
func init() {
    disc.Register("etcd", NewEtcdDiscovery())
}
```

#### 问题

- `discoveryRegistry` 是全局 `sync.Map`
- `init()` 自动注册意味着所有 Node 使用相同的 discovery 实例
- etcd discovery 内部持有连接状态，多 Node 无法独立配置不同的 etcd 集群

#### 改进方案

**删除清单**:
- `var discoveryRegistry sync.Map` — 删除
- `func Register(name string, discovery inf.IDiscovery)` — 删除
- `func CreateDiscovery(name string) inf.IDiscovery` — 删除
- `etcd/init.go` 中的 `func init()` — 删除

**改为工厂注册表**（注册的是创建函数而非实例）：

```go
// discovery/registry.go
var discoveryFactory sync.Map  // map[string]func() inf.IDiscovery

func Register(name string, creator func() inf.IDiscovery) {
    discoveryFactory.Store(name, creator)
}

func CreateDiscovery(name string) inf.IDiscovery {
    v, ok := discoveryFactory.Load(name)
    if !ok { return nil }
    return v.(func() inf.IDiscovery)()  // 每次调用创建新实例
}
```

> **说明**: `discoveryFactory` 保留为包级 `sync.Map`，因为它存储的是**无状态的工厂函数**（而非实例），
> 属于「只读注册表」类型，多 Node 共享同一份工厂函数不会冲突。
> 每次 `CreateDiscovery` 调用都会创建全新的 discovery 实例。

```go
// etcd/register.go (原 init.go)
func init() {
    disc.Register("etcd", func() inf.IDiscovery {
        return NewEtcdDiscovery()
    })
}
```

> `init()` 中注册**工厂函数**是可以接受的，因为工厂函数本身无状态。
> 关键改动是 `NewEtcdDiscovery()` 不在 `init()` 中调用，而是延迟到 `CreateDiscovery` 时才创建实例。

**影响范围**: `cluster.go`（`CreateDiscovery` 调用处）。

**建议优先级**: ⭐⭐⭐⭐

---

### 3.10 event 包 — 事件总线

#### 当前全局状态

```go
// event/eventBus.go
var bus *Bus
var busOnce sync.Once

func GetEventBus() *Bus {
    busOnce.Do(func() {
        bus = &Bus{}
    })
    return bus
}
```

`Bus` 持有:
- nats 连接
- 3 组 subscriber map（global/server/specific）
- 事件注册表、限流管理器、事件缓冲、指标

#### 问题

- `sync.Once` 保证全进程唯一
- 所有 Node 的事件订阅混在同一个 subscriber map 中
- nats 连接共享，prefix 混乱

#### 改进方案

**删除清单**:
- `var bus *Bus` — 删除
- `var busOnce sync.Once` — 删除
- `func GetEventBus() *Bus` — 删除

**新增**:
```go
func NewEventBus() *Bus {
    return &Bus{}
}

// Node 中
n.EventBus = event.NewEventBus()
n.EventBus.Init(n.Config.NodeConf.EventBusConf)
```

> `event/category.go` 中的 `eventClassifications` map 是**只读的事件分类定义**，可保留为包级变量（无多 Node 冲突）。

**影响范围**: `cluster`（事件处理）、所有 `Service`（事件订阅/发布）、`node.go`。所有 `event.GetEventBus()` 调用改为通过 `NodeContext` 获取。

**建议优先级**: ⭐⭐⭐⭐

---

### 3.11 services 包 — 服务管理器

#### 当前全局状态

```go
// services/services.go
var (
    lock        sync.RWMutex
    serviceMap  map[string]func() inf.IService  // 服务工厂注册表
    runServices []inf.IService                   // 运行中的服务列表
)

func init() {
    serviceMap = make(map[string]func() inf.IService)
}

// services/daemon.go
var Daemon = &daemon{}
```

#### 问题

- `serviceMap` 和 `runServices` 是包级全局，多 Node 的服务混在一起
- `StopAll()` 会停止所有 Node 的服务
- `Daemon` 是全局单例
- `sysService/` 的 `init()` 直接调用 `services.SetService()` 注册系统服务
- `daemon.OnInit()` 订阅全局事件总线的 `ServiceStatusChanged` 和 `NodeStatusChanged` 事件

#### 改进方案

**保留清单（全局共享工厂注册表）**:
- `var lock sync.RWMutex` — **保留**（保护 serviceMap 的并发安全）
- `var serviceMap map[string]func() inf.IService` — **保留**（全局服务工厂注册表，通过 `import` + `init()` 注册，所有 Node 共享。运行时只读 — 注册发生在 `init()` 阶段，Start() 之后不会再写入）
- `func init()` — **保留**（初始化 serviceMap）
- `func SetService(...)` — **保留**（供 `init()` 阶段注册服务工厂）
- `func GetServiceFactory(name string) func() inf.IService` — **新增**（供 `ServiceManager` 查询已注册的工厂函数）

**删除清单（运行时可变状态）**:
- `var runServices []inf.IService` — 删除（迁入 ServiceManager）
- `var Daemon = &daemon{}` — 删除（迁入 ServiceManager）
- `func Init()` — 删除（改为 ServiceManager.Init()）
- `func Start()` — 删除（改为 ServiceManager.Start()）
- `func StopAll()` — 删除（改为 ServiceManager.StopAll()）

> **关键决策**: `serviceMap` + `SetService()` 保留为全局，因为服务工厂注册通过 `import` + `init()` 完成（编译期决定），
> 而实际启动哪些服务由各 Node 的配置文件决定。这与 `discovery/etcd` 的工厂注册模式一致：**注册工厂函数是全局的，创建实例是 per-Node 的**。

**步骤 1**: 保留全局工厂注册表，引入 `ServiceManager` 收归运行时状态

```go
// ===== 全局工厂注册表（保留为包级变量） =====
var (
    lock       sync.RWMutex
    serviceMap = make(map[string]func() inf.IService)
)

func SetService(name string, builder func() inf.IService) {
    lock.Lock()
    serviceMap[name] = builder
    lock.Unlock()
}

func GetServiceFactory(name string) func() inf.IService {
    lock.RLock()
    defer lock.RUnlock()
    return serviceMap[name]
}

func GetAllServiceFactories() map[string]func() inf.IService {
    lock.RLock()
    defer lock.RUnlock()
    cp := make(map[string]func() inf.IService, len(serviceMap))
    for k, v := range serviceMap {
        cp[k] = v
    }
    return cp
}

// ===== 运行时状态（per-Node） =====
type ServiceManager struct {
    *log.Logger                                      // 嵌入 Logger
    lock        sync.RWMutex
    runServices []inf.IService
    daemon      *daemon
    nodeCtx     INodeContext
}

func NewServiceManager(ctx INodeContext) *ServiceManager {
    return &ServiceManager{
        Logger:  ctx.Logger(),
        daemon:  newDaemon(),
        nodeCtx: ctx,
    }
}

// Init 根据 Node 配置，从全局工厂注册表中查找并实例化需要启动的服务
func (m *ServiceManager) Init() error {
    factories := GetAllServiceFactories()
    for _, svcName := range m.nodeCtx.Config().NodeConf.Services {
        factory := factories[svcName]
        if factory == nil {
            return fmt.Errorf("service %q not registered", svcName)
        }
        // 实例化并初始化...
    }
    return nil
}

func (m *ServiceManager) Start() error { ... }
func (m *ServiceManager) StopAll() { ... }
```

> **daemon 改造**: `daemon` 嵌入到 `ServiceManager` 中，其 `OnInit()` 通过 `nodeCtx.EventBus()` 订阅事件
> （原来直接调用全局 `event.GetEventBus()`）。`daemon` 不再是全局 `var Daemon`，而是每个 `ServiceManager` 内部的成员。

**步骤 2**: `sysService/` 的 `init()` 全部删除，改为显式注册函数

```go
// 改前 (sysService/pprofservice/pprof.go)
func init() {
    services.SetService("PprofService", func() inf.IService { return &PprofService{} })
    systemConfig.RegisterServiceConf(&systemConfig.ServiceConfig{...})
}

// 改后: 删除 init()，提供注册函数
func RegisterPprofService(mgr *services.ServiceManager) {
    mgr.SetService("PprofService", func() inf.IService { return &PprofService{} })
}

func RegisterPprofServiceConf(cfg *config.Config) {
    cfg.RegisterServiceConf(&config.ServiceConfig{...})
}
```

**步骤 3**: Node 启动时主动调用注册

```go
// node.go
func (n *Node) Start(...) {
    // ...
    pprofservice.RegisterPprofService(n.ServiceMgr)
    dbservice.RegisterDBService(n.ServiceMgr)
    // ...
}
```

**影响范围**: `node.go`、所有 `sysService/` 的 `init()`、用户代码中的 `services.SetService()` 调用。

**建议优先级**: ⭐⭐⭐⭐⭐

---

### 3.12 rpc 包 — RPC 连接池 & Sender 管理

#### 当前全局状态

**连接池管理器** (`rpc/client/pool/factory.go`):
```go
var globalPoolManager *PoolManager
var poolManagerOnce sync.Once

func GetGlobalPoolManager() *PoolManager {
    poolManagerOnce.Do(func() {
        globalPoolManager = NewPoolManager()
    })
    return globalPoolManager
}
```

**Sender 管理** (`rpc/client/sender.go`):
```go
var senderMap = map[string]SenderCreator{...}
var senderHandlerMap map[string]map[string]inf.IRpcSender

func init() {
    senderHandlerMap = make(map[string]map[string]inf.IRpcSender)
    poolMgr := pool.GetGlobalPoolManager()
    for rpcType, creator := range senderMap {
        if rpcType != def.RpcTypeLocal {
            poolMgr.RegisterCreator(rpcType, pool.SenderCreator(creator))
        }
    }
}
```

**远程服务器** (`rpc/remote/pool/pool.go`):
```go
var remoteMap = map[string]inf.IRemoteServer{
    def.RpcTypeRpcx: rx.NewRpcxServer(),
    def.RpcTypeGrpc: gr.NewGrpcServer(),
    def.RpcTypeNats: nt.NewNatsServer(),
}
```

#### 问题

- `PoolManager` 是 `sync.Once` 单例，管理所有 RPC 连接
- `senderHandlerMap` 全局 map，所有 Node 的 sender 混在一起
- `remoteMap` 中每种协议只有一个 server 实例
- `Close()` 关闭所有连接影响全部 Node

#### 改进方案

**删除清单 (rpc/client/pool/factory.go)**:
- `var globalPoolManager *PoolManager` — 删除
- `var poolManagerOnce sync.Once` — 删除
- `func GetGlobalPoolManager() *PoolManager` — 删除

**删除清单 (rpc/client/sender.go)**:
- `var senderMap = map[string]SenderCreator{...}` — 删除（改为函数返回副本）
- `var senderHandlerMap map[string]map[string]inf.IRpcSender` — 删除
- `func init()` — 删除
- `func Close()` — 删除

**删除清单 (rpc/remote/pool/pool.go)**:
- `var remoteMap = map[string]inf.IRemoteServer{...}` — 删除

**步骤 1**: `PoolManager` 改为实例化（已有 `NewPoolManager()`）

```go
n.PoolManager = pool.NewPoolManager()
```

**步骤 2**: `SenderManager` 结构体化，持有独立状态

```go
type SenderManager struct {
    *log.Logger                                       // 嵌入 Logger
    poolMgr       *pool.PoolManager
    senderMap     map[string]SenderCreator            // 协议 → 创建器（只读）
    handlerMap    map[string]map[string]inf.IRpcSender
    handlerLock   sync.RWMutex
}

func NewSenderManager(poolMgr *pool.PoolManager, logger *log.Logger) *SenderManager {
    mgr := &SenderManager{
        Logger:     logger,
        poolMgr:    poolMgr,
        senderMap:  defaultSenderMap(),   // 返回默认创建器的拷贝
        handlerMap: make(map[string]map[string]inf.IRpcSender),
    }
    mgr.registerCreators()
    return mgr
}
```

**步骤 3**: `remoteMap` 改为工厂模式

```go
// 保留为包级变量（存储的是无状态工厂函数，非实例）
var remoteFactory = map[string]func() inf.IRemoteServer{
    def.RpcTypeRpcx: func() inf.IRemoteServer { return rx.NewRpcxServer() },
    def.RpcTypeGrpc: func() inf.IRemoteServer { return gr.NewGrpcServer() },
    def.RpcTypeNats: func() inf.IRemoteServer { return nt.NewNatsServer() },
}

func CreateRemoteServer(rpcType string) inf.IRemoteServer {
    if f, ok := remoteFactory[rpcType]; ok {
        return f()  // 每次创建新实例
    }
    return nil
}
```

**影响范围**: `cluster/endpoints`（创建 remote 连接时）、`core/rpc`（发送 RPC 时获取 sender）。

**建议优先级**: ⭐⭐⭐⭐

---

### 3.13 rpc/message/msgenvelope — 消息对象池

#### 当前全局状态

```go
// msgenvelope/meta.go
var metaPool = pool.NewSyncPoolWrapper(...)          // 立即初始化
var metaBorrowTracker = struct{ sync.Mutex; borrows map[uintptr]metaBorrow }{...}

// msgenvelope/message.go
var msgPool pool.IPool[*actor.Message]               // sync.Once 延迟初始化
var msgInitOnce sync.Once

// msgenvelope/envelope.go
var msgEnvelopePool pool.IPool[*MsgEnvelope]         // sync.Once 延迟初始化
var msgEnvelopePoolOnce sync.Once
```

#### 评估

这些是 **对象池（sync.Pool 包装器）**，本质上是性能优化的内存复用机制。

**结论: 可保留为全局**

理由:
1. `sync.Pool` 本身是线程安全的，多 Node 共享不会导致数据混乱
2. 对象池中的对象在 `Get` 后由调用者独占，`Put` 回去前会 `Reset`
3. 拆分为每 Node 独立的池反而会降低复用效率
4. `metaBorrowTracker` 仅在 debug 模式下用于泄漏检测，不影响功能

**唯一注意点**: `DumpMetaPoolLeaks()` 会输出所有 Node 的泄漏信息，但这只是调试功能，可接受。

**建议优先级**: ⭐（最低 — 无需改动）

---

### 3.14 profiler 包 — 性能分析器

#### 当前全局状态

```go
// profiler/profiler.go
var DefaultMaxOvertime time.Duration = 1 * time.Second
var DefaultOvertime time.Duration = 10 * time.Millisecond
var DefaultMaxRecordNum int = 100
var mapLock sync.RWMutex
var mapProfiler map[string]*Profiler
var reportFunc ReportFunType = DefaultReportFunction

func init() {
    mapProfiler = map[string]*Profiler{}
}
```

#### 问题

- `mapProfiler` 全局 map，多 Node 的服务 profiler 名称可能冲突
- `Report()` 遍历全部 profiler，无法区分 Node

#### 改进方案

**删除清单**:
- `var mapLock sync.RWMutex` — 删除
- `var mapProfiler map[string]*Profiler` — 删除
- `var reportFunc ReportFunType` — 删除
- `func init()` — 删除
- `func RegProfiler(...)` — 删除（改为实例方法）
- `func UnRegProfiler(...)` — 删除（改为实例方法）
- `func Report()` — 删除（改为实例方法）

> `DefaultMaxOvertime`、`DefaultOvertime`、`DefaultMaxRecordNum` 三个导出常量可保留（不可变默认值）。

**新增**:
```go
type Registry struct {
    lock      sync.RWMutex
    profilers map[string]*Profiler
    report    ReportFunType
}

func NewRegistry() *Registry {
    return &Registry{
        profilers: make(map[string]*Profiler),
        report:    DefaultReportFunction,
    }
}

func (r *Registry) RegProfiler(name string, logger log.ILoggerX) *Profiler { ... }
func (r *Registry) UnRegProfiler(name string) { ... }
func (r *Registry) Report() { ... }
```

**影响范围**: `core/service.go`（`RegProfiler`/`UnRegProfiler` 调用处）。

**建议优先级**: ⭐⭐⭐

---

### 3.15 plugins 包 — 插件注册表

#### 当前全局状态

```go
// plugins/plugin.go
var pluginMap = make(map[string]*PluginInfo)
var lock sync.Mutex
```

#### 改进方案

**删除清单**:
- `var pluginMap = make(map[string]*PluginInfo)` — 删除
- `var lock sync.Mutex` — 删除
- `func Register(...)` — 删除（改为实例方法）
- `func LoadAll()` — 删除（改为实例方法）

**新增**:
```go
type PluginManager struct {
    lock      sync.Mutex
    pluginMap map[string]*PluginInfo
}

func NewPluginManager() *PluginManager {
    return &PluginManager{
        pluginMap: make(map[string]*PluginInfo),
    }
}

func (pm *PluginManager) Register(name string, path string) { ... }
func (pm *PluginManager) LoadAll() { ... }
```

> 当前功能尚未完成（`LoadAll` 为空实现），但仍应一并改造以消除全局变量。

**建议优先级**: ⭐⭐（低 — 功能未完成，但全局变量仍需清除）

---

### 3.16 router 包 — 路由

#### 当前全局状态

```go
// router/selector.go — 无独立全局变量
// 所有函数直接委托给 endpoints.GetEndpointManager()
func Select(sender *actor.PID, options ...inf.SelectParamBuilder) inf.IBus {
    return endpoints.GetEndpointManager().GetRepository().Select(sender, options...)
}
```

#### 改进方案

当前 router 包的函数虽然没有独立的包级变量，但它们通过 `endpoints.GetEndpointManager()` 间接依赖全局状态。

**改造为结构体**:

```go
type Router struct {
    endpoints *endpoints.EndpointManager
}

func NewRouter(endpoints *endpoints.EndpointManager) *Router {
    return &Router{endpoints: endpoints}
}

func (r *Router) Select(sender *actor.PID, options ...inf.SelectParamBuilder) inf.IBus {
    return r.endpoints.GetRepository().Select(sender, options...)
}

func (r *Router) SelectByPid(sender, receiver *actor.PID) inf.IBus { ... }
func (r *Router) SelectByServiceUid(sender *actor.PID, uid string) inf.IBus { ... }
func (r *Router) SelectByRule(sender *actor.PID, rule func(*actor.PID) bool) inf.IBus { ... }
func (r *Router) SelectByServiceType(sender *actor.PID, partition int32, serviceType, serviceName string) inf.IBus { ... }
func (r *Router) SelectByFilterAndChoice(...) inf.IBus { ... }
```

**删除清单**:
- 所有包级函数 `Select()`、`SelectByPid()` 等 — 全部删除，改为 `Router` 的实例方法

**Router 实例由 Node 持有或由 Cluster 创建**:
```go
// Node 中
n.Router = router.NewRouter(n.Cluster.GetEndpointManager())
```

**影响范围**: 所有调用 `router.Select`/`router.SelectByPid` 等的地方。

**建议优先级**: ⭐⭐⭐⭐（与 endpoints 改动绑定）

---

### 3.17 core/rpc — RPC 方法索引

#### 当前全局状态

```go
// core/rpc/handler.go
var (
    apiPrefixIndex   = newPrefixBucketIndex([]string{"Api", "API"})
    rpcPrefixIndex   = newPrefixBucketIndex([]string{"Rpc", "RPC"})
    apiRoPrefixIndex = newPrefixBucketIndex([]string{"ApiRo", "APIRo"})
    rpcRoPrefixIndex = newPrefixBucketIndex([]string{"RpcRo", "RPCRo"})
)
```

#### 评估

这些 `prefixBucketIndex` 通过 `Register`/`RegisterRo` 在服务启动时填充方法名。

**问题**: 方法 append 到 `byFirst [256][]string` 数组中，**无锁，非并发安全**。多 Node 同时注册方法会导致 data race。

#### 改进方案

**删除清单**:
- `var apiPrefixIndex = newPrefixBucketIndex(...)` — 删除
- `var rpcPrefixIndex = newPrefixBucketIndex(...)` — 删除
- `var apiRoPrefixIndex = newPrefixBucketIndex(...)` — 删除
- `var rpcRoPrefixIndex = newPrefixBucketIndex(...)` — 删除

**改为每个 Service 持有自己的方法索引**:

```go
type MethodIndex struct {
    apiPrefixIndex   *prefixBucketIndex
    rpcPrefixIndex   *prefixBucketIndex
    apiRoPrefixIndex *prefixBucketIndex
    rpcRoPrefixIndex *prefixBucketIndex
}

func NewMethodIndex() *MethodIndex {
    return &MethodIndex{
        apiPrefixIndex:   newPrefixBucketIndex([]string{"Api", "API"}),
        rpcPrefixIndex:   newPrefixBucketIndex([]string{"Rpc", "RPC"}),
        apiRoPrefixIndex: newPrefixBucketIndex([]string{"ApiRo", "APIRo"}),
        rpcRoPrefixIndex: newPrefixBucketIndex([]string{"RpcRo", "RPCRo"}),
    }
}
```

> 该改动同时修复了原有的**并发安全问题**（`prefixBucketIndex.byFirst` 的 append 操作无锁保护）。

**影响范围**: `core/service.go`（方法注册）、`core/handler_job.go`（方法查找）。

**建议优先级**: ⭐⭐⭐

---

### 3.18 utils/translate — 国际化

#### 当前全局状态

```go
// utils/translate/translate.go
var language LanguageType = Chinese
var translator *ut.UniversalTranslator

// init() in translate.go, zh.go, en.go
func init() {
    translator = ut.New(zh.New(), en.New())
}
```

#### 评估

**结论: 可保留为全局**

理由:
1. 语言翻译器本身是只读的（注册后不变）
2. `SetLanguage` 通常在启动时调用一次
3. 翻译内容与 Node 无关

**但如果需要不同 Node 使用不同语言**: 可以将 `language` 变量放入 Node 中。

**建议优先级**: ⭐（最低 — 通常无需改动）

---

### 3.19 utils/pid — PID 文件

#### 当前全局状态

```go
func RecordPID(pvPath string, nodeId int32, nodeType string) { ... }
func DeletePID(pvPath string, nodeId int32, nodeType string) { ... }
```

#### 评估

**结论: 无全局变量**，每次调用独立写入/删除文件，通过参数区分。多 Node 使用不同的 `nodeId` 即可，**无需改动**。

**建议优先级**: ⭐（无需改动）

---

### 3.20 sysModule/mongomodule — Mongo 选项注册

#### 当前全局状态

```go
var opts []MongoOpt

func Register(fns ...MongoOpt) {
    opts = append(opts, fns...)
}
```

#### 问题

- 全局 slice，无锁，**非并发安全**
- 多 Node 的 mongo 配置混在一起

#### 改进方案

**删除清单**:
- `var opts []MongoOpt` — 删除
- `func Register(fns ...MongoOpt)` — 删除（改为实例方法）

**改为在 MongoModule 实例上注册**:

```go
type MongoModule struct {
    opts []MongoOpt
    // ...
}

func (m *MongoModule) Register(fns ...MongoOpt) {
    m.opts = append(m.opts, fns...)
}
```

> 同时修复原有的**并发安全问题**（全局 slice append 无锁保护）。

**建议优先级**: ⭐⭐⭐

---

### 3.21 actor/mailbox/jobs + rpc/message/msgenvelope — 对象池

#### 当前全局状态

```go
// actor/mailbox/jobs/factory.go
var msgJobPool                  // sync.Once
var eventBusJobPool             // sync.Once
var timerJobPool                // sync.Once
var concurrentCallbackJobPool   // sync.Once
var sysCtlJobPool               // sync.Once

// rpc/message/msgenvelope/ 中的对象池（见 §3.13）
```

#### 评估

**结论: 可保留为全局**

理由: 同 3.13（消息对象池），`sync.Pool` 包装器天然支持并发共享，拆分反而降低效率。

**建议优先级**: ⭐（无需改动）

---

### 3.21.1 rpc/message/msgbus — MessageBus 对象池

#### 当前全局状态

```go
// rpc/message/msgbus/bus.go
var busPool pool.IPool[*MessageBus]
var busPoolOnce sync.Once

func getBusPool() pool.IPool[*MessageBus] {
    busPoolOnce.Do(func() {
        busPool = pool.NewPerPPoolWrapper(
            config.Conf.NodeConf.BusPoolSize,   // ← 从全局 config 读取
            func() *MessageBus { return &MessageBus{} },
            pool.NewStatsRecorder("busPool"),
            // ...
        )
    })
    return busPool
}
```

#### 问题

- `busPool` 是 `PerPPoolWrapper`（带容量上限的对象池），不同于 §3.13 中的 `sync.Pool` 包装器
- `sync.Once` 内读取 `config.Conf.NodeConf.BusPoolSize`，改造后全局 `config.Conf` 被删除
- 即使改为 per-Node config，`sync.Once` 只会使用**第一个 Node** 的 `BusPoolSize`，后续 Node 的配置被忽略
- 与 `sync.Pool` 不同，`PerPPoolWrapper` 有固定容量，不同 Node 可能需要不同的池大小

#### 改进方案

**方案 A（推荐）: 改为延迟初始化 + 传参**

将 `getBusPool()` 改为接收 poolSize 参数，由使用方（Node 上下文）传入：

```go
// msgbus/bus.go — 不再使用包级 sync.Once
func newBusPool(poolSize int) pool.IPool[*MessageBus] {
    return pool.NewPerPPoolWrapper(
        poolSize,
        func() *MessageBus { return &MessageBus{} },
        pool.NewStatsRecorder("busPool"),
        // ...
    )
}
```

`busPool` 实例由 Node 持有（可放在 `ServiceManager` 或 `Node` 中），通过 `INodeContext` 传递给需要 `MessageBus` 的组件。

**方案 B: 允许全局共享，但使用固定默认值**

如果所有 Node 的 `BusPoolSize` 相同，可保留全局但**不再从 config 读取**，改为构造时传入或使用合理默认值。

**删除清单**:
- `var busPool pool.IPool[*MessageBus]` — 删除
- `var busPoolOnce sync.Once` — 删除
- `func getBusPool()` — 删除（改为实例方法或工厂函数）

**影响范围**: `msgbus.NewMessageBus()` / `msgbus.Put()` 等所有使用 `getBusPool()` 的地方。

**建议优先级**: ⭐⭐⭐⭐（高 — `config.Conf` 删除后必须改造，否则编译报错）

---

### 3.22 utils/memdbx — 内存数据库

#### 当前全局状态

```go
// utils/memdbx/memdbx.go
var memDB *gorm.DB

func Start(models []interface{}) {
    db, _ := gorm.Open(sqlite.Open("file::memory:?cache=shared"), ...)
    db.AutoMigrate(models...)
    memDB = db
}

func GetDB() *gorm.DB { return memDB }
```

#### 问题

- `memDB` 是全局唯一的 SQLite 内存数据库连接
- 多 Node 的数据表混在同一个 DB 中，无法隔离
- 一个 Node 关闭时若释放连接，其他 Node 的查询全部失败

#### 改进方案

**删除清单**:
- `var memDB *gorm.DB` — 删除
- `func Start(models []interface{})` — 删除
- `func GetDB() *gorm.DB` — 删除

**新增**:
```go
type MemDB struct {
    db *gorm.DB
}

func NewMemDB(models []interface{}) (*MemDB, error) {
    db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), ...)
    if err != nil { return nil, err }
    db.AutoMigrate(models...)
    return &MemDB{db: db}, nil
}

func (m *MemDB) GetDB() *gorm.DB { return m.db }
func (m *MemDB) Close() error { /* ... */ }
```

**影响范围**: 所有调用 `memdbx.GetDB()` 的模块。

**建议优先级**: ⭐⭐⭐（高风险 — 全局 DB 连接多 Node 冲突）

---

### 3.23 utils/jwtx — JWT 密钥

#### 当前全局状态

```go
// utils/jwtx/jwtx.go
var jwtSecret = []byte("ember-secret-pwd-xxyyzz")  // 硬编码密钥
```

#### 问题

- JWT 密钥硬编码在源码中，**安全隐患**
- 不同 Node 可能需要不同的密钥（如测试环境 vs 生产环境）
- 所有 Node 共享同一把密钥，无法独立配置

#### 改进方案

**删除清单**:
- `var jwtSecret = []byte(...)` — 删除

**改为从 Config 中读取**:

```go
// 将 jwtSecret 作为 Config 的一部分
type JwtConfig struct {
    Secret string `yaml:"secret"`
}

// jwtx 包的函数改为接收密钥参数
func GenerateToken(secret []byte, claims jwt.Claims) (string, error) { ... }
func ParseToken(secret []byte, tokenStr string) (*jwt.Token, error) { ... }
```

**影响范围**: 所有调用 `jwtx.GenerateToken()` / `jwtx.ParseToken()` 的地方。

**建议优先级**: ⭐⭐⭐⭐（安全隐患 + 多 Node 配置独立性）

---

### 3.24 utils/timelib — 服务器时间偏移

#### 当前全局状态

```go
// utils/timelib/time.go
var timeOffset time.Duration
```

以及 `timingwheel/init.go` 中的 `SetTimeOffset(offset time.Duration)` 全局函数。

#### 问题

- `timeOffset` 是全局偏移量，用于模拟服务器时间加速/回退
- 不同 Node 可能需要不同的时间偏移（如测试时模拟时区差异）
- 一个 Node 调用 `SetTimeOffset` 会影响所有 Node 的时间计算

#### 改进方案

**删除清单**:
- `var timeOffset time.Duration` — 删除
- `func SetTimeOffset(offset time.Duration)` — 删除（已在 §3.4 列入）

**改为 Node 持有时间偏移**:

```go
// Node 结构体中
type Node struct {
    // ...
    TimeOffset time.Duration
}

// timelib 包的函数改为接收偏移量参数，或通过 NodeContext 获取
func NowWithOffset(offset time.Duration) time.Time {
    return time.Now().Add(offset)
}
```

> 也可将 `timeOffset` 放入 `TimingWheel` 实例中，由 `TimingWheel.SetTimeOffset()` 管理。

**影响范围**: 所有调用 `timelib.Now()` / `timingwheel.SetTimeOffset()` 的地方。

**建议优先级**: ⭐⭐⭐（多 Node 时间独立性）

---

### 3.25 utils/codec — 编解码器注册表

#### 当前全局状态

```go
// utils/codec/codec.go
var codecs = map[int32]inf.ICodec{}

func init() {
    codecs[def.CodecJson] = &JsonCodec{}
    codecs[def.CodecProtobuf] = &ProtobufCodec{}
}

func GetCodec(codecType int32) inf.ICodec { return codecs[codecType] }

// utils/codec/protobuf.go
var typeUrlCache = map[string]reflect.Type{}
var typeUrlCacheMu sync.RWMutex
```

#### 评估

- `codecs` map 在 `init()` 中填充后运行时**只读** — 可保留为包级变量
- `typeUrlCache` 是运行时**持续增长**的缓存，有 `sync.RWMutex` 保护，线程安全
- 多 Node 共享 codec 注册表和类型缓存不会产生冲突（codec 是无状态的解析器）

**结论: 可保留为全局**

理由:
1. `codecs` 在 `init()` 后不可变，且 codec 实例本身无状态
2. `typeUrlCache` 虽然可增长，但有锁保护，缓存内容跨 Node 共享反而提高效率
3. 无多 Node 隔离需求

**建议优先级**: ⭐（无需改动 — 加入白名单）

---

### 3.26 utils/validate — 全局验证器

#### 当前全局状态

```go
// utils/validate/validate.go
var validate *validator.Validate
var transZh ut.Translator
var transEn ut.Translator
var translator *ut.UniversalTranslator

func init() {
    validate = validator.New()
    translator = ut.New(zh.New(), en.New())
    transZh, _ = translator.GetTranslator("zh")
    transEn, _ = translator.GetTranslator("en")
    // 注册翻译...
}
```

#### 评估

**结论: 可保留为全局**

理由:
1. 验证器及翻译器在 `init()` 后只读使用
2. `validator.Validate` 本身是并发安全的（官方文档保证）
3. 验证规则与 Node 无关，不需要隔离

> **注意**: 当前代码中 `Validator` 为**导出变量**（`var Validator *validator.Validate`），外部可 `validate.Validator = xxx` 覆盖。建议改为非导出 + getter 函数，或在白名单中注明「导出但约定只读」。

**建议优先级**: ⭐（无需改动 — 加入白名单）

---

### 3.27 utils/emberctx — Trace 序列号

#### 当前全局状态

```go
// utils/emberctx/ctx.go
var emberHeaderKey = &contextKey{}       // 不可变，context key 标识
var traceSeq atomic.Uint64               // 全局递增序列号
```

#### 评估

- `emberHeaderKey` 是不可变的 context key，安全保留
- `traceSeq` 是全局递增的 trace ID 生成器

**结论: 可保留为全局**

理由:
1. `atomic.Uint64` 本身是并发安全的
2. 全局递增的 trace seq 在多 Node 场景下仍然唯一（不会重复）
3. 如果需要区分 Node 来源，可在 trace 前缀中加入 NodeId，无需拆分序列号生成器

> **可选优化**: 如果需要 trace ID 中体现 Node 身份，可改为 `NodeId-Seq` 格式，但序列号生成器本身可保持全局。

**建议优先级**: ⭐（无需改动 — 加入白名单）

---

### 3.28 utils/pool — 连接池统计

#### 当前全局状态

```go
// utils/pool/pool.go
var poolStates = make(map[string]IStatsRecorder)
```

#### 问题

- `poolStates` 是全局 map，记录各连接池的统计信息
- 多 Node 的连接池统计混在一起，无法区分
- 普通 map，**无锁保护**，并发写入有 data race 风险

#### 改进方案

**删除清单**:
- `var poolStates = make(map[string]IStatsRecorder)` — 删除
- 相关的包级注册/查询函数 — 删除

**改为实例化**（可以由 Node 或 PoolManager 持有）:

```go
type PoolStats struct {
    mu     sync.RWMutex
    states map[string]IStatsRecorder
}

func NewPoolStats() *PoolStats {
    return &PoolStats{states: make(map[string]IStatsRecorder)}
}
```

> 同时修复原有的**并发安全问题**（全局 map 无锁保护）。

**建议优先级**: ⭐⭐（中 — 统计混合 + data race）

---

### 3.29 actor/mailbox/jobs — Job 工厂注册表

#### 当前全局状态

```go
// actor/mailbox/jobs/factory.go
var jobFactory = map[def.MailboxJobType]jobEntry{
    def.MailboxJobTypeRpc:                {creator: ..., pool: ...},
    def.MailboxJobTypeEventBus:           {creator: ..., pool: ...},
    def.MailboxJobTypeTimer:              {creator: ..., pool: ...},
    def.MailboxJobTypeConcurrentCallback: {creator: ..., pool: ...},
    def.MailboxJobTypeSysCtl:             {creator: ..., pool: ...},
}
```

另有 5 个 `xxxPool + xxxPoolOnce` 对组成 Job 对象池（`msgJobPool`、`eventBusJobPool`、`timerJobPool`、`concurrentCallbackJobPool`、`sysCtlJobPool`）。

#### 评估

- `jobFactory` 在包加载时初始化，**运行时只读** — 但作为普通 map 无锁保护
- 5 个 Job 对象池是 `sync.Pool` 包装器

**结论: 可保留为全局**

理由:
1. `jobFactory` 初始化后不再修改，属于只读注册表
2. 5 个 Job 对象池同 §3.13 / §3.21，`sync.Pool` 天然并发安全

> **注意**: 如果将来需要自定义 Job 类型的注册（`RegisterJobType`），则需要改为并发安全的注册表。

**建议优先级**: ⭐（无需改动 — 加入白名单）

---

### 3.30 utils/serializer — 序列化器注册表

#### 当前全局状态

```go
// utils/serializer/serializer.go
var serializeType int32
var serializers []Serializer

func init() {
    serializers = append(serializers, &JsonSerializer{}, &ProtobufSerializer{})
}
```

#### 评估

**结论: 可保留为全局**

理由:
1. `serializers` 在 `init()` 后只读
2. `serializeType` 可在启动时设置，但多 Node 通常使用相同的序列化方式
3. 序列化器本身无状态

> **注意**: 如果不同 Node 需要使用不同的序列化方式，`serializeType` 应迁入 Config。但当前场景下无此需求。

**建议优先级**: ⭐（无需改动 — 加入白名单）

---

### 3.31 core/rpc/selector — RPC 选择器

#### 当前全局状态

```go
// core/rpc/selector.go
// 无独立包级变量，但所有方法直接委托给 router 包的全局函数
func (h *Handler) Select(sender *actor.PID, ...) inf.IBus {
    return router.Select(sender, ...)
}
func (h *Handler) SelectByPid(sender, receiver *actor.PID) inf.IBus {
    return router.SelectByPid(sender, receiver)
}
```

#### 改进方案

当 `router` 包改造为 `Router` 结构体（§3.16）后，`core/rpc/selector.go` 中的方法需要从 `NodeContext` 获取 `Router` 实例：

```go
// 改后
func (h *Handler) Select(sender *actor.PID, ...) inf.IBus {
    return h.nodeCtx.Router().Select(sender, ...)
}
```

> 这不是独立改造项，而是 §3.16 Router 改造的**连锁影响**。`Handler` 需要持有 `INodeContext` 或直接持有 `*Router` 引用。

**影响范围**: `core/rpc/selector.go` 中所有 `Select*` 方法。

**建议优先级**: ⭐⭐⭐⭐（与 §3.16 绑定）

---

## 四、NodeContext 设计

### 4.1 接口定义

```go
// interfaces/INodeContext.go
type INodeContext interface {
    // 基础设施
    Config() *config.Config
    Logger() *log.Logger
    AntsPool() *asynclib.Pool
    TimingWheel() *timingwheel.TimingWheel
    
    // 核心组件
    RpcMonitor() *monitor.RpcMonitor
    DeDuplicator() inf.IDeDuplicator
    Cluster() *cluster.Cluster
    EventBus() *event.Bus
    ServiceMgr() *services.ServiceManager
    
    // RPC 层
    PoolManager() *pool.PoolManager
    SenderMgr() *client.SenderManager
    
    // 路由 & 辅助
    Router() *router.Router
    Profiler() *profiler.Registry
    PluginMgr() *plugins.PluginManager
    PoolStats() *pool.PoolStats
    
    // 节点信息
    NodeId() int32
    NodeUid() string
    TimeOffset() time.Duration
}
```

> **说明**: `Router()`、`SenderMgr()`、`PluginMgr()` 在原文档 §2.1 的 Node 结构体中已定义为字段，
> 但原 `INodeContext` 接口中遗漏了。`NodeId()`、`NodeUid()` 用于需要标识当前 Node 身份的场景
> （如 trace ID 前缀、日志标记）。`TimeOffset()` 用于获取当前 Node 的时间偏移。

### 4.2 Logger 嵌入策略

所有核心组件通过 **嵌入 `*log.Logger`** 获得日志能力，使调用方可以直接 `s.Info()`：

```go
// 组件嵌入 Logger 的标准模式
type MyComponent struct {
    *log.Logger           // 嵌入，获得 Info/Warn/Error 等方法
    nodeCtx INodeContext  // 持有上下文引用
}

func NewMyComponent(ctx INodeContext) *MyComponent {
    return &MyComponent{
        Logger:  ctx.Logger(),
        nodeCtx: ctx,
    }
}

// 使用
comp := NewMyComponent(ctx)
comp.Info("started")       // 直接调用，无需 comp.logger.Info()
```

**需要嵌入 Logger 的组件列表**:

| 组件 | 嵌入位置 |
|------|----------|
| `Service` (core) | `Service` 结构体 |
| `Cluster` | `Cluster` 结构体 |
| `RpcMonitor` | `RpcMonitor` 结构体 |
| `ServiceManager` | `ServiceManager` 结构体 |
| `EndpointManager` | `EndpointManager` 结构体 |
| `EventBus` | `Bus` 结构体 |
| `SenderManager` | `SenderManager` 结构体 |
| `TimingWheel` | 通过构造参数传入（已有 `logger` 字段） |

### 4.3 传递策略

各组件获取依赖的方式：

1. **构造函数注入 `INodeContext`**（推荐）: `NewCluster(ctx INodeContext) *Cluster`
2. **组件内部存储 `INodeContext` 引用**，后续方法调用直接使用
3. **Logger 通过嵌入获得**，其他依赖通过 `nodeCtx` 获取

### 4.4 Service 中的上下文传递

每个 `Service` 嵌入 Logger 并持有 `NodeContext`：

```go
// core/service.go — 改造后
type Service struct {
    *log.Logger            // 嵌入 Logger（替代 log.SysLogger）
    nodeCtx INodeContext   // 所属 Node 的上下文
    
    // --- 以下字段保持不变 ---
    Module
    inf.IMessageInvoker
    pid                    *actor.PID
    name                   string
    src                    inf.IService
    cfg                    interface{}
    status                 int32
    isPrimarySecondaryMode bool
    mailbox                *mailbox.Mailbox
    eventProcessor         *event.Processor
    profiler               *profiler.Profiler
    // ...
}
```

**Service 初始化时注入 NodeContext**:

```go
// ServiceManager.Init() 中为每个 Service 注入上下文
func (m *ServiceManager) initService(svc inf.IService) {
    coreService := svc.GetCoreService()  // 获取内嵌的 core.Service
    coreService.Logger = m.nodeCtx.Logger()
    coreService.nodeCtx = m.nodeCtx
    coreService.Init(m.nodeCtx, ...)
}
```

**Service 内部全局依赖替换对照**:

| 原全局调用 | 改造后调用 |
|-----------|-----------|
| `log.SysLogger.Info(...)` | `s.Info(...)` (嵌入 Logger) |
| `config.Conf.XXX` | `s.nodeCtx.Config().XXX` |
| `config.Conf.IsDebug()` | `s.nodeCtx.Config().IsDebug()` |
| `endpoints.GetEndpointManager()` | `s.nodeCtx.Cluster().GetEndpointManager()` |
| `cluster.GetCluster().IsClusterMode()` | `s.nodeCtx.Cluster().IsClusterMode()` |
| `router.Select(...)` | `s.nodeCtx.Router().Select(...)` |
| `timingwheel.GetTimingWheel()` | `s.nodeCtx.TimingWheel()` |
| `asynclib.Go(f)` | `s.nodeCtx.AntsPool().Go(f)` |
| `monitor.GetRpcMonitor().GenSeq()` | `s.nodeCtx.RpcMonitor().GenSeq()` |
| `event.GetEventBus()` | `s.nodeCtx.EventBus()` |
| `profiler.RegProfiler(...)` | `s.nodeCtx.Profiler().RegProfiler(...)` |

> **用户服务（继承 `core.Service`）无需额外操作**: 框架自动注入 `NodeContext`，
> 用户服务中通过 `s.Info()`、`s.Select()` 等方法间接使用，无感知。

### 4.5 cluster.go 匿名导入改造

当前 `cluster/cluster.go` 中有匿名导入触发 discovery 插件注册：

```go
import _ "github.com/.../cluster/discovery/etcd"  // 触发 etcd init() 注册
```

改造后（§3.9），etcd 的 `init()` 注册的是**工厂函数**而非实例，因此匿名导入仍然可以保留。
但需要确保 `import _` 出现在 `cluster.go` 或 `node.go` 中（推荐放在 `node.go`），
使得所有可用的 discovery 实现在编译时自动注册到工厂注册表中。

---

## 五、实施路线图

### Phase 1: 基础设施层（无功能变化）

| 步骤 | 组件 | 改动内容 | 预估工作量 |
|------|------|----------|-----------|
| 1.1 | `config` | 结构体实例化，`NewConfig()` + `Load()` | 大 |
| 1.2 | `log` | `NewLogger()` 工厂函数 | 中 |
| 1.3 | `asynclib` | `Pool` 结构体 + `NewPool()` | 小 |
| 1.4 | `timingwheel` | 去掉全局函数，暴露实例方法 | 小 |
| 1.5 | `dedup` | `NewDeDuplicator()` 工厂函数 + `IDeDuplicator.Close()` | 小 |

### Phase 2: 核心组件层

| 步骤 | 组件 | 改动内容 | 预估工作量 |
|------|------|----------|-----------|
| 2.1 | `monitor` | 去掉 `sync.Once`，`NewRpcMonitor()` | 中 |
| 2.2 | `event` | 去掉 `sync.Once`，`NewEventBus()` | 中 |
| 2.3 | `services` | `ServiceManager` 结构体 | 大 |
| 2.4 | `cluster` | `NewCluster()` + 内部持有 endpoints | 大 |
| 2.5 | `endpoints` | `NewEndpointManager()` | 大 |
| 2.6 | `discovery` | 工厂注册模式（注册创建函数） | 中 |

### Phase 3: RPC 层

| 步骤 | 组件 | 改动内容 | 预估工作量 |
|------|------|----------|-----------|
| 3.1 | `rpc/client/pool` | 去掉 `sync.Once`，实例化 PoolManager | 小 |
| 3.2 | `rpc/client` | `SenderManager` 结构体 | 中 |
| 3.3 | `rpc/remote/pool` | 工厂模式 | 小 |
| 3.4 | `core/rpc` | `MethodIndex` 结构体 | 中 |
| 3.5 | `rpc/message/msgbus` | `busPool` 去掉 `sync.Once` + `config.Conf` 依赖（§3.21.1） | 小 |

### Phase 4: 辅助组件层

| 步骤 | 组件 | 改动内容 | 预估工作量 |
|------|------|----------|-----------|
| 4.1 | `profiler` | `Registry` 结构体 | 小 |
| 4.2 | `plugins` | `PluginManager` 结构体 | 小 |
| 4.3 | `router` | `Router` 结构体 + `core/rpc/selector` 连锁改动 | 中 |
| 4.4 | `sysService` | `init()` 改为注册函数 | 小 |
| 4.5 | `sysModule/mongo` | opts 收归到 Module 实例 | 小 |
| 4.6 | `utils/memdbx` | `MemDB` 结构体（全局 DB 连接） | 小 |
| 4.7 | `utils/jwtx` | JWT 密钥从 Config 读取 | 小 |
| 4.8 | `utils/timelib` | `timeOffset` 迁入 Node/TimingWheel | 小 |
| 4.9 | `utils/pool` | `PoolStats` 结构体（统计 map 实例化） | 小 |

### Phase 5: 集成 & Node 重构

| 步骤 | 改动内容 | 预估工作量 |
|------|----------|-----------|
| 5.1 | 定义 `INodeContext` 接口 | 小 |
| 5.2 | 重构 `Node` 结构体，持有所有组件 | 中 |
| 5.3 | 重写 `Node.Start()` / `Node.Stop()` | 中 |
| 5.4 | 改造 `core/Service` 结构体，注入 `INodeContext` | 大 |
| 5.5 | 全局引用扫描 & 替换（`config.Conf` + `log.SysLogger` + 其他） | 大 |
| 5.6 | 更新 `example/` 目录示例代码 | 中 |
| 5.7 | 编写多 Node 集成测试 | 中 |

---

## 六、允许保留的包级变量（白名单）

以下全局状态因属于 **不可变数据、无状态工厂、sync.Pool、或线程安全的只读注册表** 类型，可安全保留为包级变量。除此之外的所有包级 `var` 必须清除：

| 组件 | 变量 | 理由 |
|------|------|------|
| `rpc/message/msgenvelope` | `metaPool`, `msgPool`, `msgEnvelopePool` | sync.Pool 天然并发安全，共享提高复用效率 |
| `rpc/message/msgenvelope` | `metaBorrowTracker`, `metaLeakTrackEnabled*` | 调试用泄漏检测，带锁保护 |
| `actor/mailbox/jobs` | `msgJobPool`, `eventBusJobPool`, `timerJobPool`, `concurrentCallbackJobPool`, `sysCtlJobPool` | sync.Pool 包装器 |
| `actor/mailbox/jobs` | `jobFactory` | init() 后只读的注册表 |
| `actor/mailbox/strategy.go` | `builderMap` | syncx.Map，策略注册表启动时写入，并发安全 |
| `actor/mailbox` | `ErrCircuitBreakerOpen`, `ErrRateLimitExceeded`, `ErrMailboxStopped`, `ErrRWDisableTimeout`, `ErrSentinelBlocked` | 不可变错误哨兵值 |
| `actor/mailbox/sentinel_middleware.go` | `sentinelInitOnce`, `sentinelInitErr` | sync.Once 保护的一次性初始化（Sentinel 基础设施全局一次性加载；如需完全隔离 Sentinel 实例则需改造） |
| `monitor/callstate.go` | `callStatePool`, `callStatePoolOnce` | sync.Pool 包装器 |
| `log/bufferpool.go` | `bufferPool`, `once` | sync.Pool 包装器（内部实现） |
| `log/errors.go` | `ErrRotationTime`, `ErrLevel` | 不可变错误哨兵值 |
| `log/log.go` | `levelMap`, `AllLevelStrs` | 不可变映射表 / 只读 slice |
| `utils/translate` | `languageConf`, `transMap`, `zhCnMap`, `enUsMap` | 只读数据，启动时注册 |
| `utils/validate/engin.go` | `zhT`, `enT`, `translator`, `Validator` | init() 后只读，validator 并发安全 |
| `utils/validate/custom.go` | `phoneReg`, `sm3Reg`, `usernameReg`, `pwdReg` | 不可变编译期正则（`regexp.MustCompile`） |
| `utils/validate/custom.go` | `locales` | 只读 slice |
| `utils/codec` | `codecs`, `typeUrlCache/Mu`, `anyPool` | init() 后只读注册表 + 带锁缓存 + sync.Pool |
| `utils/codec/pool.go` | `bytePoolMgr` | sync.Pool 管理器 |
| `utils/codec/protobuf.go` | `protoDeterministic*` | sync.Once 一次性初始化 |
| `utils/serializer` | `DefaultSerializerID`, `serializers` | init() 后只读 |
| `log/zap_core.go` | `moduleNameOnce`, `moduleName` | sync.Once 读取 go.mod 模块名，同进程不变 |
| `log/zap_core.go` | `stdoutWriteSyncerFactory` | 函数变量，测试可替换，生产环境不变 |
| `profiler/profiler.go` | `DefaultMaxOvertime`, `DefaultOvertime`, `DefaultMaxRecordNum` | 导出默认值常量（建议改为 const 或收入 Config） |
| `utils/network/http_server.go` | `DefaultMaxHeaderBytes` | 导出默认值（建议改为 const 或收入 Config） |
| `utils/emberctx` | `emberHeaderKey`, `traceSeq` | 不可变 key + 原子计数器 |
| `utils/pid` | (无全局变量) | 纯函数 |
| `utils/version` | `Version` | 不可变常量 |
| `utils/bytespool` | `memAreaPoolList` | sync.Pool 包装 |
| `utils/diag` | `enabledOnce`, `enabledCached` | sync.Once 一次性初始化 |
| `utils/network` | `pbPackPool` | sync.Pool |
| `sysModule/gate/ws/processor.go` | `pbPackPool` | sync.Pool 包装器（ws 协议适配器内部） |
| `utils/title` | `titleBase`, `bakUrl` | 不可变字符串 |
| `services` | `serviceMap`, `lock` | 全局服务工厂注册表（`init()` 阶段写入，`Start()` 后只读），有 `sync.RWMutex` 保护。与 `discoveryFactory`/`remoteFactory` 同属"注册工厂函数"模式 |
| `utils/timingwheel` | `cronParser`, spec 解析常量 | 只读解析器 |
| `utils/timingwheel/cron.go` | `places`, `defaults`, `standardParser` | cron 解析只读数据 |
| `utils/timingwheel/task_scheduler.go` | `defaultSeed` | 初始值常量（建议改为 `const`） |
| `def/error.go` | `Err*` 系列 | 不可变 `errors.New()` |
| `def/consts.go` | 常量 | 不可变 |
| `def/mailbox.go` | `RWModeContextKey`, `RWSourceServiceKey` | 不可变 context key |
| `event/category.go` | `defaultClassifications` | 只读 map（init 后不修改） |
| `core/rpc/handler.go` | `emptyError` | 不可变 reflect.Type |
| `discovery` | `discoveryFactory` (改造后) | 无状态工厂函数注册表 |
| `rpc/remote/pool` | `remoteFactory` (改造后) | 无状态工厂函数注册表 |
| protobuf 生成代码 | `*_proto_*` 系列 | 标准 proto 注册机制 |
| 编译期接口断言 | `var _ Interface = (*Type)(nil)` | 零值，编译期检查 |

---

## 七、风险与注意事项

### 7.1 改动波及面

- `config.Conf` 和 `log.SysLogger` 的引用遍布整个代码库，是改动量最大的两个点
- 依靠 **编译器** 来确保无遗漏 — 删除全局变量后，所有引用处都会编译报错
- 建议配合 `go vet` 和 `go build` 逐包修复

### 7.2 全局变量清除检查清单

改造完成后，使用以下命令验证所有包级可变全局变量已清除：

```bash
# 搜索残留的包级 var 声明（排除 const、type、接口断言、error 哨兵值）
grep -rn "^var " engine/pkg/ --include="*.go" | grep -v "_test.go" | grep -v ".pb.go"
```

**允许保留的包级变量**（白名单）:
- `var Err* = errors.New(...)` — 不可变错误哨兵值
- `var *Pool = pool.NewSyncPoolWrapper(...)` — sync.Pool 包装器
- `var *Factory = map[string]func()...` — 无状态工厂注册表（如 `discoveryFactory`, `remoteFactory`）
- `var codecs = map[int32]inf.ICodec{}` — init() 后只读的注册表
- `var serializers []Serializer` — init() 后只读
- `var serviceMap map[string]func() inf.IService` — 全局服务工厂注册表（init 阶段写入，Start 后只读）
- `var Validator *validator.Validate` — init() 后只读，并发安全
- `var cronParser Parser` — 只读解析器
- `var traceSeq atomic.Uint64` — 原子计数器，线程安全
- `var Version string` — 不可变常量
- `var *ContextKey = &contextKeyType{}` — 不可变 context key
- `var jobFactory = map[...]jobEntry{...}` — init() 后只读的 Job 工厂
- `var builderMap = syncx.Map[...]` — 并发安全的策略注册表
- protobuf 生成代码中的 `var` — 标准 proto 注册机制
- `var _ Interface = (*Type)(nil)` — 编译期接口断言

**必须清除的包级变量**（黑名单）:
- 所有 `sync.Once` + 单例指针对（`rpcMonitor`, `bus`, `globalPoolManager`, `SysLogger` 等）
- 所有 `GetXxx()` 全局 getter（`GetCluster()`, `GetEndpointManager()`, `GetRpcMonitor()` 等）
- 所有可变 `map`/`slice`/`struct` 全局变量（`runServices`, `senderHandlerMap`, `poolStates` 等）
- 所有包级 `sync.RWMutex`/`sync.Mutex`（伴随可变状态的）
- `var memDB *gorm.DB` — 全局 DB 连接
- `var jwtSecret []byte` — 硬编码密钥
- `var timeOffset time.Duration` — 可变时间偏移
- `var Conf = new(conf)` — 全局配置
- `var remoteMap = map[string]inf.IRemoteServer{...}` — 实例 map（改为工厂 map）
- `var busPool` / `var busPoolOnce` — PerPPool + sync.Once 内读取 config.Conf（见 §3.21.1）

### 7.3 init() 函数处理

需要删除或改造的 `init()` 函数列表：

| 包 | 文件 | 当前 init() 作用 | 改造方式 |
|----|------|-----------------|----------|
| `services` | `services.go` | 初始化 `serviceMap` | **保留**（全局工厂注册表，`SetService()` 仍为包级函数。仅删除运行时状态 `runServices`/`Daemon` 等） |
| `sysService/pprofservice` | `pprof.go` | 注册 PprofService + ServiceConf | 删除，改为 `RegisterPprofService()` |
| `sysService/dbservice` | `db.go` | 注册 DBService + ServiceConf | 删除，改为 `RegisterDBService()` |
| `profiler` | `profiler.go` | 初始化 `mapProfiler` | 删除，改为 `NewRegistry()` |
| `rpc/client` | `sender.go` | 初始化 sender map + 注册到 pool | 删除，改为 `NewSenderManager()` |
| `utils/codec` | `codec.go` | 注册 JSON/Protobuf codec | 保留（init 后只读，无状态注册表） |
| `utils/validate` | `validate.go` | 创建 validator + 注册翻译 | 保留（init 后只读，并发安全） |
| `utils/serializer` | `serializer.go` | 注册序列化器 | 保留（init 后只读） |
| `utils/timingwheel` | `init.go` | 初始化 `cronParser` | 保留（只读解析器） |
| `discovery/etcd` | `init.go` | 注册 etcd discovery **实例** | 改为注册**工厂函数**（可保留 init） |
| `actor/mailbox` | `strategy.go` | 注册策略 builder | 保留（syncx.Map 并发安全注册表） |
| `actor/mailbox/jobs` | `factory.go` | 初始化 `jobFactory` map | 保留（init 后只读） |
| `utils/translate` | `*.go` | 注册翻译器 | 保留（只读数据） |

> **关键区分**: 注册**实例**的 `init()` 必须删除（如 `sysService`），
> 注册**无状态工厂函数或只读数据**的 `init()` 可以保留（如 `services`（工厂注册表）、`discovery/etcd` 改造后、`codec`、`translate`）。

### 7.4 错误处理模型改造（panic/fatal → error 透传）

#### 7.4.1 现状

当前包级初始化函数在遇到错误时，普遍采用 **直接终止进程** 的策略：

```go
// 典型模式 1: panic
func Init(...) {
    if err != nil {
        panic("xxx init failed: " + err.Error())
    }
}

// 典型模式 2: log.Fatal (内部调用 os.Exit(1))
func Start(...) {
    conn, err := connect(addr)
    if err != nil {
        log.Fatal("connect failed", err)
    }
}
```

在单 Node/单进程模型下这样做尚可接受——初始化失败意味着整个进程无法工作。但在多 Node 改造后，**一个 Node 的初始化失败不应终止整个进程（其他 Node 可能正常运行）**。

#### 7.4.2 改造规则

| 规则 | 说明 |
|------|------|
| **R1: 所有 `New*()`/`Init()`/`Start()` 必须返回 `error`** | 不再使用 `panic` 或 `log.Fatal`。唯一允许 panic 的场景是程序员错误（如 nil 接口断言），不是运行时/配置错误 |
| **R2: `Node.Start()` 统一决策** | 收到 error 后可选择：(a) 立即 return 并回滚已初始化组件；(b) 降级启动（跳过非关键组件并记录 warning） |
| **R3: 组件内部捕获 panic** | `ServiceManager.Init()` / `Start()` 在调用用户 `IService.OnInit()` 等回调时，用 `defer recover()` 包裹，将 panic 转为 error 返回 |
| **R4: 错误包装** | 每一层用 `fmt.Errorf("模块名: %w", err)` 包装，保证最终 error 链可通过 `errors.Is/As` 定位根因 |
| **R5: Close/Stop 不返回 error** | 关闭操作仅记录日志（best-effort），不返回 error，避免关闭链路中断 |

#### 7.4.3 需要改造签名的组件清单

| 组件 | 原签名 | 新签名 |
|------|--------|--------|
| `config` | `func Init(confPath string)` | `func (c *Config) Load(confPath string) error` |
| `log` | `func Init(conf, isDebug)` | `func NewLogger(conf, isDebug) (*Logger, error)` |
| `asynclib` | `func InitAntsPool(size int)` | `func NewPool(size int) (*Pool, error)` |
| `timingwheel` | `func Start(interval, size, logger)` | `func NewTimingWheel(interval, size, logger) (*TimingWheel, error)` |
| `dedup` | `func Init(conf)` (无返回值，无 Close) | `func NewDeDuplicator(conf) (IDeDuplicator, error)` + `IDeDuplicator.Close()` |
| `monitor` | `func (rm *RpcMonitor) Init(conf, tw)` | `func (rm *RpcMonitor) Init(conf, tw, logger) error` |
| `event` | `func (b *Bus) Init(conf)` | `func (b *Bus) Init(conf) error` |
| `cluster` | `func (c *Cluster) Init(ctx)` | `func (c *Cluster) Init(ctx) error` |
| `cluster` | `func (c *Cluster) Start()` | `func (c *Cluster) Start() error` |
| `services` | `func Init()` / `func Start()` | `func (m *ServiceManager) Init() error` / `Start() error` |
| `client` | `func NewSenderManager(pm, logger)` 无 error | `func NewSenderManager(pm, logger) (*SenderManager, error)` |
| `memdbx` | `func Start(models)` | `func NewMemDB(models) (*MemDB, error)` |
| `HookFun` | `func(extra map[any]any)` | `func(ctx INodeContext, extra map[any]any) error` |

> **小结**: 所有从 `init()` / 包级函数迁移到实例方法的场景，签名一律加 `error` 返回值。
> 这与 §2.2 原则 5 和 §2.3 `Start()` 中的 `cleanups` 回滚机制配套。

#### 7.4.4 用户 Service 回调的防护

用户实现的 `IService.OnInit()` / `OnStart()` 等回调可能 panic。`ServiceManager` 在调用时统一包裹：

```go
func (m *ServiceManager) safeCall(name string, fn func() error) (err error) {
    defer func() {
        if r := recover(); r != nil {
            err = fmt.Errorf("service %q panic: %v\n%s", name, r, debug.Stack())
        }
    }()
    return fn()
}

// 调用示例
if err := m.safeCall(svc.Name(), func() error { return svc.OnInit() }); err != nil {
    return fmt.Errorf("service init %q: %w", svc.Name(), err)
}
```

#### 7.4.5 panic 仅保留场景（白名单）

| 场景 | 理由 |
|------|------|
| 接口断言失败 (`v.(Type)`) | 编码错误，应立刻暴露 |
| `must*` 辅助函数（如 `regexp.MustCompile`） | 编译期常量，不可能运行时失败 |
| 检测到不可恢复的内部状态不一致 | 继续运行会导致数据损坏 |

> 除白名单以外，所有 `panic()`/`log.Fatal()`/`os.Exit()` 调用必须在改造中移除。
> 改造完成后可用以下命令扫描残留：
> ```bash
> grep -rn 'panic(\|log\.Fatal\|os\.Exit' engine/pkg/ --include="*.go" | grep -v "_test.go" | grep -v ".pb.go" | grep -v "must"
> ```

### 7.5 性能考量

- 多个 Node 各自持有独立的时间轮、协程池等，内存开销会增加
- 对象池（sync.Pool）继续共享，避免重复分配
- 每个 Node 的协程池大小可以适当调小，总量不超过原来的全局池大小

### 7.6 测试策略

- 每个 Phase 完成后运行 `go build ./...` 和 `go vet ./...` 确保编译通过
- Phase 5 需要新增多 Node 集成测试：
  ```go
  func TestMultiNode(t *testing.T) {
      node1, _ := node.New().Start(
          node.WithConfPath("configs/node1"),
      )
      node2, _ := node.New().Start(
          node.WithConfPath("configs/node2"),
      )
      defer node1.Stop()
      defer node2.Stop()
      
      // 验证两个 Node 独立运行
      // 验证跨 Node RPC 通信
      // 验证各自的服务路由独立
      // 验证 node1.Stop() 不影响 node2
  }
  ```

### 7.7 example/ 目录更新

所有示例代码需要同步更新：

**`example/node*/main.go` 启动方式变更**:

```go
// 改前 (example/node_local/main.go)
func main() {
    services.SetService("Service1", func() inf.IService { return &comm.Service1{} })
    services.SetService("Service2", func() inf.IService { return &comm.Service2{} })
    node.Start(
        node.WithConfPath("configs/node_local"),
        node.WithVersion("1.0.0"),
    )
}

// 改后
// 服务工厂注册仍通过包级 init() / import 完成（全局共享注册表，
// 实际启动哪些服务由各 Node 的配置文件决定）
func init() {
    services.SetService("Service1", func() inf.IService { return &comm.Service1{} })
    services.SetService("Service2", func() inf.IService { return &comm.Service2{} })
}

func main() {
    n, err := node.New().Start(
        node.WithConfPath("configs/node_local"),
        node.WithVersion("1.0.0"),
    )
    if err != nil { panic(err) }
    defer n.Stop()
    
    // 等待信号...
    sig := make(chan os.Signal, 1)
    signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
    <-sig
}
```

**`example/comm/test_service*.go` 日志方式变更**:

```go
// 改前
log.SysLogger.Info("something")

// 改后（Service 嵌入了 *log.Logger）
s.Info("something")
```

**多 Node 示例（新增）**: 改造完成后应新增 `example/node_multi/main.go`，演示单进程多 Node。

- 这是验证改造是否完整的**最佳试金石**
