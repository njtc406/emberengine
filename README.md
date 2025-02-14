# 分布式服务框架

这是一个基于 Actor 模型的分布式服务框架，服务之间通过消息进行数据传递。

框架特点：
- 服务默认单线程运行，支持多线程扩展。
- 结构灵活、模块化，便于开发和维护。

## 框架结构

本框架由以下三部分组成：

1. **Node（节点）**  
   每个节点对应一个独立进程，可以运行多个服务。

2. **Service（服务）**  
   每个服务是一个 Actor，以协程形式运行，支持多线程执行。服务由若干模块组成。

3. **Module（模块）**  
   模块是最小的业务单元，依附于服务运行，负责实现具体功能。

---

### 目录结构
````
1. 整个项目结构
emberengine/
├── engine/              # 核心框架库
├── example/             # 使用示例，展示如何基于 engine 构建 service
├── template/            # 配置或其他模板，供开发者参考
├── go.mod
├── go.sum
├── LICENSE
└── README.md


2. engine结构
engine/
├── pkg/                         # 对外公开的 API 和工具库
│   ├── actor/                   # Actor 模型（proto 及实现）
│   ├── cluster/                 # 分布式/集群支持（包括 discovery、endpoints、repository 等）
│   ├── config/                  # 框架配置管理
│   ├── core/                    # 核心基础设施（模块、service、mailbox、node、rpc）
│   ├── def/                     # 常量、配置定义及辅助方法
│   ├── dto/                     # 数据传输对象
│   ├── event/                   # 事件系统（事件、事件类型、处理器等）
│   ├── interfaces/              # 接口定义（IBus、IDataDef、IDiscovery、…）
│   ├── plugins/                 # 插件机制
│   ├── profiler/                # 性能分析器
│   ├── services/                # 内置服务（守护进程、服务管理）
│   ├── sysModule/               # 系统模块（httpmodule、mysqlmodule、redismodule、wsmodule）
│   ├── sysMsg/                  # 系统消息
│   ├── sysService/              # 系统服务（dbservice、pprofservice）
│   └── utils/                   # 通用工具库
│         ├── network/           # 网络通信库（底层连接与协议处理）
│         ├── asynclib/          # 异步调用库
│         ├── bytespool/         # 字节池
│         ├── concurrent/        # 并发工具（调度、工作器等）
│         ├── errorlib/          # 错误处理工具
│         ├── httplib/           # HTTP 请求工具
│         ├── log/               # 日志系统
│         ├── maps/              # Map 工具
│         ├── mpmc/              # 多生产者多消费者队列
│         ├── mpsc/              # 多生产者单消费者队列
│         ├── pid/               # 进程/唯一标识管理
│         ├── pool/              # 对象/内存池
│         ├── queue/             # 队列实现（同步/异步队列）
│         ├── ring/              # 环形队列
│         ├── serializer/        # 序列化工具（JSON、Proto 等）
│         ├── timelib/           # 时间工具库
│         ├── timer/             # 定时器
│         ├── timingwheel/       # Timing Wheel 实现
│         │   └── delayqueue/    # 延时队列
│         ├── validate/          # 数据校验工具
│         └── version/           # 版本管理
├── internal/                    # 框架内部实现，不对外公开
│   ├── message/                 # 消息总线实现（内部消息调度）
│   └──  monitor/                 # 监控实现（状态监测、定时任务等）
├── config/                      # 非代码配置文件（示例或默认配置）
├── docs/                        # 设计文档、架构图、接口说明等
├── example/                     # 使用示例（展示如何基于 engine 构建服务）
└── template/                    # 配置及其他模板（快速入门指南）


````

## 集群功能

1. **集群管理**  
   每个节点内置集群模块，用于发现和管理注册到集群中的服务。

2. **服务注册与发现**
    - 所有实现 `RPC` 开头接口的服务，都会自动注册到集群中。
    - 服务注册与发现通过 `etcd` 实现（还未做成接口,后续再说）。

3. **远程调用**
    - 基于 `rpcx` 实现跨节点远程调用。

### 版本说明:
目前为第一版,后续慢慢补充
````
已经完成的内容:
1. 节点、服务、模块封装
2. 集群管理
3. 配置管理
4. 定时器(新增了时间轮支持)
5. 日志(支持自定义日志时间粒度和自动切分粒度的配置,过期自动删除日志的配置)
6. pprof
````

### 使用说明
````
详细使用,请查看example文件夹的使用示例
````
---

### 可使用的环境变量
````
TODO
````
---

### 后续规划:
````
1. 集群认证机制
2. 服务健康监控
3. 服务的限流策略
4. 更加模块化的设计,将大部分的底层内容做成插件,可以自由扩展和配置使用
5. 增加服务热更新的功能
6. 日志的异步模式,包括输出扩展(自定义输出器,目前实际是有的,只是没有暴露出来,也没有办法动态更新)
````

> 感谢 [github.com/duanhf2012/origin](https://github.com/duanhf2012/origin) 大佬提供的开源项目。本框架参考了其设计理念,做了一些适合自己项目的优化。
>
> 感谢 [https://github.com/RussellLuo/timingwheel](https://github.com/RussellLuo/timingwheel) 多层时间轮。
---
