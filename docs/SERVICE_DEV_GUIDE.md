# EmberEngine Service 开发指南

本指南面向需要在 EmberEngine 上开发业务逻辑的工程师，覆盖 Service 生命周期、RPC 调用、事件系统、定时器、Module 和高级特性。

---

## 目录

- [1. Service 生命周期](#1-service-生命周期)
- [2. 注册 Service](#2-注册-service)
- [3. RPC Handler](#3-rpc-handler)
- [4. 调用其他 Service](#4-调用其他-service)
- [5. Module 子模块](#5-module-子模块)
- [6. 事件系统](#6-事件系统)
- [7. 定时器](#7-定时器)
- [8. 读写分离模式](#8-读写分离模式)
- [9. 日志最佳实践](#9-日志最佳实践)
- [10. 自定义业务配置](#10-自定义业务配置)

---

## 1. Service 生命周期

```text
Init → OnInit → Start → OnStart → 注册服务 → OnStarted → Ready → [运行中] → Stop → OnRelease
```

`OnInit`、`OnStart`、`OnStarted` 均返回 `error`；初始化或启动失败会中止相应流程。服务在 `OnStarted` 成功后才进入可被常规路由选择的 Ready 状态，不代表此时所有其他服务都已就绪。

| 阶段 | 回调方法 | 用途 |
|------|----------|------|
| 初始化 | `OnInit() error` | 注册 RPC Handler、定时器、Module、事件订阅 |
| 启动前 | `OnStart() error` | Mailbox 已启动，执行本服务的启动准备工作；回调同步执行 |
| 启动后 | `OnStarted() error` | 本服务已注册，完成 Ready 前的准备工作 |
| 释放 | `OnRelease()` | 清理资源、关闭连接 |

```go
type MyService struct {
    core.Service
}

func (s *MyService) OnInit() error {
    // 在这里注册定时器、事件订阅等
    return nil
}

func (s *MyService) OnRelease() {
    // 清理资源
}
```

---

## 2. 注册 Service

在 `init()` 或 `main()` 中注册服务工厂：

```go
import (
    inf "github.com/njtc406/emberengine/v2/engine/pkg/interfaces"
    "github.com/njtc406/emberengine/v2/engine/pkg/services"
)

func init() {
    services.SetService("MyService", func() inf.IService {
        return &MyService{}
    })
}
```

配置文件中引用：

```yaml
ServiceConf:
  StartServices:
    - ClassName: MyService        # 对应 SetService 的 key
      ServiceName: MyService      # RPC 调用时使用的名称
      Type: game
      Partition: "1"
```

---

## 3. RPC Handler

Service 和 Module 的公开方法按前缀自动注册，调用时使用完整方法名（不去掉前缀）：

```go
// RpcSum 注册为 "RpcSum"；此处使用普通 Go 参数/返回值，仅演示节点内调用。
// 若要跨节点调用，应改用 protobuf 请求和响应消息。
func (s *MyService) RpcSum(ctx context.Context, a, b int) (int, error) {
    return a + b, nil
}

// ApiGetUser 注册为 "ApiGetUser"；本例使用普通 Go 类型，仅适用于节点内调用。
// 限制来自本例的参数/返回类型，而非 Api 前缀；使用 protobuf 请求和响应也可跨节点调用。
// User 和 loadUser 为业务自行定义的类型与方法。
func (s *MyService) ApiGetUser(ctx context.Context, uid int64) (*User, error) {
    user := s.loadUser(uid)
    if user == nil {
        return nil, fmt.Errorf("user %d not found", uid)
    }
    return user, nil
}
```

**默认命名与签名规则**：
- `Rpc` / `RPC` 与 `Api` / `API` 都进入同一方法表；当前调用链不按这两类前缀限制本地或远程访问。`Rpc` 用于远程接口、`Api` 用于节点内接口是命名约定，不是强制隔离或授权边界。
- 前缀后的下一个字符须为 ASCII 大写字母，例如 `RpcSum`，而不是 `Rpcsum`。
- `RpcRo` / `RPCRo`、`ApiRo` / `APIRo`：对应的只读方法前缀，详见第 8 节。
- `context.Context` 可省略；如果声明，必须是接收者之后的第一个参数，由框架注入，不放入调用参数列表。
- 参数类型须为导出类型或内置类型；返回结构体应使用指针，不支持直接返回结构体值。可返回业务结果及 `error`。
- **类型限制取决于调用是否跨节点，而非方法前缀**：节点内不经过网络序列化，`Rpc` 和 `Api` 方法均可使用基本类型、自定义结构体指针、slice、map 等（仍须满足上述签名规则）。
- 当前跨节点消息封装使用 protobuf `Any`，非空请求和非空业务响应都必须实现 `proto.Message`，并且接收端须能解析对应消息类型；`nil` 请求/响应允许省略。普通 Go 结构体、`[]interface{}{1, 2}` 或 `int` 结果不能直接跨节点传输。即使方法名为 `Api...`，只要参数和响应满足这些要求且路由、授权允许，当前实现也可以从远端调用。

---

## 4. 调用其他 Service

以下示例中 `rpc` 来自 `github.com/njtc406/emberengine/v2/engine/pkg/core/rpc`，`dto` 来自 `github.com/njtc406/emberengine/v2/engine/pkg/dto`。

### 4.1 同步调用（Call）

**一次性调用（默认自动释放，推荐）**：

```go
func (s *MyService) callOther(ctx context.Context) {
    ctxTimeout, cancel := context.WithTimeout(ctx, time.Second)
    defer cancel()

    var result int
    err := s.Select(rpc.WithName("TargetService")).CallWithOpt(ctxTimeout,
        dto.WithMethod("RpcSum"),
        dto.WithIn([]interface{}{1, 2}),
        dto.WithOut(&result),
    )
    if err != nil {
        s.WithContext(ctxTimeout).Errorf("call failed: %v", err)
        return
    }
    s.WithContext(ctxTimeout).Infof("result: %d", result)
}
```

也可以使用简化接口：

```go
var result int
err := s.Select(rpc.WithName("TargetService")).Call(ctx, "RpcSum", []interface{}{1, 2}, &result)
if err != nil {
    s.WithContext(ctx).Errorf("call failed: %v", err)
}
```

**复用 Bus（显式使用不会自动回收，最后需手动释放）**：

```go
bus := s.Select(rpc.WithName("TargetService"))
defer bus.Release()

for _, pair := range [][2]int{{1, 2}, {3, 4}} {
    var result int
    if err := bus.CallWithOpt(ctx,
        dto.WithNotRecycle(), // 设置本次调用不释放bus
        dto.WithMethod("RpcSum"),
        dto.WithIn([]interface{}{pair[0], pair[1]}),
        dto.WithOut(&result),
    ); err != nil {
        s.WithContext(ctx).Errorf("call failed: %v", err)
        return
    }
}
```

> `Call`、`AsyncCall`、`Send` 以及默认的 `*WithOpt` 调用都会自动释放 Bus（包括调用返回错误时）。不要再配合 `defer bus.Release()`，也不能在调用后继续使用该 Bus。仅 `*WithOpt` 可通过**每次调用**传入 `dto.WithNotRecycle()` 来复用，最终由调用方释放；Bus 不支持并发使用。选择后未发起调用的 Bus 也需要手动释放。

### 4.2 异步调用（AsyncCall）

```go
_, err := s.Select(rpc.WithName("TargetService")).AsyncCallWithOpt(ctx,
    dto.WithMethod("RpcSum"),
    dto.WithIn([]interface{}{1, 2}),
    dto.WithCallbacks(func(ctx context.Context, data interface{}, err error, params ...interface{}) {
        if err != nil {
            s.WithContext(ctx).Errorf("async call failed: %v", err)
            return
        }
        s.WithContext(ctx).Infof("async result: %v", data)
    }),
)
if err != nil {
    s.WithContext(ctx).Errorf("submit async call failed: %v", err)
}
```

> 返回值为取消句柄和提交错误；成功提交后，业务结果或调用错误通过回调接收。Bus 在提交返回时即自动释放，不必等回调完成。回调投递到调用方 Service 的 Mailbox；其并发安全边界取决于 Mailbox 配置，见第 8 节。

### 4.3 单向发送（Send）

```go
if err := s.Select(rpc.WithName("TargetService")).SendWithOpt(ctx,
    dto.WithMethod("RpcNotify"),
    dto.WithIn([]interface{}{"hello"}),
); err != nil {
    s.WithContext(ctx).Errorf("send failed: %v", err)
}
```

> Send 不等待业务响应，但仍需检查投递错误。目标需自行实现 `RpcNotify` 方法。

### 4.4 路由选项

**什么是分区（Partition）？**

内部 Partition 统一为 string；所有公开分区参数（选择器、PID 构造、事件发布及对应接口）接受 any，经 `actor.PartitionString` 统一转换。非 nil 使用 `fmt.Sprint`（遵循 Stringer/Formatter）；`1`、`int64(1)` 和 `"1"` 表示同一分区，`"001"` 不同。nil/带类型 nil 转空字符串；`WithPartition(nil)` 清除分区过滤，空字符串则精确匹配空分区。旧 `SelectByServiceType` 的 nil/空字符串均不限制分区。配置与直接填充存储结构字段仍用 string。复杂值应保证格式化结果跨节点稳定，详见 Partition 设计。

分区是配置在 Service 实例上的逻辑分组编号，可用于区分游戏区服、业务分片等。例如，`Partition: "1"` 表示该实例属于分区 1，`Partition: "2"` 表示属于分区 2；分区编号的业务含义由应用定义。

它**不是节点 ID，也不是 Worker 编号**：一个节点可以部署多个分区的服务，同一分区的服务也可以分布在多个节点。分区用于选路筛选，不会自动完成用户分片、数据隔离或访问授权。

例如部署以下实例（表格表示部署关系，并非要求它们都在同一个节点）：

| 服务名称 | Partition | 业务含义 |
|----------|-----------|----------|
| `GameService` | `1` | 1 区的业务调用方 |
| `UserService` | `1` | 处理 用户1 |
| `UserService` | `2` | 处理 用户2 |

当 `GameService` 自身的分区为 `1` 时：

```go
// Select 会隐式追加调用方的分区：选择所有符合条件的 Partition=1 的 UserService。
// 本例只演示选路，因此立即释放；实际可以接 Call/Send 等自动释放的调用。
samePartitionBus := s.Select(rpc.WithName("UserService"))
samePartitionBus.Release()

// SelectByOpt 只传名称、不限定分区：分区 1 和分区 2 的 UserService 都会被选中。
allPartitionsBus := s.SelectByOpt(rpc.WithName("UserService"))
allPartitionsBus.Release()

// 显式跨分区：选择 Partition=2 的 UserService。
otherPartitionBus := s.SelectByOpt(rpc.WithName("UserService"), rpc.WithPartition(2))
otherPartitionBus.Release()
```

跨分区不等于跨节点：是否走网络取决于目标实例是否位于当前节点。同样，同分区调用也可能走网络；跨节点时仍须满足服务可见性、传输和 protobuf 数据类型要求。

这里须区分 `Select` 与 `SelectByOpt`：`Select` 强制使用调用方的分区，传入 `WithPartition` 也会被覆盖；`SelectByOpt` 不隐式追加分区，仅传名称时会选择所有分区中符合条件的同名实例，而非随机选一个。选择结果仍受服务可选状态、主从等条件约束，默认选择主实例。跨分区或显式指定分区应使用 `SelectByOpt`。以下片段仅展示选路，实际应接着调用，或在不使用时释放 Bus：

```go
// 当前分区，按名称选择
bus := s.Select(rpc.WithName("UserService"))
bus.Release()

// 按名称 + 显式分区
bus = s.SelectByOpt(rpc.WithName("UserService"), rpc.WithPartition(1))
bus.Release()

// 当前分区，按服务类型选择
bus = s.Select(rpc.WithType("game"))
bus.Release()

// 按分区和服务名称选择；精确实例路由使用 SelectByServiceUid 或 SelectByPid
bus = s.SelectByOpt(rpc.WithName("UserService"), rpc.WithPartition(1))
bus.Release()
```

已持有目标 PID 时可用 `SelectByPid`，按自定义条件选路可用 `SelectByRule`（例如匹配 `pid.GetNodeId()`）。当前没有 `rpc.WithNodeId` 选项。跨节点还要求目标服务配置 `Visibility: cluster` 并启用相应集群传输。

---

## 5. Module 子模块

Module 是 Service 的逻辑子单元，共享 Service 的 Mailbox：

```go
type CalcModule struct {
    core.Module
}

// Module 同样按 Api / Rpc 前缀自动注册。
func (m *CalcModule) RpcMultiply(ctx context.Context, a, b int) int {
    return a * b
}

// 在 Service 的 OnInit 中挂载。
func (s *MyService) OnInit() error {
    _, err := s.AddModule(&CalcModule{})
    return err
}
```

Module 与 Service 共用方法表，调用目标是所属 Service，方法名为 `RpcMultiply`，不需要 Module 名称前缀。应避免 Service 与各 Module 的方法重名。`AddModule` 返回模块 ID 和错误，必须检查错误。

---

## 6. 事件系统

### 6.1 订阅事件

```go
func (s *MyService) OnInit() error {
    reg := s.GetEventHandlerRegistry()
    
    // 注册事件处理器
    err := reg.RegisterEvent(
        def.EventType(1001),           // 事件类型
        "onUserLogin",                  // 处理器名称（需唯一）
        func(ctx context.Context, data any) error {
            // 处理事件
            s.WithContext(ctx).Infof("user logged in: %v", data)
            return nil
        },
    )
    if err != nil {
        return err
    }
    return nil
}
```

### 6.2 发布事件

`RegisterEvent` 注册的是**服务内本地事件**，由 Processor 同步触发，不会自动订阅集群事件：

```go
s.GetEventProcessor().Trigger(ctx, def.EventType(1001), data)
```

集群事件需按作用域分别使用 `RegisterGlobalEvent`、`RegisterServerEvent`、`RegisterSpecificEvent` 注册。回调中的 `data` 为发布的 protobuf 消息；发布参数必须实现 `proto.Message`。例如订阅并发布同分区事件（`data` 为业务定义的 protobuf 消息）：

```go
const userLoginEvent = def.EventType(1001)
if err := s.GetEventHandlerRegistry().RegisterServerEvent(userLoginEvent, "onServerUserLogin",
    func(ctx context.Context, data any) error {
        s.WithContext(ctx).Infof("user logged in: %v", data)
        return nil
    },
); err != nil {
    return err
}
if err := s.GetEventProcessor().PublishServer(ctx, userLoginEvent, data); err != nil {
    return err
}
```

其他作用域的发布接口（分别使用对应的注册接口订阅）：

```go
processor := s.GetEventProcessor()
if err := processor.PublishGlobal(ctx, eventType, data); err != nil {
    return err
}
if err := processor.PublishSpecific(ctx, eventType, targetServiceUid, data); err != nil {
    return err
}
```

`PublishServer` 的分区取自当前服务；`PublishSpecific` 的参数顺序是事件类型、目标 Service UID、数据。跨节点分发需要启用事件总线传输；未启用时只在本节点分发。本地 `Trigger` 在调用 goroutine 同步执行，不能据此假设会自动切换到 Mailbox。

### 6.3 内置系统事件

| 事件 | 说明 |
|------|------|
| `event.ServiceBecomeMaster` | 当前服务成为主节点 |
| `event.ServiceBecomeSlaver` | 当前服务成为从节点 |
| `event.ServiceLoseMaster` | 失去主节点身份 |

---

## 7. 定时器

```go
func (s *MyService) OnInit() error {
    // 延迟执行（一次性）。
    if _, err := s.AfterFunc(5*time.Second, "delayed-task",
        func(ctx context.Context, timer *timingwheel.Timer, args ...interface{}) error {
            s.WithContext(ctx).Infof("5 seconds elapsed")
            return nil
        },
    ); err != nil {
        return err
    }

    // 固定间隔重复执行，参数为 time.Duration。
    if _, err := s.TickerFunc(10*time.Second, "periodic-task",
        func(ctx context.Context, timer *timingwheel.Timer, args ...interface{}) error {
            s.WithContext(ctx).Infof("every 10 seconds")
            return nil
        },
    ); err != nil {
        return err
    }

    // Cron 调度，参数为字符串表达式，精度到秒。
    if _, err := s.CronFunc("@every 1m", "cron-task",
        func(ctx context.Context, timer *timingwheel.Timer, args ...interface{}) error {
            s.WithContext(ctx).Infof("cron task ran")
            return nil
        },
    ); err != nil {
        return err
    }
    return nil
}
```

`timingwheel` 来自 `github.com/njtc406/emberengine/v2/engine/pkg/utils/timingwheel`。三个接口均返回任务 ID 和错误；需要提前取消时保存 ID 并调用 `s.CancelTimer(taskID)`，Service 停止时会停止其调度器。

> 普通定时器回调通过 Service 的 Mailbox 执行，并发安全边界见第 8 节。`AfterAsyncFunc`、`TickerAsyncFunc`、`CronAsyncFunc` 则在独立 goroutine 中执行，访问业务状态需自行同步；不要长期持有回调传入的池化 Timer。

---

## 8. 读写分离模式

适用于读多写少的场景，允许标记为 ReadOnly 的 RPC 方法并发执行：

**配置**：

```yaml
Mailbox:
  EnableRWMode: true
  MaxConcurrentReads: 32
```

该 `Mailbox` 配置位于对应的 `ServiceConf.StartServices` 条目下。

**代码**：使用只读前缀，或实现 `inf.IReadOnlyDeclarer` 显式声明已注册的方法：

```go
func (s *MyService) RpcRoSum(ctx context.Context, a, b int) int {
    return a + b
}

// 另一种声明方式：保留普通方法名，并返回完整的注册名称。
func (s *MyService) ReadOnlyMethods() []string {
    return []string{"RpcSum"}
}
```

读 Job 使用共享读锁并发执行，写 Job 使用独占写锁；未标记为只读的 RPC 请求、RPC 回复回调、普通定时器和事件 Job 按写任务处理。只读标记是业务承诺，框架不会检查方法是否真的修改了状态。

**并发边界**：共享 Mailbox 不等于默认全局串行。未启用 RW 模式时，多 Worker 可并发执行不同 dispatch key 的任务；如需整个 Service 串行处理，应将 `Mailbox.SchedulePolicy.InitialWorkerNum` 设为 `1` 且关闭自动扩缩容。RW 模式的锁不覆盖自行启动的 goroutine、Async 定时器或外部直接调用；这些路径仍需业务同步。

不要在 Mailbox Handler 中同步调用本服务的 `Stop()`（会等待自身退出而死锁）；在 RW 写任务中同步等待另一个需要本服务 Mailbox 处理的调用，也可能形成等待环。优先使用异步调用拆开依赖。

---

## 9. 日志最佳实践

```go
// ✅ 推荐：带 context 的日志（自动携带 TraceID）
s.WithContext(ctx).Infof("user %d logged in", uid)
s.WithContext(ctx).Errorf("operation failed: %v", err)

// ❌ 避免：不带 context 的日志（丢失链路信息）
s.Infof("something happened")
```

日志级别：

| 级别 | 用途 |
|------|------|
| `Trace` | 极细粒度调试信息 |
| `Debug` | 开发调试信息 |
| `Info` | 正常业务流程记录 |
| `Warn` | 异常但可恢复的情况 |
| `Error` | 错误，需要关注 |
| `Fatal` | 致命错误，进程退出 |

---

## 10. 自定义业务配置

在节点加载配置之前注册业务配置描述和结构体工厂，而不是仅在 YAML 中声明 `ServicesConfMap`：

```go
import "github.com/njtc406/emberengine/v2/engine/pkg/config"

type MyServiceConf struct {
    MaxUsers int `mapstructure:"MaxUsers"`
}

func init() {
    config.RegisterServiceConf(&config.ServiceConfig{
        ServiceName: "MyService",
        ConfName:    "myservice",
        ConfType:    "yaml",
        CfgCreator: func() interface{} {
            return &MyServiceConf{}
        },
    })
}
```

对应的 `myservice.yaml` 内容：

```yaml
MaxUsers: 1000
```

本地文件从节点加载配置时传入的配置目录查找，当前实现未使用每项 `ServiceConfig.ConfPath` 选择目录。成功解析的对象保存在该节点 `*config.Config` 的 `ServiceConf.ServicesConfMap["MyService"].Cfg` 中，使用时应检查条目是否存在并做类型断言；不要把 `GetServiceConf` 当作已解析对象的获取接口，当前它返回的是工厂函数。

当前业务配置加载流程没有接入文件监听或调用 `OnChangeFun`，**不支持自动热更新**。配置文件读取失败会记录警告并跳过，业务应在启动时校验必需配置。

---

## 附录：常见模式

### 资源释放

```go
// 一次性 Bus：Call/AsyncCall/Send 默认自动释放，不再手动 Release。
var result int
err := s.Select(rpc.WithName("Target")).Call(ctx, "RpcSum", []interface{}{1, 2}, &result)
if err != nil {
    s.WithContext(ctx).Errorf("call failed: %v", err)
}

// 仅选路而未调用时，仍需释放；显式 WithNotRecycle 的复用见第 4.1 节。
unusedBus := s.Select(rpc.WithName("Target"))
unusedBus.Release()

// 从有效的父 Context 派生超时，并释放相关资源。
ctxTimeout, cancel := context.WithTimeout(ctx, time.Second)
defer cancel()
_ = ctxTimeout // 实际调用中使用 ctxTimeout
```

### 错误处理

```go
// 返回带上下文的 wrapped error
if err != nil {
    return fmt.Errorf("MyService.loadUser: %w", err)
}

// 禁止忽略错误
result, _ := doSomething()  // ❌
```

### 接口断言

```go
// 确保编译期验证接口实现
var _ inf.IService = (*MyService)(nil)
```
