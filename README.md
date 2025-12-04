# EmberEngine 🔥
> 以 Actor + RPC 为内核的分布式服务框架/容器

> *只考虑自己的叫想法，能平衡各方的才叫设计。*

---

## ✨ 简介

**Ember** 是一个以 **Actor 模型 + RPC 通信** 为核心能力的分布式 **服务运行时 / 服务容器**。

它提供了一套统一的运行模型（**Node → Service → Module **），
可以在同一套框架下承载多种类型的 long-running 服务：

- 游戏服务器（逻辑服、房间服、网关服等）；
- 实时服务网格、内部 RPC 服务；
- HTTP / Web 服务（通过 `sysModule/httpmodule` 嵌入 gin 等框架）；
- 以及未来通过 Module / Component 扩展出来的任意自定义服务。

目标是为游戏服务器、实时服务以及可伸缩微服务提供一种 **高并发、安全、易维护、易扩展** 的统一运行时。

核心特点：

- **服务容器化运行时**：统一的 Node → Service → Module 架构；
- **高并发 / Actor 设计**：并行事件驱动 + 安全队列；
- **自动扩缩容**：可配置策略，动态调整资源；
- **灵活路由机制**：通过选择器自由选择调用服务,支持 UID、Pid、ServiceType 等多维选择；
- **内置时间轮**：支持低成本高性能定时器；
- **可配置执行模型**：每个 Service 可通过配置选择“严格单线程（接近 Actor 风格）”或“多 Worker 并发”执行模型，以在顺序性与吞吐量之间灵活权衡；
- **集群事件**： 支持集群事件, 按需订阅；
- **主从模式**： 服务支持主从模式, 主节点空缺时从节点自动切换为主节点，防止单点故障。

---

## 🧠 架构概览

### 核心概念

- **Node**  
  一个运行时节点，通常对应一个进程，负责承载多个 Service / Module，并与集群中的其它节点协作。

- **Service**  
  进程内的业务服务单元，例如：玩家服务、房间服务、匹配服务、HTTP 服务等。每个 Service 拥有自己的消息队列和生命周期管理。

- **Module**  
  Service 内部或 Node 级别的功能模块/子系统，是构成service整体功能的基本单位。
  每个 Module 都有自己的生命周期管理，且可以挂载到不同的 Service 中。
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

* 每个 Service 拥有自己的消息队列；
* Module 作为 Service / Node 内的功能模块, 也可以组织为树状结构；
* Node 级别负责生命周期管理、集群注册与扩缩容。
```

---

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

## 🧩 核心模块介绍

| 模块名          | 说明                          |
| ------------ |-----------------------------|
| `actor`      | Actor 基础结构，实现消息投递、生命周期管理    |
| `messagebus` | 负责消息传输与调度，支持本地直调与 RPC       |
| `selector`   | 多维度选择器，支持 UID、Pid、Type 路由方式 |
| `repository` | 本地缓存服务注册表，支持查询与动态变更         |
| `discovery`  | etcd 实现服务注册与发现，支持事件监听与主从选举  |
| `event`      | 支持集群事件广播（全局/命名空间：serverId）  |
| `scheduler`  | 动态扩缩容策略器，支持嵌套策略组合           |

---

## ❓ FAQ

* **Q:** Actor 是否是单线程？

  * **A:** 如果没有开启并发模式,那么service是严格单线程执行,如果开启了并发模式,那么service是多线程执行

* **Q:** 支持热更新吗？

  * **A:** 暂不支持二进制热更，但可通过主从切换、重载 Worker 达到平滑迁移。

* **Q:** 如何实现消息的顺序性？

  * **A:** 对于同一 UID，通过 `%` 哈希绑定固定 Worker，可以保证有序。

---

## 🛠 TODO
* 将rpc拆分为独立项目,方便其他项目使用
* 移动internal中的代码到pkg目录下
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
