# EmberEngine Multi-Node Phase 3 修复清单

> 来源：2026-03-04 代码审查，基于 `DESIGN_MULTI_NODE.md` 完成状态核查。
> 构建基准：`go build ./...` EXIT=0
> 最后更新：2026-03-05（九次复查：P3-14 已修复，`go test ./...` 全部 PASS；新增 P3-16 data race 阻断、P3-17 锁范围中等）

---

## 问题汇总

| 编号 | 级别 | 问题 | 状态 |
|------|------|------|------|
| P3-01 | 🔴 高 | RPC 子链 6 个包存在包级可变全局状态，多 Node 下互相覆盖 | ✅ 已完成 |
| P3-02 | 🟡 中 | `IService.GetLoggerX()` 与 `GetLogger()` 重复，过渡方法未清理 | ✅ 已完成 |
| P3-03 | ⛔ 阻断 | `errorlib.Is(int)` 签名违反 `errors` 包约定，`errors.Is()` 绕过，按码匹配完全失效 | ✅ 已完成 |
| P3-04 | ⛔ 阻断 | `bus_benchmark_test.go` 按值复制含 `noCopy` 的 `atomic.Uint64`，go vet 报错 | ✅ 已完成 |
| P3-05 | 🔴 高 | `example/node1` 无缓冲 signal channel，`signal.Notify` 要求有缓冲，信号可能丢失 | ✅ 已完成 |
| P3-06 | 🔴 高 | `timingwheel` 测试 10 处 `NewJobScheduler` 单值接收（API 已改 2 返回值），测试套件编译失败 | ✅ 已完成 |
| P3-07 | 🔴 高 | `monitor_add_test.go` 测试桩 `*inlineDispatcher` 缺少 `DeliverRequest`，接口不满足 | ✅ 已完成 |
| P3-08 | 🔴 高 | `service.go.rollbackInitResources` 对 `ILoggerX` 做具体类型断言，Mock/Wrapper 场景 logger 泄漏 | ✅ 已完成 |
| P3-09 | 🔴 高 | `core/rpc/handler.go` 方法注册失败静默 continue，服务以不完整方法集启动 | ✅ 已完成 |
| P3-10 | 🟡 中 | `pool/manager.go` 未导出字段 `circuitState` 带 json tag，tag 对 `json.Marshal` 无效 | ✅ 已完成 |
| P3-11 | 🟡 中 | `mpsc` 和 `ring` 测试 goroutine 内调用 `t.FailNow()`，行为未定义，可能死锁 | ✅ 已完成 |
| P3-12 | 🟡 中 | `example/comm/test_service2.go:104` 不可达代码，go vet 报错 | ✅ 已完成 |
| P3-13 | 🟡 中 | `node.go` 通过 `SetDebug()` 向 5 个包的包级 `runtimeDebug` 写入，多 Node 下后者覆盖前者 | ✅ 已完成 |
| P3-14 | ⛔ 阻断 | `monitor.Add` 与 `etcd watchLoop/syncInitialState` 无 logger nil 守卫，裸结构测试时 nil pointer panic，`go test` FAIL | ✅ 已完成 |
| P3-15 | 🔴 高 | `msgbus.asyncCall` 中 `go func() { state.Wait() }()` 永不退出，每次 `AsyncCall` 泄漏一个 goroutine | ✅ 已完成 |
| P3-16 | ⛔ 阻断 | `monitor.go` `Stop()`/`listen()`/`Remove()` 三处数据竞争：`rm.sd` 字段无锁读写，`-race` 确认 FAIL | ⏳ 未完成 |
| P3-17 | 🟡 中 | `services.Init()` 在 `lock.RLock()` 持有期间执行所有 `svc.Init()`，若 `Init()` 内触发 `SetService()` 将死锁 | ⏳ 未完成 |

---

## P3-01 RPC 子链包级可变全局状态

### 问题描述

