# 特定服务事件使用指南

## 概述

特定服务事件(Specific Service Event)允许服务订阅指定服务的事件,实现精准的事件路由。这种机制特别适用于以下场景:

- **服务间精准通知**: 当需要向特定服务发送事件通知时
- **一对多订阅**: 多个服务可以订阅同一个服务的事件
- **解耦服务通信**: 通过事件机制替代直接RPC调用

## 关于 event 对象的共享（重要）

- 为了降低引用计数带来的误用风险，事件系统在“多订阅投递/广播”场景下默认不再共享同一个 `*event.Event` 实例。
- 每个订阅者都会收到一个独立的 `*event.Event`（浅拷贝），避免在多个 service 并发处理同一对象导致的数据竞争。
- 业务代码一般不需要手动调用 `IncRef()`；事件在 mailbox 内部会在处理结束后自动 `Release()`。

## 事件类型对比

| 事件类型 | 范围 | 订阅者 | 使用场景 |
|---------|------|--------|---------|
| **全局事件** | 所有节点 | 所有订阅该事件类型的服务 | 系统广播、配置更新 |
| **分区事件** | 同一Partition的服务 | 同一Partition下订阅该事件的服务 | 分区内部通信 |
| **特定服务事件** | 指定服务 | 订阅了该服务事件的所有服务 | 精准服务间通信 |

## 核心API

### 1. 订阅特定服务事件

```go
// SubscribeSpecific 订阅指定服务的事件
// eventType: 事件类型
// serviceUid: 目标服务的唯一ID(要订阅哪个服务的事件)
// svc: 订阅者服务
func (eb *Bus) SubscribeSpecific(eventType int32, serviceUid string, svc inf.IListener)

// 使用示例
eventBus := event.GetEventBus()
targetServiceUid := "game-service-001"
eventBus.SubscribeSpecific(1001, targetServiceUid, myService)
```

### 2. 发布特定服务事件

```go
// PublishSpecific 发布指定服务的事件
// 只有订阅了该服务事件的服务会收到
func (eb *Bus) PublishSpecific(ctx context.Context, eventType int32, serviceUid string, data proto.Message) error

// PublishSpecificLocal 发布本地特定服务事件(不通过NATS)
func (eb *Bus) PublishSpecificLocal(ctx context.Context, eventType int32, serviceUid string, data proto.Message) error

// 使用示例
ctx := context.Background()
myServiceUid := myService.GetPid().GetServiceUid()
eventData := &MyEventData{...}
eventBus.PublishSpecific(ctx, 1001, myServiceUid, eventData)
```

### 3. 取消订阅

```go
// UnSubscribeSpecific 取消订阅指定服务的事件
func (eb *Bus) UnSubscribeSpecific(eventType int32, serviceUid string, svc inf.IListener)

// 使用示例
eventBus.UnSubscribeSpecific(1001, targetServiceUid, myService)
```

## 使用Processor的便捷方法

在Service中,可以通过EventProcessor提供的便捷方法来订阅和发布事件:

```go
// 订阅特定服务事件
func (p *Processor) RegSpecificEventReceiverFunc(
    eventType int32, 
    serviceUid string, 
    receiver inf.IEventHandler, 
    callback inf.EventCallBack
)

// 取消订阅
func (p *Processor) UnRegSpecificEventReceiverFun(
    eventType int32, 
    serviceUid string, 
    receiver inf.IEventHandler
)

// 发布特定服务事件
func (p *Processor) PublishSpecific(
    ctx context.Context, 
    eventType int32, 
    serviceUid string, 
    data proto.Message
) error
```

## 完整使用示例

### 场景: 游戏服务订阅玩家服务的事件

