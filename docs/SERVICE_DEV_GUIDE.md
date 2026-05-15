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

```
Init → OnInit → Start → OnStart → OnStarted → [运行中] → Stop → OnRelease
```

| 阶段 | 回调方法 | 用途 |
|------|----------|------|
| 初始化 | `OnInit()` | 注册 RPC Handler、定时器、Module、事件订阅 |
| 启动前 | `OnStart()` | 启动前的异步准备工作 |
| 启动后 | `OnStarted()` | 完全启动后的回调（所有依赖就绪） |
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
    inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
    "github.com/njtc406/emberengine/engine/pkg/services"
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
      Partition: 1
```

---

## 3. RPC Handler

以 `API` 开头的公开方法会自动注册为 RPC Handler：

```go
// APISum 自动注册为 RPC 方法 "APISum"
// 第一个参数必须是 context.Context
// 返回值：最后一个可以是 error
func (s *MyService) APISum(ctx context.Context, a, b int) (int, error) {
    return a + b, nil
}

// APIGetUser 支持复杂类型
func (s *MyService) APIGetUser(ctx context.Context, uid int64) (*User, error) {
    user := s.loadUser(uid)
    if user == nil {
        return nil, fmt.Errorf("user %d not found", uid)
    }
    return user, nil
}
```

**命名规则**：
- 方法名必须以 `API` 开头
- 第一个参数必须是 `context.Context`
- 参数和返回值支持基本类型、结构体指针、slice、map

---

## 4. 调用其他 Service

### 4.1 同步调用（Call）

```go
func (s *MyService) callOther(ctx context.Context) {
    ctxTimeout, cancel := xcontext.NewWithTimeout(nil, time.Second)
    defer cancel()

    // 选择目标服务
    bus := s.Select(rpc.WithName("TargetService"), rpc.WithPartition(1))
    defer bus.Release()

    // 同步调用
    var result int
    err := bus.CallWithOpt(ctxTimeout,
        dto.WithMethod("APISum"),
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

### 4.2 异步调用（AsyncCall）

```go
bus := s.Select(rpc.WithName("TargetService"), rpc.WithPartition(1))
defer bus.Release()

bus.AsyncCallWithOpt(ctx,
    dto.WithMethod("APISum"),
    dto.WithIn([]interface{}{1, 2}),
    dto.WithCallbacks(func(ctx context.Context, data interface{}, err error, params ...interface{}) {
        if err != nil {
            s.WithContext(ctx).Errorf("async call failed: %v", err)
            return
        }
        s.WithContext(ctx).Infof("async result: %v", data)
    }),
)
```

> **注意**：回调在调用方 Service 的 Mailbox 中执行，线程安全。

### 4.3 单向发送（Send）

```go
bus := s.Select(rpc.WithName("TargetService"), rpc.WithPartition(1))
defer bus.Release()

bus.SendWithOpt(ctx,
    dto.WithMethod("APINotify"),
    dto.WithIn([]interface{}{"hello"}),
)
```

> Send 不等待响应，适用于不关心结果的通知场景。

### 4.4 路由选项

```go
// 按名称 + 分区
s.Select(rpc.WithName("UserService"), rpc.WithPartition(1))

// 按服务类型
s.Select(rpc.WithType("game"))

// 指定节点
s.Select(rpc.WithName("UserService"), rpc.WithNodeId("node2"))
```

---

## 5. Module 子模块

Module 是 Service 的逻辑子单元，共享 Service 的 Mailbox：

```go
type CalcModule struct {
    core.Module
}

// Module 的 API 方法同样自动注册
func (m *CalcModule) APIMultiply(ctx context.Context, a, b int) int {
    return a * b
}

// 在 Service 的 OnInit 中挂载
func (s *MyService) OnInit() error {
    s.AddModule(&CalcModule{})
    return nil
}
```

Module 的 RPC 方法可以直接通过 Service 名称调用，框架自动路由到对应 Module。

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

```go
// 全局事件（所有订阅者收到）
s.GetEventBus().PublishGlobal(ctx, eventType, data)

// 服务级事件（同分区的服务收到）
s.GetEventBus().PublishServer(ctx, eventType, data)

// 指定目标事件
s.GetEventBus().PublishSpecific(ctx, targetServiceUid, eventType, data)
```

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
    // 延迟执行（一次性）
    _, _ = s.AfterFunc(5*time.Second, "delayed-task",
        func(ctx context.Context, timer *timingwheel.Timer, args ...interface{}) error {
            s.WithContext(ctx).Infof("5 seconds elapsed")
            return nil
        },
    )

    // 周期执行
    _, _ = s.CronFunc(10*time.Second, "periodic-task",
        func(ctx context.Context, timer *timingwheel.Timer, args ...interface{}) error {
            s.WithContext(ctx).Infof("every 10 seconds")
            return nil
        },
    )

    return nil
}
```

> 定时器回调在 Service 的 Mailbox 中执行，线程安全。

---

## 8. 读写分离模式

适用于读多写少的场景，允许标记为 ReadOnly 的 RPC 方法并发执行：

**配置**：

```yaml
Mailbox:
  EnableRWMode: true
  MaxConcurrentReads: 32
```

**代码**：在 RPC Handler 上标记 ReadOnly（通过方法命名或注册时标记）。

读操作使用 `RLock` 并发执行，写操作使用 `WLock` 独占执行，保证数据一致性。

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

Service 支持加载独立的业务配置文件：

```go
// 在 ServiceConf 中配置
ServicesConfMap:
  MyService:
    ConfName: myservice
    ConfPath: ./configs
    ConfType: yaml
```

通过 `ServiceConfig.CfgCreator` 注册自定义配置结构体，框架自动加载和热更新。

---

## 附录：常见模式

### 资源释放

```go
// Bus 使用后必须释放
bus := s.Select(rpc.WithName("Target"))
defer bus.Release()

// Context 使用后取消
ctx, cancel := xcontext.NewWithTimeout(nil, time.Second)
defer cancel()
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