`node.go` 的 `Start()` 中通过 `Set*` 系列函数将 Node 实例级资源（logger、rpcMonitor、deduplcator、natsConf）写入各 RPC 子包的**包级变量**。这是进程级共享状态：

- 启动第二个 Node 时其 `Set*` 调用会覆盖第一个 Node 的值，两个 Node 却共用同一份 `rpcMonitor`/`logger`。
- `sync.Once` 兜底行为使得第一个 `Set*` 调用永久生效，后续 Node 的设置静默失效。
- 上述行为完全破坏"单进程多 Node 资源隔离"核心目标。

### 涉及文件

#### 1. `rpc/remote/handler/handler.go` ✅ 已完成

包级变量已全部删除。`Handler` 结构体已实现，持有 `rpcMonitor`/`logger`/`dedup` 三个字段，由 `NewHandler()` 构造并经 `Cluster.Init()` → `Remote.Init()` 注入到 `gr/rx/nt` 各 listener。

---

#### 2. `rpc/message/msgbus/bus.go` ✅ 已完成

**已完成部分**：
- `MessageBusFactory` 结构体已创建，持有 `pool`/`logger`/`rpcMonitor`/`rpcTimeout`
- `node.go` 已通过 `NewMessageBusFactory()` 构造并赋值 `n.BusFactory`，传入 `Cluster.Init()`
- `MessageBus` 持有 `factory *MessageBusFactory` 字段

`MessageBus` 方法体已切换到 `mb.factory` 路径，包级可变状态入口已移除；
`bus_benchmark_test.go` 也已从 `SetRpcMonitor()` 切换为 `MessageBusFactory` 注入。

---

#### 3. `rpc/client/sender.go` ✅ 已完成

包级变量 `clientLogger`/`clientNatsConf`/`clientLoggerOnce` 已全部删除。`SenderManager` 构造函数签名已升级为 `NewSenderManager(poolMgr, logger, rpcMonitor, natsConf)`，`node.go` 通过构造参数传入，不再调用任何 `Set*`。

---

#### 4. `rpc/client/sender_local.go` ✅ 已完成

包级变量 `rpcMonitorProvider` 和 `SetRpcMonitor()` 已删除。`localSender` 持有 `rpcMonitor` 字段，`newLClient(_, rm)` 注入，由 `Dispatcher.getSender()` 从 `SenderManager.rpcMonitor` 传入。

---

#### 5. `rpc/client/pool/manager.go` ✅ 已完成

包级变量 `poolLogger`/`poolLoggerOnce` 已删除。`PoolManager` 通过 `NewPoolManager(logger)` 注入，内部 `PoolConnection` 由 `NewPoolConnection(id, sender, logger)` 接收 logger 字段，不再依赖包级 getter。

---

#### 6. `rpc/remote/nt/server.go` ✅ 已完成

包级变量 `natsConfProvider` 已删除。`natsServer` 持有 `natsConf`/`handler` 实例字段，`SetNatsConf()`/`SetHandler()` 均为实例方法，由 `Remote.Init()` 经 `loggerAwareRemoteServer` 接口调用注入。

---

### node.go 改动汇总

**已完成**：原有的 9 个 `Set*` 调用已全部从 `node.go` 中删除，改为构造参数传入：

```go
// 当前 node.go（已无任何包级 Set* 调用）
n.SenderMgr = client.NewSenderManager(n.PoolManager, n.Logger, n.RpcMonitor, natsConf)
remoteMsgHandler := remotehandler.NewHandler(n.RpcMonitor, n.Logger, n.DeDuplicator)
n.BusFactory = msgbus.NewMessageBusFactory(
    n.Config.NodeConf.BusPoolSize, n.Logger, n.RpcMonitor, n.Config.GetDefaultRpcTimeout(),
)
n.Cluster.Init(n.Config.ClusterConf, n.Logger, n.SenderMgr, remoteMsgHandler, natsConf, n.BusFactory)
```

