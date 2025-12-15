# EmberEngine 🔥
>
> 以 Actor + RPC 为内核的分布式服务框架/容器
> 
> *只考虑自己的叫想法，能平衡各方的才叫设计。*

---

## ✨ 简介

**Ember** 是一个以服务 **容器框架** ,为所有服务提供统一的运行环境。

它提供了一套统一的运行模型（**Node → Service → Module**），
可以在同一套框架下承载多种类型的 long-running 服务：

- 游戏服务器（逻辑服、房间服、网关服等）；
- 实时服务网格、内部 RPC 服务；
- HTTP / Web 服务（通过 `sysModule/httpmodule` 嵌入 gin 等框架）；

可以将任意类型的服务（如游戏逻辑服、房间服、网关服等）都运行在 Ember 框架下，统一集群下所有服务的调用方式

目标是为游戏服务器、实时服务以及可伸缩微服务提供一种 **高并发、安全、易维护、易扩展** 的统一运行时。

核心特点：

- **服务容器化运行时**：统一的 Node → Service → Module 架构，node只是service容器,service可以运行在任意node上,module是业务承载层,service的功能完全由module构成
- **服务解耦**：每个服务都是独立的 Actor，通过消息队列通信，实现高度解耦和可伸缩性
- **服务可扩展**：通过 Module 机制，可动态添加或替换服务的功能模块，无需重启服务；
- **同时支持actor模式和并发模式**：对于需要严格顺序执行的业务逻辑, 可以选择actor模式; 对于高并发场景, 可以选择并发模式
- **自动扩缩容**：可配置策略，动态调整资源；
- **灵活路由机制**：实现了路由选择器, 可以根据不同的路由策略, 选择不同的服务实例进行调用
- **内置时间轮**：支持低成本高性能定时器；
- **集群事件**： 支持集群事件（全局事件/server事件/特定事件）, 合理使用事件可以实现服务之间的解耦和通信

---

## 🧠 架构概览

### 核心概念

- **Node**  
  服务容器，对应一个进程，可以承载任意数量的服务

- **Service**  
  服务，提供业务功能，由任意个module组成，每个service有自己的mailbox, 用于接收和处理消息

- **Module**  
  业务模块，实现具体的业务功能，挂载到service中使用
  Service本身也是一个Module

整体结构示意：

```text
      +-----------------------------+
      |           Node              |
      |  (一个进程, 包含多个服务)     |
      +-----------------------------+
              |        |       |
    +---------+        |       +-----------+
    |                  |                   |
    v                  v                   v
+---------------+  +---------------+   +---------------+
|   Service A   |  |   Service B   |   |   Service C   |
|   (Actor/...) |  |   (HTTP/...)  |   |   (自定义...) |
+-------+-------+  +-------+-------+   +-------+-------+
        |                  |                   |
   +----+----+        +----+----+         +----+----+
   | Module  |        | Module  |         | Module  |
   | (e.g.   |        | (e.g.   |         |  ... )  |
   | http)   |        | ws)     |         |         |
   +---------+        +---------+         +---------+

* 每个 Service 拥有自己的mailbox；
```

---

## 📦 系统介绍
### **Mailbox**  
- 服务的消息队列, 用于接收和处理消息
- 目前有两种工作模式：
  - Actor 模式：每个 Service 都是一个 Actor，消息按顺序处理；
  - 并发模式：Service 内部可以开启多个 goroutine 并发处理消息；
- 支持优先级：可以为每个消息设置优先级，高优先级消息会优先处理；
  - duel: 双队列模式,只有低级和高级两种队列，高优先级消息会优先处理
  - priority： 多优先级模式，支持 sys/urgent/high/normal/low/batch 六级优先级
- mailbox挂起后，将只会处理urgent级别以上的调用和服务自身的回调消息，方便控制运行时状态（如暂停/恢复/重启等）

### **Event**  
- 事件系统, 用于服务之间的解耦和通信

### **Cluster**  
- 集群，将服务组成集群，扩展服务的功能

### **RPC**  
- 远程调用系统, 用于服务之间的通信
- 支持同步/异步/广播调用
- 支持service和module级别的函数调用
  - 服务初始化时, 会自动注册service下所有以API/Api/RPC/Rpc开头的方法
  - 如果服务包含RPC/Rpc开头的方法，则会自动将服务注册到集群， 其他服务可以通过服务发现调用该服务的方法
  - 否则，只能被同node上的其他服务调用，不能跨node调用

### **Timer**  
- 定时器系统, 用于服务之间的定时任务

### **Log**  
- 日志系统, 支持服务级独立日志记录

## 🚀 快速开始

### 1. 克隆项目
```bash
git clone https://github.com/njtc406/emberengine.git
cd emberengine
````

### 2. 安装依赖

```bash
go mod download
```

### 3. 运行示例

```bash
cd example/node1
go run main.go
```

你还可以启动多个 Slave 节点，实现主从架构验证：

```bash
cd example/node_slave
go run main.go
```

---

## 🔁 消息示例：Service 之间通信

```go

// 所有的调用都支持优先级 
ctx := xcontext.New(nil)
ctx.SetHeader(def.DefaultPriorityKey, def.PrioritySysStr) // 系统级（在执行完当前任务后，优先执行系统级任务）
ctx.SetHeader(def.DefaultPriorityKey, def.PriorityUserStr) // 用户级
// Select 选择目标服务(结果可以是一个或多个)

// 同步调用远程服务方法（带返回值）
err := serviceInstance.Select(rpc.WithServiceName(ServiceName2)).Call(ctxWithTimeout, "APITest2", nil, nil)

// 异步调用远程服务方法
err := serviceInstance.Select(rpc.WithServiceName(ServiceName2)).AsyncCall(ctx, "RPCSum", &msg.Msg_Test_Req{A: 1, B: 2}, &dto.AsyncCallParams{
    Params: []interface{}{1, 2, "a"},
   }, func(data interface{}, err error, params ...interface{}) {
         if err != nil {
			 serviceInstance.GetLogger().Errorf("AsyncCall Service3.RPCSum response failed, err:%v", err)
			 return
         }

         serviceInstance.GetLogger().Debugf("AsyncCall Service3.RPCSum params:%+v", params)
         
         resp := data.(*msg.Msg_Test_Resp)
		 serviceInstance.GetLogger().Debugf("AsyncCall Service3.RPCSum out:%d", resp.Ret)
   })

// 或发送消息（不等待响应）
err := serviceInstance.Select(rpc.WithServiceName(ServiceName2)).Send(ctx, "APITest2", nil)
```
---

## ❓ FAQ

* **Q:** Actor 是否是单线程？

  * **A:** 如果没有开启并发模式,那么service是严格单线程执行,如果开启了并发模式,那么service是多线程执行

* **Q:** 支持热更新吗？

  * **A:** 暂不支持二进制热更，但可通过主从切换、重载 service 达到平滑迁移。

* **Q:** 如何实现消息的顺序性？

  * **A:** 对于同一 dispatchKey（如 UID），通过 `%` 哈希绑定固定 Worker，可以保证有序(在未开启自动扩缩容的情况下)。

---

## 🤝 贡献指南

欢迎 Issue、PR 与讨论，推荐从以下方向入手：

* 补充模块文档
* 优化 selector 路由策略
* 实现更多调度策略插件
* 性能基准测试压测方案

---

## 📚 参考与致谢

* [origin](https://github.com/duanhf2012/origin)：参考了大佬的部分设计思路
* [timingwheel](https://github.com/RussellLuo/timingwheel)：多层时间轮

---

## License
[Apache2.0 License](LICENSE)
