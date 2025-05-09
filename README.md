# EmberEngine 🔥

---

## 简介
EmberEngine 是一个基于 Actor 模型和 RPC 通信的分布式框架，旨在提供高性能、低延迟的分布式通信解决方案。
- **逻辑层次**：整个框架分为三个层级，分别为：
1. 节点(node): 是一个进程，包含可以运行多个服务
2. 服务(service): 是一个 Actor，包含多个模块，是运行的基础单位。
3. 模块(module): 最小逻辑单元，实现具体业务，service本身也是一个module, 多个module可以建立树形结构。
---

## 特性
- **Actor 模型**：每个 Actor 独立运行，保证线程安全，减少并发问题。
- **RPC 通信**：通过 MessageBus 进行消息发送，保持各部分独立。
- **灵活的路由机制**：业务层通过 `selector` 手动选择路由，再调用 `call` 或 `send` 完成通信。
- **模块化设计**：各组件职责单一，方便维护和扩展。
- **高效的队列模型**：MPSC 用于任务分发和消息邮箱，提升消息传输效率。
- **多线程支持**：支持多线程运行，并且可以设置自动扩容策略，主要用于无状态服务，比如战斗服。
- **自动扩容策略**：支持自动扩容，以应对负载变化。
- **主从服务架构**：支持主从服务架构，保证单点服务的高可用(数据一致性暂时未做)。
- **集群事件**：支持集群事件，保证集群之间的消息传递。
- **时间轮**：每个节点都有使用时间轮调度，以减少资源浪费。
---

## 架构概览
EmberEngine 的主要部分：
- **MessageBus**：只负责消息发送，不处理业务逻辑。
- **RpcMonitor**：监控 RPC 调用和超时，确保 Actor 的单线程安全。
- **Selector & Sender**：通过 `selector` 获取 `sender`，实现灵活的路由和通信。
- **IRpcSenderHandler**：支持多个 `sender` 共享同一连接，减少重复连接、提高资源利用率。

---

## 使用示例
- **详细使用示例请直接参考[https://github.com/njtc406/emberengine/tree/main/] example中的示例文件**
---

## 快速开始
1. 克隆项目：
    ```sh
    git clone https://github.com/njtc406/emberengine.git
    cd emberengine
    ```

2. 安装依赖：
    ```sh
    go mod tidy
    ```

3. 运行示例：
    ```sh
    go run main.go
    ```

---

## TODO & 未来计划
- 完善文档和示例，方便开发者上手。

---

## 贡献
欢迎提出 Issue 和 PR 一起完善 EmberEngine。  
如果有问题或建议，欢迎讨论交流。

感谢 https://github.com/duanhf2012/origin 大佬提供的开源项目。本框架参考了其设计理念,做了一些适合自己项目的优化。

感谢 https://github.com/RussellLuo/timingwheel 多层时间轮。

---

## License
[Apache2.0 License](LICENSE)