**仍需完成**：`BusFactory` 虽已传入 `Cluster.Init()`，但 `MessageBus` 方法体内部还未使用 `mb.factory`，包级全局状态路径仍然活跃（见 §2 msgbus 半成品说明）。

---

### 剩余工作

仅剩 `msgbus/bus.go` 方法体迁移，集中在一个文件内，预计半天完成：

1. `MessageBus.getLogger()` 改为 `return mb.factory.logger`
2. 所有 `requireRpcMonitor(ctx)` 调用改为 `mb.getRpcMonitorOrErr(ctx)`（新增实例方法读 `mb.factory.rpcMonitor`）
3. 所有 `getBusPool().Get()` 改为 `mb.factory.pool.Get()`（或保持通过 `factory.New()` 统一入口）
4. 所有 `getBusPool().Put(mb)` 改为 `mb.factory.Put(mb)`
5. 删除包级变量/函数（见上方待删除列表）
6. 修复 `bus_benchmark_test.go` 中的 `SetRpcMonitor()` 调用，改为构造 `MessageBusFactory`

---

## P3-02 `IService.GetLoggerX()` 过渡方法冗余 ✅ 已完成

`ILogger` 接口中 `GetLoggerX()` 声明已删除，`core/service.go` 和 `core/module.go` 中对应实现已删除。
全仓搜索 `GetLoggerX`：仅本文档历史记录命中，`.go` 文件 **0 处命中**。`go build ./...` EXIT=0。

---

## P3-03 `errorlib.Is()` 签名违反 `errors` 包约定 ⛔ 阻断

> 来源：`go vet ./...` 2026-03-05

**文件**：`engine/pkg/utils/errorlib/errors.go:21,61`

```go
// 当前：go vet 报错，errors.Is() 永远不会调用此方法
type CError interface {
    Is(int) bool   // ← 错误签名
}
func (e *ErrCode) Is(code int) bool { return e.Code == code }
```

`errors.Is(err, target)` 要求 target 实现 `Is(error) bool`，此处签名为 `Is(int) bool`，接口协议完全不兼容，`errors.Is` 运行时绕过此方法，库的按码匹配设计彻底失效。

**修复**：将自定义匹配方法重命名为 `IsCode(code int) bool`，同步更新 `CError` 接口声明及所有调用侧。

---

## P3-04 `bus_benchmark_test.go` 按值复制 `atomic.Uint64` ⛔ 阻断

> 来源：`go vet ./...` 2026-03-05

**文件**：`engine/pkg/rpc/message/msgbus/bus_benchmark_test.go:161-163`

```go
// 当前：按值复制含 noCopy 的 atomic.Uint64，go vet 报错
_ = callCount
_ = asyncCount
_ = sendCount

// 修复
_ = &callCount
_ = &asyncCount
_ = &sendCount
```

---

## P3-05 无缓冲 Signal Channel 🔴 高

> 来源：`go vet ./...` 2026-03-05

**文件**：`example/node1/main.go:29`（其余 example 节点应同步检查）

```go
// 当前：go vet 报错，信号可能丢失
var exitCh = make(chan os.Signal)

// 修复
var exitCh = make(chan os.Signal, 1)
```

`signal.Notify` 文档明确：channel 必须有缓冲（至少 1），否则 runtime 可能在 channel 未就绪时丢弃信号。

---

## P3-06 `timingwheel` 测试 `NewJobScheduler` 返回值不匹配 🔴 高

> 来源：`go vet ./...` 2026-03-05

**文件**：`engine/pkg/utils/timingwheel/*.go`（10 处测试文件）

`NewJobScheduler` API 已改为返回 `(IJobScheduler, error)`，但所有测试文件仍以单值接收，导致整个 timingwheel 测试套件编译失败，CI 全红。

**修复**：

```go
sch, err := timingwheel.NewJobScheduler(...)
if err != nil {
    t.Fatal(err)
}
```

---

## P3-07 `monitor` 测试桩缺少 `DeliverRequest` 接口方法 🔴 高