```go
package example

import (
    "context"
    "github.com/njtc406/emberengine/engine/pkg/core"
    "github.com/njtc406/emberengine/engine/pkg/event"
    inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

const (
    // 定义事件类型
    EventPlayerLogin  int32 = 1001
    EventPlayerLogout int32 = 1002
)

// PlayerService 玩家服务
type PlayerService struct {
    core.Service
}

func (s *PlayerService) OnInit() error {
    // 玩家服务初始化
    return nil
}

// OnPlayerLogin 玩家登录时发布事件
func (s *PlayerService) OnPlayerLogin(playerId string) error {
    ctx := context.Background()
    
    // 发布玩家登录事件,所有订阅了该玩家服务事件的服务都会收到
    eventData := &PlayerLoginEvent{
        PlayerId: playerId,
        LoginTime: time.Now().Unix(),
    }
    
    myServiceUid := s.GetPid().GetServiceUid()
    return s.GetEventProcessor().PublishSpecific(ctx, EventPlayerLogin, myServiceUid, eventData)
}

// GameService 游戏服务
type GameService struct {
    core.Service
    playerServiceUid string
    eventHandler     *event.Handler
}

func (s *GameService) OnInit() error {
    // 获取玩家服务的UID(可以通过服务发现获取)
    s.playerServiceUid = "player-service-001"
    
    // 创建事件处理器
    s.eventHandler = event.NewHandler()
    s.eventHandler.Init(s.GetEventProcessor())
    
    // 订阅玩家服务的登录事件
    s.GetEventProcessor().RegSpecificEventReceiverFunc(
        EventPlayerLogin,
        s.playerServiceUid,
        s.eventHandler,
        s.onPlayerLogin,
    )
    
    // 订阅玩家服务的登出事件
    s.GetEventProcessor().RegSpecificEventReceiverFunc(
        EventPlayerLogout,
        s.playerServiceUid,
        s.eventHandler,
        s.onPlayerLogout,
    )
    
    return nil
}

func (s *GameService) OnRelease() {
    // 取消订阅
    s.GetEventProcessor().UnRegSpecificEventReceiverFun(
        EventPlayerLogin,
        s.playerServiceUid,
        s.eventHandler,
    )
    
    s.GetEventProcessor().UnRegSpecificEventReceiverFun(
        EventPlayerLogout,
        s.playerServiceUid,
        s.eventHandler,
    )
    
    s.eventHandler.Destroy()
}

// 处理玩家登录事件
func (s *GameService) onPlayerLogin(e inf.IEvent) {
    // 获取事件数据
    ev := e.(*event.Event)
    actorEvent := ev.Data.(*actor.Event)
    
    // 解析事件数据
    var loginEvent PlayerLoginEvent
    if err := proto.Unmarshal(actorEvent.Data.RawData, &loginEvent); err != nil {
        s.Error("unmarshal player login event error:", err)
        return
    }
    
    s.Info("player login:", loginEvent.PlayerId, "at", loginEvent.LoginTime)
    // 处理玩家登录逻辑...
}

// 处理玩家登出事件
func (s *GameService) onPlayerLogout(e inf.IEvent) {
    // 类似处理...
}
```

## 最佳实践

### 1. 事件类型定义

建议将事件类型定义在统一的地方:

```go
// events/types.go
package events

const (
    // 玩家相关事件 1000-1999
    EventPlayerLogin  int32 = 1001
    EventPlayerLogout int32 = 1002
    
    // 房间相关事件 2000-2999
    EventRoomCreated  int32 = 2001
    EventRoomDestroy  int32 = 2002
)
```

### 2. 服务UID管理

通过服务发现获取目标服务的UID:

```go
// 从集群中获取玩家服务
endpoints := cluster.GetCluster().GetEndpoints()
playerServices := endpoints.GetServicesByName("PlayerService")
if len(playerServices) > 0 {
    playerServiceUid = playerServices[0].GetServiceUid()
}
```

### 3. 事件数据结构

使用protobuf定义事件数据:

```protobuf
// events.proto
syntax = "proto3";

message PlayerLoginEvent {
    string player_id = 1;
    int64 login_time = 2;
    string ip = 3;
}

message PlayerLogoutEvent {
    string player_id = 1;
    int64 logout_time = 2;
    int32 reason = 3;
}
```

### 4. 资源清理

服务关闭时务必取消订阅:

```go
func (s *MyService) OnRelease() {
    s.GetEventProcessor().UnRegSpecificEventReceiverFun(
        eventType,
        targetServiceUid,
        s.eventHandler,
    )
    s.eventHandler.Destroy()
}
```

## 注意事项

1. **ServiceUid唯一性**: 确保ServiceUid在集群中是唯一的
2. **事件处理异步**: 事件处理是异步的,不要依赖同步返回结果
3. **错误处理**: 在事件处理函数中做好错误处理和日志记录
4. **性能考虑**: 避免在事件处理函数中执行耗时操作
5. **NATS支持**: 当启用NATS时,事件可以跨节点传播;否则仅在本地生效

## 配置示例

```go
// 初始化EventBus时配置特定事件前缀
eventBusConf := &config.EventBusConf{
    NatsConf: &config.NatsConf{
        EndPoints: []string{"nats://127.0.0.1:4222"},
    },
    GlobalPrefix:   "global.%d",
    ServerPrefix:   "server.%d.%d",
    SpecificPrefix: "specific.%d.%s",  // 特定服务事件前缀: specific.{eventType}.{serviceUid}
    ShardCount:     16,
}

eventBus := event.GetEventBus()
eventBus.Init(eventBusConf)
```

## 架构优势

1. **解耦性**: 服务间通过事件通信,降低耦合度
2. **扩展性**: 新增订阅者无需修改发布者代码
3. **灵活性**: 可以动态订阅和取消订阅
4. **可靠性**: 支持跨节点的事件传播(通过NATS)
5. **性能**: 使用分段锁提升并发性能

## 调试技巧

1. 启用日志查看事件流转:
```go
log.SysLogger.Debug("subscribe specific event, eventType:", eventType, "serviceUid:", serviceUid)
```

2. 查看订阅信息:
```go
// 在EventBus中添加调试方法
func (eb *Bus) GetSpecificSubscribers(eventType int32, serviceUid string) []string {
    // 返回订阅者列表
}
```

3. 使用测试代码验证:
```bash
go test -v -run TestSpecificEvent
```
