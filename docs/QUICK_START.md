# EmberEngine 快速开始

本指南帮助你从零开始创建并运行第一个 EmberEngine 服务。

---

## 环境准备

- **Go 1.24+**
- **etcd**（可选，仅集群模式需要）
- **NATS**（可选，仅跨节点事件/RPC 需要）

> 本指南以单节点本地模式为例，无需 etcd 和 NATS。

---

## 1. 获取框架

```bash
# 创建项目
mkdir myproject && cd myproject
go mod init myproject

# 引入 EmberEngine
go get github.com/njtc406/emberengine@latest
```

---

## 2. 编写服务

创建 `service.go`：

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/njtc406/emberengine/engine/pkg/core"
    "github.com/njtc406/emberengine/engine/pkg/core/rpc"
    "github.com/njtc406/emberengine/engine/pkg/dto"
    "github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
    "github.com/njtc406/emberengine/engine/pkg/utils/xcontext"
)

// GreeterService 一个简单的问候服务
type GreeterService struct {
    core.Service
}

// OnInit 服务初始化，注册定时器
func (s *GreeterService) OnInit() error {
    // 每 3 秒调用一次 CalcService.APISum
    _, _ = s.AfterFunc(3*time.Second, "call calc", func(ctx context.Context, _ *timingwheel.Timer, _ ...interface{}) error {
        ctxTimeout, cancel := xcontext.NewWithTimeout(nil, time.Second)
        defer cancel()

        bus := s.Select(rpc.WithName("CalcService"), rpc.WithPartition(1))
        defer bus.Release()

        var result int
        if err := bus.CallWithOpt(ctxTimeout,
            dto.WithMethod("APISum"),
            dto.WithIn([]interface{}{10, 20}),
            dto.WithOut(&result),
        ); err != nil {
            s.WithContext(ctxTimeout).Errorf("call failed: %v", err)
        } else {
            s.WithContext(ctxTimeout).Infof("10 + 20 = %d", result)
        }
        return nil
    })
    return nil
}

// CalcService 提供计算能力的服务
type CalcService struct {
    core.Service
}

// APISum 对外暴露的 RPC 方法（方法名以 API 开头自动注册）
func (s *CalcService) APISum(ctx context.Context, a, b int) (int, error) {
    fmt.Printf("[CalcService] received: %d + %d\n", a, b)
    return a + b, nil
}
```

---

## 3. 编写主程序

创建 `main.go`：

```go
package main

import (
    "fmt"
    "os"
    "os/signal"
    "syscall"

    inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
    "github.com/njtc406/emberengine/engine/pkg/node"
    "github.com/njtc406/emberengine/engine/pkg/services"
)

func init() {
    // 注册服务工厂
    services.SetService("GreeterService", func() inf.IService {
        return &GreeterService{}
    })
    services.SetService("CalcService", func() inf.IService {
        return &CalcService{}
    })
}

func main() {
    n, err := node.New().Start(
        node.WithConfPath("./configs"),
    )
    if err != nil {
        panic(err)
    }

    // 等待退出信号
    exitCh := make(chan os.Signal, 1)
    signal.Notify(exitCh, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGINT)
    <-exitCh
    fmt.Println("shutting down...")
    n.Stop()
}
```

---

## 4. 编写配置

创建 `configs/node.yaml`：

```yaml
NodeConf:
  NodeId: 1
  NodeType: demo
  SystemStatus: debug
  PVCPath: ./data
  PVPath: ./cache
  AntsPoolSize: 1

ServiceConf:
  StartServices:
    - ClassName: CalcService
      ServiceName: CalcService
      Type: demo
      Partition: 1
    - ClassName: GreeterService
      ServiceName: GreeterService
      Type: demo
      Partition: 1

SystemLogger:
  Dir: ./data/logs
  PrefixName: demo
  Level: debug
  Stdout: true
  Color: true
```

> **启动顺序**：服务按配置中的顺序启动。CalcService 需在 GreeterService 之前启动，因为后者在 `OnInit` 中会调用前者。

---

## 5. 运行

```bash
# 创建数据目录
mkdir -p data/logs cache

# 启动
go run .
```

预期输出（每 3 秒一次）：

```
[CalcService] received: 10 + 20
... INFO  10 + 20 = 30
```

按 `Ctrl+C` 优雅退出。

---

## 6. 核心概念速查

| 概念 | 说明 |
|------|------|
| **Node** | 进程级容器，管理所有 Service 的生命周期 |
| **Service** | Actor 单元，拥有独立 Mailbox，消息驱动串行执行 |
| **Module** | Service 的子模块，共享 Service 的 Mailbox |
| **RPC** | 服务间通信：`Call`（同步）、`AsyncCall`（异步回调）、`Send`（单向） |
| **Event** | 事件系统：Global / Server / Specific 三级，支持 NATS 跨节点 |
| **Mailbox** | 消息队列：双队列模式（默认）或多优先级队列模式 |

---

## 下一步

- [Service 开发指南](SERVICE_DEV_GUIDE.md) — RPC、事件、定时器、读写分离等高级用法
- [配置参考手册](CONFIG_REFERENCE.md) — 完整配置项说明
- [示例目录](../example/README.md) — 集群、并发测试、主从模式等完整示例