> 来源：`go vet ./...` 2026-03-05

**文件**：`engine/pkg/monitor/monitor_add_test.go:137`

`*inlineDispatcher` 未实现 `interfaces.IRpcDispatcher` 要求的 `DeliverRequest` 方法，go vet 报接口不满足。

**修复**：为 `inlineDispatcher` 补充空实现：

```go
func (d *inlineDispatcher) DeliverRequest(ctx context.Context, env inf.IEnvelope) error {
    return nil
}
```

---

## P3-08 `rollbackInitResources` 对接口做具体类型断言 🔴 高

**文件**：`engine/pkg/core/service.go`

```go
// 当前：接口化后反向类型断言，Mock/Wrapper 场景 logger 泄漏
if concrete, ok := s.logger.(*log.Logger); ok {
    log.Release(concrete)
}
```

接口化完成后通过 `.(concrete)` 回到具体类型是反模式：若 logger 是测试 Mock 或 Wrapper，此分支永远不执行，logger 实例泄漏，同时 `core` 包重新强依赖具体 `log.Logger`。

**修复**：在 `ILoggerX` 或单独 `IReleasable` 接口上声明 `Release()` 方法，由工厂层实现，上层只调用接口，不做类型断言。

---

## P3-09 RPC 方法注册失败静默忽略 🔴 高

**文件**：`engine/pkg/core/rpc/handler.go`

```go
// 当前：注册失败只打日志，服务以不完整方法集继续启动
h.Errorf("register rpc method failed: %v", err)
continue
```

服务启动后部分 RPC 接口不可用，调用侧收到"方法不存在"错误，难以与配置问题区分，排查成本极高。

**修复**：`registerMethod()` 改为返回 error，`Init()` 向上传递，让服务在注册失败时明确失败：

```go
func (h *Handler) registerMethod() error {
    for m := 0; m < typ.NumMethod(); m++ {
        if err := h.suitableMethods(typ.Method(m)); err != nil {
            return fmt.Errorf("register method %s: %w", typ.Method(m).Name, err)
        }
    }
    return nil
}
```

---

## P3-10 未导出字段带 json tag 无效 🟡 中

> 来源：`go vet ./...` 2026-03-05

**文件**：`engine/pkg/rpc/client/pool/manager.go:51`

```go
circuitState CircuitBreakerState `json:"circuit_state"` // 未导出，tag 无效
```

`json.Marshal` 忽略未导出字段，tag 形同废纸，且误导阅读者认为此字段会被序列化。

**修复**：导出字段（`CircuitState`）或删除 json tag。

---

## P3-11 goroutine 内调用 `t.FailNow()` 🟡 中

> 来源：`go vet ./...` 2026-03-05

**文件**：`engine/pkg/utils/mpsc/deque_test.go:83`、`engine/pkg/utils/ring/ring_test.go:83`

```go
go func() {
    t.FailNow() // ← 仅能在测试 goroutine 调用，此处行为未定义，可能死锁
}()
```

`t.FailNow()` 通过 `runtime.Goexit()` 终止当前 goroutine，在非测试 goroutine 中调用行为未定义。

**修复**：改为 `t.Error()` + channel 信号，由主 goroutine 调用 `t.FailNow()`。

---

## P3-12 不可达代码 🟡 中

> 来源：`go vet ./...` 2026-03-05

**文件**：`example/comm/test_service2.go:104`

删除或修正控制流，消除 go vet 报告的不可达代码。

---

## P3-13 `runtimeDebug` 包级写入未隔离 🟡 中 ✅ 已完成（方案 B）

> 来源：2026-03-05 代码审查

**文件**：`engine/pkg/node/node.go`（写入侧）；涉及以下 5 个包（持有侧）：

