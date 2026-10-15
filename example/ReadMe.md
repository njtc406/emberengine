# EmberEngine 示例目录

本目录包含 EmberEngine 框架的各种使用示例，从基础 RPC 调用到高并发压测和集群主从模式。

---

## 示例总览

| 目录 | 场景 | 依赖 | 说明 |
|------|------|------|------|
| `node_local` | 本地单节点 | etcd | 三个服务在同一节点内 RPC 互调 |
| `node1` | 跨节点 RPC（发送端） | etcd | 注册 Service1 + Service2，向 node2/3 发起调用 |
| `node2` / `node3` | 跨节点 RPC（接收端） | etcd | 注册 Service3，接受远端 RPC |
| `node_concurrency` | 并发压测（发送端） | etcd, NATS | 高并发 Send/Call/AsyncCall 性能测试 |
| `node_concurrency1` | 并发压测（接收端） | etcd, NATS | 压测目标节点 |
| `node_master` | 主从模式（主节点） | etcd | leadership guard + 状态同步 |
| `node_slave` / `node_slave1` | 主从模式（从节点） | etcd | 从节点跟随 + 故障切换 |
| `node_ctl` | 控制节点 | etcd | 全局事件测试 |

---

## 前置依赖

### etcd

所有示例需要 etcd（用于服务发现和主从选举）。

```bash
# 使用 Docker 快速启动
docker-compose -f template/docker/etcd-docker-compose.yaml up -d
```

或设置环境变量指向已有 etcd：

```powershell
$env:REMOTE_HOST = '192.168.145.188'  # etcd 地址
```

### NATS（可选）

跨节点事件和 nats RPC 类型需要 NATS 服务器。

---

## 场景一：本地 RPC 调用

最简单的入门示例，三个服务在同一进程内通过 RPC 互调。

```powershell
$env:REMOTE_HOST = '192.168.145.188'
go run ./example/node_local
```

**验证**：观察日志中 Service1 定时调用 Service2.APISum 的输出。

---

## 场景二：跨节点 RPC 调用

演示通过 gRPC/rpcx 进行跨进程 RPC 通信。

**启动顺序**：

```powershell
# 终端 1：启动接收端
$env:REMOTE_HOST = '192.168.145.188'
go run ./example/node2

# 终端 2：启动发送端
$env:REMOTE_HOST = '192.168.145.188'
go run ./example/node1
```

**验证**：node1 的 Service1 定时向 node2 的 Service3 发起 RPC 调用。

---

## 场景三：主从模式

演示基于 etcd 选举的主从切换和状态同步。

**启动顺序**：

```powershell
# 终端 1：启动主节点
$env:REMOTE_HOST = '192.168.145.188'
go run ./example/node_master

# 终端 2：启动从节点
$env:REMOTE_HOST = '192.168.145.188'
go run ./example/node_slave
```

**验证**：
1. 主节点获取 leadership，日志显示 "become master"
2. 停止主节点（Ctrl+C），从节点自动升主
3. 重启原主节点，自动成为从节点

**核心机制**：
- `leadership.Guard`：失主即停服务
- Fencing Token (Epoch)：防止脑裂下的过期写入
- 事件同步：通过 NATS 广播主从状态变更

---

## 场景四：并发压测

详细文档参见 [node_concurrency/README.md](node_concurrency/README.md)。

**快速启动**：

```powershell
# 终端 1：启动接收端
$env:REMOTE_HOST = '192.168.145.188'
go run ./example/node_concurrency1

# 终端 2：启动压测
$env:REMOTE_HOST = '192.168.145.188'
$env:BENCH_TOTAL = '100000'
$env:BENCH_CONCURRENCY = '500'
$env:BENCH_TYPE = 'send'
go run ./example/node_concurrency
```

---

## 公共服务代码

`example/comm/` 目录包含所有示例共用的服务定义：

| 文件 | 内容 |
|------|------|
| `test_service1.go` | Service1：定时 RPC 调用示例（Call/AsyncCall/Send） |
| `test_service2.go` | Service2：Module 挂载、多参数返回 |
| `test_service3.go` | Service3：基础 RPC Handler |
| `concurrency.go` | ConcurrencyTest：高并发压测逻辑 |
| `master_slaver.go` | MasterSlaverTest：主从模式状态机 |
| `test_mailbox_service.go` | MailboxTestService：多优先级 Mailbox 演示 |
| `test_rw.go` | ServiceRW：读写分离模式 |

---

## 配置文件

每个节点的配置在 `example/configs/node_*/` 目录下：

```
example/configs/
├── node_concurrency/     # 并发发送端配置
├── node_concurrency1/    # 并发接收端配置
├── node_local/           # 本地节点配置
├── node_master/          # 主节点配置
├── node_slave/           # 从节点配置
├── node_slave1/          # 从节点2配置
├── node1/                # 基础RPC节点1
├── node2/                # 基础RPC节点2
└── node3/                # 基础RPC节点3
```

---

## 端口分配

| 端口 | 用途 |
|------|------|
| 6060 | node_concurrency pprof |
| 6061 | node_concurrency1 pprof |
| 6670-6671 | RPC 服务端口 (rpcx/grpc) |
| 6680-6681 | RPC 服务端口 (rpcx/grpc) |

---

## 数据目录

```
example/data/
├── etcd/      # etcd 本地数据（如使用本地 etcd）
├── logs/      # 运行日志
└── pprof/     # pprof 采集数据
```

---

## 进一步阅读

- [快速开始指南](../docs/QUICK_START.md)
- [Service 开发指南](../docs/SERVICE_DEV_GUIDE.md)
- [配置参考手册](../docs/CONFIG_REFERENCE.md)