| 包 | 文件 | 包级变量（修复后） |
|----|------|-------------------|
| `actor/mailbox/job` | `job_factory.go:17` | `var runtimeDebug atomic.Bool` |
| `utils/codec` | `pool.go:16` | `var runtimeDebug atomic.Bool` |
| `rpc/message/msgenvelope` | `debug.go:3` | `var runtimeDebug atomic.Bool` |
| `monitor` | `call_state.go:52` | `var runtimeDebug atomic.Bool` |
| `cluster/discovery/etcd` | `discovery.go:30` | `var runtimeDebug atomic.Bool` |

**问题描述**：`node.go` 的 `Start()` 中通过以下 5 行向各包的包级变量注入调试开关：

```go
job.SetDebug(n.Config.IsDebug())
codec.SetDebug(n.Config.IsDebug())
msgenvelope.SetDebug(n.Config.IsDebug())
monitor.SetDebug(n.Config.IsDebug())
etcddiscovery.SetDebug(n.Config.IsDebug())
```

这是进程级共享状态，与「单进程多 Node 资源隔离」核心目标冲突：

- 若两个 Node 的 `IsDebug()` 不同，后启动的 Node 会覆盖先启动 Node 的调试设置。
- 包级 bool 无同步保护（大多数包直接读 `runtimeDebug`），存在数据竞争（`-race` 下会报告）。

**影响评估**：仅影响诊断输出（日志详细程度），不影响业务逻辑。在所有生产节点 `IsDebug()` 返回相同值的典型部署中无实际副作用。

**修复结果**：采用**方案 B（最小改动）**，已完成以下改造：

- `actor/mailbox/job/job_factory.go`：`runtimeDebug` 改为 `atomic.Bool`，`SetDebug` 改为 `Store`，读取改为 `Load`
- `utils/codec/pool.go`：`runtimeDebug` 改为 `atomic.Bool`，读取/写入统一 `Load/Store`
- `rpc/message/msgenvelope/debug.go`：`runtimeDebug` 改为 `atomic.Bool`，`isDebug()` 改为 `Load()`
- `monitor/call_state.go`：`runtimeDebug` 改为 `atomic.Bool`，pool recorder 分支改为 `Load()`
- `cluster/discovery/etcd/discovery.go`：`runtimeDebug` 改为 `atomic.Bool`，etcd zap logger 分支改为 `Load()`

语义上保留“进程级诊断开关（非业务状态）”共享行为，但已消除并发读写数据竞争。

**备选修复方向（未采用）**：

**方案 A（推荐）— 改为构造参数传入**：去除包级 `runtimeDebug` 变量，将调试开关作为各组件构造函数参数存储为实例字段，`node.go` 不再调用任何 `SetDebug()`。

**方案 B（最小改动）— atomic 读写 + 文档豁免**：将包级 `var runtimeDebug bool` 改为 `var runtimeDebug atomic.Bool`，消除数据竞争；接受多 Node 共享调试开关的语义，写入文档豁免为"进程级诊断开关（非业务状态），允许共享"。

---

## P3-14 `ILoggerX` 嵌入结构体无 nil 守卫导致测试 panic ✅ 已完成

> 来源：`go test ./engine/...` 2026-03-05；修复：2026-03-05

### 涉及文件与失败测试

| 包 | 测试函数 | panic 位置 |
|----|---------|-----------|
| `engine/pkg/monitor` | `TestRpcMonitorAdd_WhenSchedulerFails_CallDoesNotHang` | `monitor.go:273` `rm.WithContext(state.ctx).Errorf(...)` |
| `engine/pkg/cluster/discovery/etcd` | `TestWatchLoopPushesEvents` | `discovery.go:166` `e.Infof(...)` / `discovery.go:217` `e.Infof(...)` |

### 根因

两个测试都绕过了 `Init()`（为避免真实 etcd/timewheel 依赖），直接构造裸结构体，没有设置嵌入的 `ILoggerX`。当执行路径触发日志调用时，`nil.ILoggerX` 导致 nil pointer dereference。

- **`monitor.go:273`**：`RpcMonitor.Add()` 在 scheduler 返回错误时调用 `rm.WithContext(...).Errorf(...)` — `rm.ILoggerX` 为 nil
- **`discovery.go:166/217`**：`syncInitialState`/`watchLoop` 的第一条日志调用 `e.Infof(...)` — `e.ILoggerX` 为 nil

### 修复结果（方案 A — 测试侧注入真实 logger）

`monitor_test.go` / `monitor_add_test.go` 的 `newTestRpcMonitor()` 已注入真实 logger；`discovery_test.go` 裸结构初始化已补充 `ILoggerX` 赋值。

```
go test ./engine/pkg/monitor/... ./engine/pkg/cluster/discovery/etcd/... -v
--- PASS: TestRpcMonitorAdd_WhenSchedulerFails_CallDoesNotHang
--- PASS: TestWatchLoopPushesEvents
ok  engine/pkg/monitor
ok  engine/pkg/cluster/discovery/etcd
```

`go test ./...` 所有包全部 PASS。

---

## P3-15 `asyncCall` goroutine 永久泄漏 ✅ 已完成

> 来源：2026-03-05 代码审查；修复：2026-03-05

**文件**：`engine/pkg/rpc/message/msgbus/bus.go:433`

### 问题描述

`asyncCall` 内部在请求发送成功后启动一个裸 goroutine：

```go
go func() {
    state.Wait()
}()
```

`state.Wait()` 阻塞于 `<-state.done`（缓冲 channel，size=1）。`state.done` 仅通过 `state.signalDone()` 写入，而 `signalDone()` 只在 `Complete()` 的**同步 Call 路径**调用：

```go
func (s *CallState) Complete() {
    if s.NeedCallback() {
        // 异步路径：post job → return   ← 无 signalDone()
        return
    }
    s.signalDone()  // ← 仅同步路径到达
}
```

所有 `asyncCall` 调用时 `NeedCallback() == true`（有回调），故：

| 场景 | `signalDone()` 是否被调用 | goroutine 是否退出 |
|------|--------------------------|-------------------|
| 正常成功响应（sender_local） | ❌ | ❌ 永久泄漏 |
| 超时（monitor timer 触发 `Complete`） | ❌（走 callback 分支） | ❌ 永久泄漏 |
| monitor 关闭（`isClosed` 触发 `Complete`） | ❌（走 callback 分支） | ❌ 永久泄漏 |

**结论：每次 `AsyncCall` 泄漏一个 goroutine，直到进程退出。**

### 修复方案

**方案 A（推荐）— 直接删除该 goroutine**：该 goroutine 在 goroutine 退出后不执行任何操作，是无意义的死代码，直接移除：

```go
// 删除以下 5 行
go func() {
    state.Wait()

}()
```

**方案 B — 补充 `signalDone()` 调用**：在 `Complete()` 的 callback 分支末尾也调用 `signalDone()`，让 goroutine 正常退出（但 goroutine 本身仍无意义，不如方案 A 简洁）。

### 修复记录

采用方案 A，删除 `bus.go` 中的 4 行裸 goroutine，并替换为说明注释：

```go
// AsyncCall 路径：state 生命周期由 Complete() 管理。
// Complete() 在 async 分支中直接 Put(s) 归还池，不调用 signalDone()，
// 因此此处不得 Wait/Release，否则会导致 goroutine 泄漏和 double-put 池损坏。
return reqId, nil
```

额外发现：超时触发后 state 已被 `getCallStatePool().Put(s)` 归还，若保留旧 goroutine，其被新请求复用的 `done` channel 意外唤醒后再次 `Release()`，导致 **double-put 池损坏**。方案 A 同时消除了该风险。

`go build ./...` EXIT=0。

---

---

## P3-16 `monitor.go` 数据竞争 ⛔ 阻断

> 来源：`go test -race ./engine/pkg/monitor/...` 2026-03-05

**文件**：`engine/pkg/monitor/monitor.go`

### 竞争路径（`-race` 输出确认）

```
Write at Stop()  line 188: rm.sd = nil
Read  at listen() line 205: rm.sd.GetTimerCbChannel()   // select 每轮重新求值
```

### 三处并发缺陷

**① `listen()` 每轮循环重新读 `rm.sd` 字段（无锁）**

`select` 的 channel 表达式在每次迭代都被重新求值，`rm.sd.GetTimerCbChannel()` 中的 `rm.sd` 读取与 `Stop()` 对 `rm.sd` 的写入存在竞争。

**② `Remove()` TOCTOU**

```go
// 当前：nil-check 与调用之间 Stop() 可插入 rm.sd = nil
if rm.sd != nil && !rm.isClosed() {
    rm.sd.CancelTimer(state.timerId())
}
```

`rm.sd` 在 nil 检查通过后、`CancelTimer` 调用前可被 `Stop()` 置 nil，导致 nil dereference。

**③ `Stop()` 未等待 `listen()` goroutine 退出**

`Stop()` 在执行 `rm.sd.Stop()` / `rm.sd = nil` 后直接返回，但 `listen()` goroutine 可能仍在运行（持有 `rm.wg`）。`Node.Stop()` 随即停止 TimingWheel 和 Pool，而 `listen()` 仍在使用它们。

### 修复方案

```go
// ① listen() — 进入循环前一次性捕获 channel，不再重复读字段
func (rm *RpcMonitor) listen() {
    defer rm.wg.Done()
    ch := rm.sd.GetTimerCbChannel() // 捕获一次
    wg := sync.WaitGroup{}
    defer func() { rm.Infof("rpc monitor listen stop") }()
    defer wg.Wait()
    for {
        select {
        case t, ok := <-ch:     // 使用本地变量 ch
            ...
        case <-rm.ctx.Done():
            return
        }
    }
}

// ② Stop() — 删除 rm.sd = nil，改为先 Wait goroutine 再清空 buckets
func (rm *RpcMonitor) Stop() {
    if !rm.closed.CompareAndSwap(false, true) { return }
    rm.cancel()
    if rm.sd != nil {
        rm.sd.Stop()
        // 不再写 rm.sd = nil，消除写-读竞争
    }
    rm.wg.Wait() // 等 listen() 退出后再清空
    for _, bucket := range rm.buckets {
        bucket.Clear()
    }
}

// ③ Remove() — 用局部变量保存 sd，消除 TOCTOU
func (rm *RpcMonitor) Remove(seqId uint64) *CallState {
    state := rm.remove(seqId)
    if state != nil {
        if sd := rm.sd; sd != nil && !rm.isClosed() {
            sd.CancelTimer(state.timerId())
        }
    }
    return state
}
```

---

## P3-17 `services.Init()` 持有 RLock 时间过长 🟡 中

> 来源：2026-03-05 代码审查

**文件**：`engine/pkg/services/services.go:85`

### 问题描述

```go
func (sm *ServiceManager) Init(serviceConf *config.ServiceConf) error {
    lock.RLock()
    defer lock.RUnlock()            // ← 包住了所有 svc.Init() 调用
    for _, initConf := range ... {
        svc.Init(svc, initConf, cfg) // 可能耗时甚至间接调用 SetService()
    }
}
```

- `SetService()` 需要 `lock.Lock()`，若任意 `svc.Init()` 路径触发 `SetService()`（如动态注册子服务）即发生**死锁**。
- 即使当前无此场景，长时间持有 `RLock` 也会阻塞所有并发 `SetService()` 调用（如 `init()` 阶段仍在注册的包）。

### 修复方案

在锁内只读取 map，拷贝到本地 slice 后立即释放，再在锁外执行 `svc.Init()`：

```go
func (sm *ServiceManager) Init(serviceConf *config.ServiceConf) error {
    type entry struct {
        conf    inf.ServiceInitConf
        builder func() inf.IService
    }
    var entries []entry
    lock.RLock()
    for _, initConf := range serviceConf.StartServices {
        b, ok := serviceMap[initConf.ClassName]
        if !ok {
            lock.RUnlock()
            return fmt.Errorf("service[%s] not registered", initConf.ClassName)
        }
        entries = append(entries, entry{initConf, b})
    }
    lock.RUnlock() // 提前释放，后续 svc.Init() 在锁外执行

    for _, e := range entries {
        svc := e.builder()
        ...
        if err := svc.Init(svc, e.conf, cfg); err != nil {
            return fmt.Errorf("init service[%s] failed: %w", e.conf.ClassName, err)
        }
        sm.runServices = append(sm.runServices, svc)
    }
    return nil
}
```

---

## 当前剩余任务

1. **P3-16（阻断）**：修复 `monitor.go` 中 `listen()`/`Stop()`/`Remove()` 三处数据竞争（`go test -race` 已确认）。
2. **P3-17（中）**：缩短 `services.Init()` 中 `RLock` 持有范围，消除潜在死锁。
3. **集成验证**：单进程双 Node（不同 NodeId/端口/配置）并行启动，验证资源隔离与互不覆盖。

---

## 完成标准

### 已达成
- [x] `go build ./...` EXIT=0
- [x] `go vet ./...` EXIT=0
- [x] `node.go` 中 `Set*` 包级注入调用 **0 命中**
- [x] 全仓 `.go` 文件 `GetLoggerX` **0 命中**
- [x] `rpc/remote/handler`、`rpc/client/sender`、`rpc/client/sender_local`、`rpc/client/pool`、`rpc/remote/nt` 包级可变状态 **0 残留**

### 待达成（阻断 / 高）
- [x] `go test ./...` 全部 **PASS**（P3-14 已修复）
- [x] `errorlib.Is()` 签名修正为 `IsCode(int) bool`（P3-03）
- [x] `bus_benchmark_test.go` atomic 复制修复（P3-04）
- [x] `rpc/message/msgbus` 包内包级可变变量 **0 残留**，`mb.factory` 完全接管（P3-01 msgbus）
- [x] `example/node*` signal channel 全部加缓冲（P3-05）
- [x] `timingwheel` 测试 `NewJobScheduler` 返回值对齐，测试套件编译恢复（P3-06）
- [x] `monitor` 测试桩补全 `DeliverRequest`（P3-07）
- [x] `rollbackInitResources` 类型断言替换为接口方法（P3-08）
- [x] 方法注册失败向上返回 error，不再静默 continue（P3-09）
- [x] `monitor.Add`/`etcd watchLoop/syncInitialState` logger nil 守卫，消除 `go test` panic（P3-14）
- [x] `asyncCall` 中泄漏 goroutine 删除（P3-15）
- [ ] `monitor.go` `listen()`/`Stop()`/`Remove()` 三处数据竞争修复，`go test -race` 通过（P3-16）

### 待达成（中）
- [x] `pool/manager.go` `circuitState` json tag 修正（P3-10）
- [x] goroutine 内 `t.FailNow()` 修正（P3-11）
- [x] `test_service2.go` 不可达代码清除（P3-12）
- [x] `runtimeDebug` 包级写入隔离或 atomic 加固（P3-13）
- [ ] `services.Init()` 缩短 `RLock` 持有范围，消除潜在死锁（P3-17）
- [ ] 单进程启动两个独立 Node（不同 NodeId/端口/配置）互不干扰的集成验证

### 集成验证记录（2026-03-05）

- 已尝试并行启动 `example/node1` 与 `example/node2`。
- `node1` 在当前环境阻塞于 etcd：`context deadline exceeded`（`192.168.145.188:2379` 不可达）。
- `node2` 在当前配置阻塞于基础字段缺失：`NodeType为必填字段`。
- 结论：本轮代码层修复已完成，集成验证受环境与配置前置条件影响，待补齐本地 etcd/nats 依赖及 `node2` 配置后复测。
