# EmberEngine 设计漏洞修复清单

## 已修复问题汇总

| 编号 | 问题 | 修复状态 | 说明 |
|------|------|----------|------|
| 1.1 | Actor邮箱忙等循环问题 | ✅ 已修复 | 实现了 `idle.Controller` 混合策略（backoff sleep + cond park） |
| 2.1 | 主从切换的脑裂风险 | ✅ 已修复 | 实现了 `leadership.Guard` 提供 epoch fencing token 和自动 ctx 取消 |
| 2.2 | 服务发现故障恢复机制不完善 | ✅ 已修复 | 使用 `idle.ExponentialBackoff` 无限重试，支持优雅退出 |
| 3.1 | MultiBus错误处理策略不合理 | ✅ 已修复 | 实现了 `CallMode` 策略（CallModeAny/CallModeAll） |
| 3.2 | 服务选择逻辑的主从分离 | ✅ 有意设计 | 默认走 Master 防止误写从服务，提供 `SelectSlavers` 显式接口 |
| 4.1 | Panic恢复机制的信息丢失 | ✅ 已修复 | 双重 panic 保护 + 提供 `circuitbreaker` 工具包供业务层熔断 |
| 4.2 | 超时机制的不一致性 | ✅ 已修复 | RPC 超时配置化，各层超时独立配置是有意设计 |
| 5.1 | MessageBus对象池的潜在泄漏 | ✅ 已修复 | 所有公开方法使用 `defer ReleaseMessageBus`，并有统计监控 |
| 5.2 | 动态扩缩容机制的死锁风险 | ✅ 已修复 | 使用 RLock + 复制 workers 列表后释放锁，决策在锁外执行 |
| 6.2 | 接口抽象层次不一致 | ✅ 已修复 | 拆分 `IRpcHandler`; ~~`IPID` 接口已回退~~ (性能考量，无实际替换需求) |

---

### ~~1.1 Actor邮箱忙等循环问题~~ ✅ 已修复

**修复说明:**
已实现 `idle.Controller` 混合策略，替代原有的忙等循环：
- 低空闲阶段：使用 exponential backoff sleep 保持活跃
- 高空闲阶段：使用 sync.Cond park 等待唤醒，零CPU消耗

**修复位置:** 
- `engine/pkg/utils/idle/idle.go` - 空闲控制器实现
- `engine/pkg/actor/mailbox/worker.go` - Worker 主循环使用 `idler.Idle()`

---

### 1.2 服务状态管理的竞态条件 🟡

**问题描述:**
Service的状态变更(status字段)使用原子操作，但与其他字段的组合操作不是原子的，可能导致状态不一致。

**文件位置:** `engine/pkg/core/service.go:40-45`

**问题代码:**
```go
type Service struct {
    // ...
    status                 int32        // 服务状态(0初始化 1启动中 2启动  3关闭中 4关闭 5退休)
    mailbox              inf.IMailbox        // 邮箱
    eventProcessor       inf.IEventProcessor // 事件管理器
    // ...
}
```

**修复方案:**
1. 引入状态机模式，统一管理状态转换
2. 使用读写锁保护复合状态操作
3. 增加状态一致性检查机制

**修复优先级:** 中
**预计工作量:** 1-2天

---

## 2. 分布式系统可靠性设计缺陷

### ~~2.1 主从切换的脑裂风险~~ ✅ 已修复

**修复说明:**
实现了 `leadership.Guard` 组件，提供：
- **Epoch Fencing Token**: 单调递增的 epoch 用于隔离旧 master 的副作用
- **自动 Context 取消**: 失去 leadership 时立即取消 ctx，停止进行中的 master-only 工作
- **事件驱动状态机**: 通过 `ServiceBecomeMaster`/`ServiceLoseMaster` 等事件驱动状态变更

**修复位置:** 
- `engine/pkg/cluster/leadership/guard.go` - Guard 实现
- `engine/pkg/cluster/leadership/guard_test.go` - 单元测试

**使用方式:**
```go
g := leadership.NewGuard(context.Background())
// 在事件处理器中:
g.OnEvent(ev)
// master-only 工作使用 g.Ctx():
go func() {
    <-g.Ctx().Done() // 失去 master 时自动停止
}()
// 副作用边界检查 epoch:
if g.Epoch() != expectedEpoch { return } // fencing
```

---

### ~~2.2 服务发现故障恢复机制不完善~~ ✅ 已修复

**修复说明:**
重构了 watcher 的重试机制：
- 使用 `idle.ExponentialBackoff` 统一退避策略（基础延迟1秒，最大延迟30秒）
- `Restart()` 和 `keepaliveLoop()` 均改为无限重试，直到成功或 watcher 被停止
- 支持通过 `ctx.Done()` 优雅退出，避免 goroutine 泄漏
- 控制日志输出频率，避免日志爆炸（前5次每次打印，之后每10次打印一次）
- 成功重连后重置退避计数器

**修复位置:** `engine/pkg/cluster/discovery/etcd/watcher.go`

---

## 3. 消息路由与选择器设计缺陷

### ~~3.1 MultiBus错误处理策略不合理~~ ✅ 已修复

**修复说明:**
已实现 `CallMode` 策略，支持：
- `CallModeAny`: 依次尝试调用每个服务，找到第一个成功的就返回（默认）
- `CallModeAll`: 所有节点都调用，收集所有结果

**修复位置:** `engine/pkg/rpc/message/msgbus/bus.go` - `CallWithOpt` 方法

---

### 3.2 服务选择逻辑的主从分离设计 ✅ 有意设计

**设计说明:**
选择器默认优先返回 Master 节点，这是**有意为之的安全设计**，而非缺陷。

**设计原因:**
1. **防止误写从服务**: 用户在不清楚主从的情况下可能误调用从服务进行写操作，导致数据丢失或不一致
2. **显式意图**: 提供专门的 `SelectSlavers` 接口，强制用户明确表达"我要调用从服务"的意图
3. **代码可读性**: 调用方代码能清晰表达当前调用的是主服务还是从服务，便于维护

**接口设计:**
- `Select()` / `SelectOne()` - 默认返回 Master，安全的默认行为
- `SelectSlavers()` - 显式获取从服务列表，用于读操作或特定场景

**文件位置:** `engine/pkg/cluster/endpoints/repository/selector.go`

**备注:** 如需负载均衡，可在 `SelectSlavers` 返回的列表中实现，或未来扩展 `SelectWithStrategy` 接口。

---

## 4. 错误处理与容错机制漏洞

### ~~4.1 Panic恢复机制的信息丢失~~ ✅ 已修复

**修复说明:**
框架层面已实现基础保护，熔断策略由业务层通过 `EscalateFailure` 自行实现：

1. **双重 panic 保护**: `safeExec` 中对 `EscalateFailure` 本身也做了 panic 保护
2. **Context 日志**: 使用 `logger.WithContext(ctx)` 支持链路追踪
3. **Worker 持续运行**: panic 后 Worker 不退出，继续处理后续消息
4. **熔断器工具包**: 提供 `circuitbreaker.CircuitBreaker` 供业务层使用

**修复位置:**
- `engine/pkg/actor/mailbox/worker.go` - `safeExec` 方法的双重保护
- `engine/pkg/utils/circuitbreaker/breaker.go` - 熔断器工具包

**使用方式:**
```go
type MyService struct {
    core.Service
    breaker *circuitbreaker.CircuitBreaker
}

func (s *MyService) OnInit() error {
    s.breaker = circuitbreaker.New(&circuitbreaker.Config{
        Threshold:   5,              // 连续5次失败触发熔断
        Timeout:     30 * time.Second, // 30秒后尝试恢复
        HalfOpenMax: 3,              // 半开状态允许3个请求
        OnStateChange: func(from, to circuitbreaker.State) {
            s.Warnf("circuit breaker: %s -> %s", from, to)
        },
    })
    return nil
}

func (s *MyService) EscalateFailure(ctx context.Context, reason interface{}, evt inf.IEvent) {
    s.breaker.RecordFailure()
    if s.breaker.State() == circuitbreaker.StateOpen {
        // 熔断状态，执行降级逻辑
        s.handleCircuitOpen(ctx)
    }
}
```

---

### ~~4.2 超时机制的不一致性~~ ✅ 已修复

**修复说明:**
经过分析，各层超时独立配置是有意为之的设计：
- **RPC 层超时** (1s): 业务调用快速失败，避免调用方长时间阻塞
- **网络层超时** (30s): 底层 TCP 连接管理，容忍网络抖动
- **事件层周期** (100ms): 批处理调度间隔，与超时无关

已将硬编码的 RPC 超时值改为可配置：

**修复位置:**
- `engine/pkg/config/define.go` - 添加 `RpcMonitorConf.DefaultRpcTimeout` 和 `CheckTimeoutInterval`
- `engine/pkg/config/init.go` - 添加 `GetDefaultRpcTimeout()` 和 `GetCheckTimeoutInterval()` 辅助函数
- `engine/pkg/rpc/message/msgbus/bus.go` - 使用 `config.GetDefaultRpcTimeout()`
- `engine/pkg/rpc/client/sender_remote_grpc.go` - 使用 `config.GetDefaultRpcTimeout()`
- `engine/pkg/rpc/client/sender_remote_rpcx.go` - 使用 `config.GetDefaultRpcTimeout()`
- `template/config/node.yaml` - 添加配置示例

**配置示例:**
```yaml
RpcMonitorConf:
  MonitorTimerSize: 10000
  MonitorBucketSize: 20
  # RPC 调用默认超时时间(不配置则默认1秒)
  DefaultRpcTimeout: 1s
  # RPC 超时检查间隔(不配置则默认1秒)
  CheckTimeoutInterval: 1s
```

---

## 5. 资源管理与内存泄漏风险

### ~~5.1 MessageBus对象池的潜在泄漏~~ ✅ 已修复

**修复说明:**
- 所有公开方法（`Call`, `CallWithOpt`, `AsyncCall`, `Send` 等）都使用 `defer ReleaseMessageBus(mb)` 确保释放
- 内部方法通过 `recycle` 参数控制是否释放
- 对象池有统计功能（`StatsRecorder`）可监控泄漏

**修复位置:** `engine/pkg/rpc/message/msgbus/bus.go`

---

### ~~5.2 动态扩缩容机制的死锁风险~~ ✅ 已修复

**修复说明:**
- `autoScaleWorkers` 中使用 `RLock` 读取 workers 列表，复制一份后立即释放锁
- 决策逻辑（`ShouldResize`）在锁外执行，避免长时间持锁
- `resizeWorkers` 使用独立的写锁，操作简洁

**修复位置:** `engine/pkg/actor/mailbox/worker_pool.go`

---

## 6. 配置与接口设计问题

### 6.1 配置验证不充分 🟡（已部分修复）

**问题描述:**
部分配置参数缺乏“合理性/范围/枚举”检查，可能导致系统启动失败或运行异常。

**现状核对（2025-12-29）:**
- 已存在统一校验框架：`engine/pkg/utils/validate`（基于 go-playground/validator）。
- 启动时已执行结构校验：`engine/pkg/config/init.go` 中 `validate.Struct(Conf)`。
- 配置结构体已使用 `binding:"required"` 等标签做必填校验（例如 `NodeConf.SystemStatus`、`ServiceConf.StartServices` 等）。

**仍建议补齐的点（按收益从高到低）:**
1. `SystemStatus` 等枚举字段使用 `oneof`/自定义 tag 校验（避免拼写导致启动后行为异常）
2. 数值/Duration 范围校验（例如池大小、timeout、bucket size 等；避免 0/负数/过大）
3. 外部依赖配置的语义校验（例如 etcd endpoints 非空、证书路径存在等）

**文件位置:** 多个配置文件

**修复方案:**
1. 在现有 `binding` 标签基础上补充范围/枚举校验（优先补“会导致启动失败/不可用”的字段）
2. 对关键配置给出更明确的报错信息（定位到字段与配置文件来源）
3. 配置示例/文档保持与 `define.go` 同步（已存在模板则补齐说明）
4. 配置热更新属于增强项，可单独作为中长期规划（不建议和校验混在同一修复项里）

**修复优先级:** 低
**预计工作量:** 1-2天

---

### ~~6.2 接口抽象层次不一致~~ ✅ 已修复

**问题描述:**
某些模块同时暴露了高层接口和底层实现细节，增加了错误使用的风险。

**已修复内容:**

1. **拆分 `IRpcHandler` 接口** - 将混合的接口拆分为不同抽象层次：

```go
// IRpcHandler 完整的 RPC 处理器接口（框架内部使用）
type IRpcHandler interface {
    IRpcInvoker
    IRpcProcessor
}

// IRpcInvoker 高层 RPC 调用接口（用户使用）
// 提供服务选择和方法查询能力
type IRpcInvoker interface {
    IRpcSelector
    GetMethods() []string
}

// IRpcProcessor 低层 RPC 消息处理接口（框架内部使用）
// 处理原始的 RPC 请求和响应消息
type IRpcProcessor interface {
    HandleRequest(ctx context.Context, msg IEnvelope)
    HandleResponse(ctx context.Context, msg IEnvelope)
}
```

**修复位置:** `engine/pkg/interfaces/IRpc.go`

2. **~~抽象 `IPID` 接口~~** - ⚠️ **已回退** (2024-12-28)

**回退原因:**
1. **性能考量**: 接口调用有间接调用开销，无法内联优化
2. **无替换需求**: `actor.PID` 是 protobuf 生成的，绑定网络协议，实际上不可能替换
3. **用户不创建 PID**: PID 由框架生成，用户只读取属性，无需接口抽象
4. **形式抽象**: 框架内部仍强依赖 `*actor.PID`，接口抽象变成了"形式上的抽象"，增加了大量类型断言代码

**当前状态:** 所有接口和方法直接使用 `*actor.PID` 类型

**待优化内容:**

| 优先级 | 改动 | 工作量 | 说明 |
|--------|------|--------|------|
| 🟡 中 | 隐藏 Protobuf 细节 | 1天 | `IEnvelope.ToProtoMsg` 改为 `ISerializable` |
| 🟢 低 | `ILogger`/`IProfiler` 接口化 | 0.5天 | 返回接口而非具体类型 |
| 🟢 低 | 配置接口化 | 1天 | `IDiscovery.Init` 使用配置接口 |

**修复优先级:** 低（建议在大版本重构时一并处理）
**预计剩余工作量:** 2-3天

---

## 修复计划建议

### 第一阶段（高优先级）- 预计2-3周
1. ~~Actor邮箱忙等循环问题 (1.1)~~ ✅ 已修复 - 实现了 `idle.Controller` 混合策略（backoff sleep + cond park）
2. ~~主从切换脑裂风险 (2.1)~~ ✅ 已修复 - 实现了 `leadership.Guard` 组件

### 第二阶段（中优先级）- 预计2-3周  
1. 服务状态竞态条件 (1.2)
2. ~~服务发现故障恢复 (2.2)~~ ✅ 已修复
3. ~~MultiBus错误处理 (3.1)~~ ✅ 已修复
4. ~~Panic恢复机制 (4.1)~~ ✅ 已修复
5. ~~对象池内存泄漏 (5.1)~~ ✅ 已修复
6. ~~扩缩容死锁风险 (5.2)~~ ✅ 已修复

### 第三阶段（低优先级）- 预计1-2周
1. ~~服务选择负载均衡 (3.2)~~ ✅ 有意设计（默认 Master；`SelectSlavers` 显式选择从）
2. ~~超时机制统一 (4.2)~~ ✅ 已处理（RPC 超时已配置化；各层独立超时为有意设计）
3. 配置合理性校验补全 (6.1)
4. ~~接口设计优化 (6.2)~~ ✅ 已修复

## 测试建议

1. **压力测试**: 针对高并发场景进行长时间压力测试
2. **故障注入**: 模拟网络分区、节点故障等异常情况
3. **内存泄漏检测**: 使用profiling工具监控内存使用
4. **性能基准测试**: 建立性能基线，量化优化效果

## 监控建议

1. **系统监控**: CPU、内存、网络等系统资源监控
2. **业务监控**: 消息处理延迟、错误率、吞吐量等
3. **集群监控**: 节点状态、主从切换、服务发现等
4. **告警机制**: 关键指标异常时及时告警

---

**注意**: 此清单基于当前代码分析得出，在实际修复过程中可能会发现其他问题，需要及时更新此清单。建议每个修复完成后进行充分测试，确保不引入新的问题。

---

# EmberEngine 模块化重组方案

## 版本信息
- 创建时间: 2025-11-14
- 当前状态: 待实施
- 优先级: 中长期规划

---

## 概述

当前项目存在模块间职责不清、依赖关系复杂的问题。本方案旨在通过重组目录结构和模块划分，实现更清晰的架构分层和更好的可维护性。

---

## 核心架构层（Core Layer）

### 1. Actor 系统模块 (`engine/pkg/actor`)

**当前状态**: 包含 mailbox、PID、事件定义

**职责**: Actor模型核心实现，消息传递和邮箱系统

**待完善**:
- [x] ~~邮箱性能优化（已部分完成，见上文1.1）~~ ✅ 已实现 `idle.Controller` 混合策略（backoff + cond park）
- [ ] Actor生命周期管理增强
- [ ] Actor监督策略完善
- [ ] 死信队列机制
- [ ] Actor地址解析优化

**预计工作量**: 1-2周

---

### 2. 核心服务模块 (`engine/pkg/core`)

**当前状态**: Service、Module、RPC、事件处理

**职责**: 服务和模块的基础抽象层

**待完善**:
- [ ] Service和Module的依赖注入
- [ ] 生命周期钩子标准化
- [ ] 热重载机制
- [ ] 服务降级和熔断
- [ ] 状态机模式实现（见上文1.2）

**预计工作量**: 1-2周

---

### 3. 接口定义模块 (`engine/pkg/interfaces`)

**当前状态**: 所有接口定义

**职责**: 系统核心接口契约

**待完善**:
- [ ] 接口文档完善
- [ ] 接口版本管理
- [ ] 向后兼容性策略
- [ ] 接口抽象层次优化（见上文6.2）

**预计工作量**: 3-5天

---

### 4. 类型和常量模块 (`engine/pkg/types` - 原 `def`)

**当前状态**: 错误定义、优先级、常量

**建议重命名**: `def` → `types` 更符合Go惯例

**职责**: 系统级常量和类型定义

**待完善**:
- [ ] 错误码统一管理
- [ ] 配置常量分离
- [ ] 类型定义文档化
- [ ] 错误处理标准化

**预计工作量**: 2-3天

---

## 通信层（Communication Layer）

### 5. RPC 通信模块 (`engine/pkg/communication/rpc`)

**当前状态**: `engine/pkg/rpc` - gRPC、NATS、RPCX客户端和远程服务

**建议重组**: 
```
engine/pkg/communication/rpc/
├── client/          # 客户端实现
├── server/          # 服务端实现
├── protocol/        # 协议适配器
│   ├── grpc/
│   ├── nats/
│   └── rpcx/
└── pool/           # 连接池管理
```

**职责**: 远程过程调用

**待完善**:
- [ ] 连接池管理优化（已部分完成）
- [ ] 熔断器和重试策略
- [ ] RPC超时和取消机制
- [ ] 协议扩展性增强
- [ ] 流式RPC支持
- [ ] 负载均衡集成

**预计工作量**: 2-3周

---

### 6. 事件系统模块 (`engine/pkg/communication/event`)

**当前状态**: `engine/pkg/event` - EventBus、事件处理器、节流

**建议移动**: `engine/pkg/event` → `engine/pkg/communication/event`

**职责**: 事件驱动通信

**待完善**:
- [ ] 事件持久化
- [ ] 事件溯源支持
- [ ] 事件过滤和路由优化
- [ ] 跨节点事件传播
- [ ] 事件重放机制

**预计工作量**: 1-2周

---

### 7. 消息传递模块 (`engine/pkg/communication/message`)

**当前状态**: `engine/internal/message` - 消息总线、消息信封

**建议移动**: `internal/message` → `pkg/communication/message`

**职责**: 内部消息路由和封装

**待完善**:
- [ ] 消息序列化优化
- [ ] 消息压缩支持
- [ ] 消息优先级队列优化
- [ ] 消息追踪和监控
- [x] ~~MultiBus错误处理优化（见上文3.1）~~ ✅ 已实现 CallMode 策略

**预计工作量**: 1周

---

## 集群和服务发现层（Cluster Layer）

### 8. 集群模块 (`engine/pkg/cluster`)

**当前状态**: 服务发现、端点管理

**建议重组**:
```
engine/pkg/cluster/
├── discovery/       # 服务发现
├── registry/        # 服务注册
├── endpoints/       # 端点管理
└── loadbalancer/    # 负载均衡（新增）
```

**职责**: 集群管理和服务协调

**待完善**:
- [ ] 多种服务发现后端支持（当前仅ETCD）
- [ ] 健康检查机制完善
- [ ] 负载均衡策略实现（见上文3.2）
- [ ] 服务元数据管理
- [ ] 服务分组和版本管理
- [x] ~~主从切换脑裂防护（见上文2.1）~~ ✅ 已实现 `leadership.Guard`
- [x] ~~故障恢复机制增强（见上文2.2）~~ ✅ 已实现无限重试 + 指数退避

**预计工作量**: 3-4周

---

### 9. 路由模块 (`engine/pkg/cluster/router`)

**当前状态**: `engine/pkg/router` - 选择器实现

**建议移动**: `engine/pkg/router` → `engine/pkg/cluster/router`

**职责**: 服务路由和负载均衡

**待完善**:
- [ ] 多种负载均衡算法（轮询、随机、加权、一致性哈希）
- [ ] 动态路由规则
- [ ] 灰度发布支持
- [ ] 路由策略配置化
- [ ] 智能路由算法

**预计工作量**: 1-2周

---

## 网关层（Gateway Layer）

### 10. 网关统一模块 (`engine/pkg/gateway`)

**当前状态**: 分散在 `sysModule/gate`、`sysModule/httpmodule`、`sysModule/wsmodule`

**建议重组**:
```
engine/pkg/gateway/
├── http/            # HTTP网关
├── websocket/       # WebSocket网关
├── tcp/             # TCP网关（新增）
├── udp/             # UDP网关（新增）
└── protocol/        # 协议适配器接口
```

**职责**: 外部通信入口和协议适配

**待完善**:
- [ ] 协议适配器标准化
- [ ] 请求限流和熔断
- [ ] 认证授权中间件
- [ ] API版本管理
- [ ] OpenAPI文档生成
- [ ] HTTP/2和HTTP/3支持
- [ ] WebSocket消息广播优化
- [ ] 断线重连和心跳机制

**预计工作量**: 2-3周

---

## 数据访问层（Data Layer）

### 11. 数据库访问模块 (`engine/pkg/database`)

**当前状态**: 分散在 `sysModule/mysqlmodule`、`sysModule/mongomodule`、`sysModule/redismodule`

**建议重组**:
```
engine/pkg/database/
├── mysql/           # MySQL访问
├── mongodb/         # MongoDB访问
├── redis/           # Redis访问
├── orm/             # ORM抽象层
└── migration/       # 数据库迁移工具（新增）
```

**职责**: 统一的数据访问层

**待完善**:
- [ ] 连接池管理统一
- [ ] 事务支持完善
- [ ] 读写分离
- [ ] 数据库迁移工具
- [ ] ORM集成优化
- [ ] 缓存策略实现
- [ ] 数据库监控和慢查询分析

**预计工作量**: 2-3周

---

### 12. DTO 数据传输模块 (`engine/pkg/dto`)

**当前状态**: RPC数据定义

**职责**: 数据传输对象定义

**待完善**:
- [ ] 数据校验注解
- [ ] 数据转换工具
- [ ] 序列化优化
- [ ] 版本兼容性

**预计工作量**: 3-5天

---

## 基础设施层（Infrastructure Layer）

### 13. 配置管理模块 (`engine/pkg/infrastructure/config`)

**当前状态**: `engine/pkg/config`

**建议移动**: 提升到infrastructure层

**职责**: 配置加载和管理

**待完善**:
- [ ] 配置热更新
- [ ] 配置版本管理
- [ ] 配置加密
- [ ] 多环境配置支持
- [ ] 配置校验框架（见上文6.1）
- [ ] 配置中心集成

**预计工作量**: 1-2周

---

### 14. 日志模块 (`engine/pkg/infrastructure/logging`)

**当前状态**: `engine/pkg/utils/log`

**建议提升**: utils层 → infrastructure层

**职责**: 统一的日志系统

**待完善**:
- [ ] 结构化日志标准化
- [ ] 日志分级存储
- [ ] 日志聚合和查询
- [ ] 链路追踪集成
- [ ] 日志采样和过滤
- [ ] 异步日志写入优化

**预计工作量**: 1周

---

### 15. 监控和性能分析模块 (`engine/pkg/infrastructure/monitoring`)

**当前状态**: `engine/pkg/profiler`、`engine/internal/monitor`

**建议重组**:
```
engine/pkg/infrastructure/monitoring/
├── profiling/       # 性能分析
├── metrics/         # 指标收集
├── tracing/         # 分布式追踪
└── alerting/        # 告警系统（新增）
```

**职责**: 系统监控和性能分析

**待完善**:
- [ ] Prometheus集成
- [ ] 自定义指标采集
- [ ] 分布式追踪（OpenTelemetry）
- [ ] 性能告警机制
- [ ] 监控仪表板
- [ ] 业务监控指标

**预计工作量**: 2-3周

---

### 16. 定时任务模块 (`engine/pkg/infrastructure/timing`)

**当前状态**: `engine/pkg/utils/timingwheel`、`engine/internal/monitor`

**建议重组**: 合并定时相关功能

**职责**: 定时任务调度

**待完善**:
- [ ] 分布式定时任务
- [ ] 定时任务持久化
- [ ] 定时任务监控
- [ ] Cron表达式支持
- [ ] 多级时间轮优化
- [ ] 任务依赖管理

**预计工作量**: 1-2周

---

## 公共工具层（Common Layer）

### 17. 序列化模块 (`engine/pkg/common/codec`)

**当前状态**: `engine/pkg/utils/codec`

**建议提升**: 从utils提升到common

**职责**: 数据序列化和编解码

**待完善**:
- [ ] MessagePack支持
- [ ] 自定义序列化协议
- [ ] 序列化性能优化
- [ ] 零拷贝序列化

**预计工作量**: 3-5天

---

### 18. 并发控制模块 (`engine/pkg/common/concurrent`)

**当前状态**: `engine/pkg/utils/concurrent`

**建议提升**: 从utils提升到common

**职责**: 并发原语和控制

**待完善**:
- [ ] 协程池实现
- [ ] 并发限制器
- [ ] 异步任务队列
- [ ] 分布式锁
- [x] ~~扩缩容死锁防护（见上文5.2）~~ ✅ 已修复

**预计工作量**: 1周

---

### 19. 对象池模块 (`engine/pkg/common/pool`)

**当前状态**: 分散在多个包中

**职责**: 对象复用和内存管理

**待完善**:
- [ ] 自适应池大小
- [ ] 池统计和监控
- [ ] 多种池策略
- [x] ~~对象池泄漏检测（见上文5.1）~~ ✅ 已实现 StatsRecorder 监控

**预计工作量**: 3-5天

---

### 20. 上下文模块 (`engine/pkg/common/context`)

**当前状态**: `engine/pkg/utils/xcontext`

**建议重命名**: xcontext → context

**职责**: 上下文传递和管理

**待完善**:
- [ ] 元数据传递优化
- [ ] 超时控制统一（见上文4.2）
- [ ] 取消传播机制
- [ ] 上下文值类型安全

**预计工作量**: 2-3天

---

### 21. 错误处理模块 (`engine/pkg/common/errors`)

**当前状态**: 分散在各个模块

**建议新建**: 统一错误处理

**职责**: 错误定义、包装和处理

**待完善**:
- [ ] 错误链追踪
- [ ] 错误码标准化
- [ ] 错误国际化
- [ ] Panic恢复策略（见上文4.1）

**预计工作量**: 3-5天

---

## 节点和服务层（Node & Service Layer）

### 22. 节点管理模块 (`engine/pkg/node`)

**当前状态**: 节点启动和管理

**职责**: 进程级管理和协调

**待完善**:
- [ ] 优雅关闭机制
- [ ] 资源清理完善
- [ ] 信号处理标准化
- [ ] 进程监控集成
- [ ] 多实例管理

**预计工作量**: 1周

---

### 23. 服务注册模块 (`engine/pkg/services`)

**当前状态**: 服务工厂

**职责**: 服务创建和注册

**待完善**:
- [ ] 依赖注入框架
- [ ] 服务发现集成
- [ ] 服务生命周期管理
- [ ] 服务编排

**预计工作量**: 1-2周

---

### 24. 插件系统模块 (`engine/pkg/plugins`)

**当前状态**: 基础插件框架

**职责**: 插件加载和管理

**待完善**:
- [ ] 插件热加载
- [ ] 插件依赖管理
- [ ] 插件安全沙箱
- [ ] 插件生命周期管理
- [ ] 插件配置管理

**预计工作量**: 2-3周

---

## 建议的新目录结构

```
emberengine/
├── engine/
│   ├── pkg/                           # 公共包
│   │   ├── actor/                     # Actor系统
│   │   │   ├── mailbox/
│   │   │   ├── pid/
│   │   │   └── supervisor/
│   │   ├── core/                      # 核心抽象
│   │   │   ├── service/
│   │   │   ├── module/
│   │   │   └── lifecycle/
│   │   ├── communication/             # 通信层（重组）
│   │   │   ├── rpc/
│   │   │   │   ├── client/
│   │   │   │   ├── server/
│   │   │   │   ├── protocol/
│   │   │   │   └── pool/
│   │   │   ├── event/
│   │   │   └── message/
│   │   ├── cluster/                   # 集群管理
│   │   │   ├── discovery/
│   │   │   ├── registry/
│   │   │   ├── endpoints/
│   │   │   ├── router/
│   │   │   └── loadbalancer/
│   │   ├── gateway/                   # 网关层（重组）
│   │   │   ├── http/
│   │   │   ├── websocket/
│   │   │   ├── tcp/
│   │   │   ├── udp/
│   │   │   └── protocol/
│   │   ├── database/                  # 数据访问（重组）
│   │   │   ├── mysql/
│   │   │   ├── mongodb/
│   │   │   ├── redis/
│   │   │   ├── orm/
│   │   │   └── migration/
│   │   ├── infrastructure/            # 基础设施（重组）
│   │   │   ├── config/
│   │   │   ├── logging/
│   │   │   ├── monitoring/
│   │   │   │   ├── profiling/
│   │   │   │   ├── metrics/
│   │   │   │   ├── tracing/
│   │   │   │   └── alerting/
│   │   │   └── timing/
│   │   ├── common/                    # 公共工具（重组）
│   │   │   ├── codec/
│   │   │   ├── concurrent/
│   │   │   ├── pool/
│   │   │   ├── context/
│   │   │   └── errors/
│   │   ├── node/                      # 节点管理
│   │   ├── services/                  # 服务注册
│   │   ├── plugins/                   # 插件系统
│   │   ├── interfaces/                # 接口定义
│   │   ├── types/                     # 类型定义（原def）
│   │   ├── dto/                       # 数据传输对象
│   │   └── utils/                     # 其他工具（保留少量）
│   └── internal/                      # 内部实现
│       └── (framework internals)
├── example/                           # 示例代码
├── template/                          # 配置模板
├── docs/                              # 文档（建议新增）
└── tools/                             # 开发工具（建议新增）
```

---

## 迁移优先级和时间计划

### 第一阶段：核心稳定化（2-3周）
**目标**: 修复关键设计问题，为重构打基础

1. [x] ~~Actor邮箱系统优化~~ ✅ 已完成 - 实现 `idle.Controller` 混合策略
2. [ ] 服务状态管理竞态条件修复
3. [ ] Panic恢复和错误处理机制
4. [ ] 资源泄漏检测和修复

### 第二阶段：通信层重组（3-4周）
**目标**: 统一通信层架构

5. [ ] RPC模块重组（`pkg/rpc` → `pkg/communication/rpc`）
6. [ ] 事件系统移动（`pkg/event` → `pkg/communication/event`）
7. [ ] 消息模块提升（`internal/message` → `pkg/communication/message`）
8. [x] ~~MultiBus错误处理优化~~ ✅ 已实现 CallMode 策略

### 第三阶段：集群层增强（3-4周）
**目标**: 完善集群管理能力

9. [x] ~~主从切换脑裂防护~~ ✅ 已实现 `leadership.Guard`
10. [x] ~~服务发现故障恢复增强~~ ✅ 已实现无限重试 + 指数退避
11. [ ] 路由模块移动和优化
12. [ ] 负载均衡实现

### 第四阶段：网关层统一（2-3周）
**目标**: 统一网关架构

13. [ ] HTTP/WebSocket模块合并到gateway
14. [ ] 协议适配器标准化
15. [ ] 网关中间件生态
16. [ ] 认证授权框架

### 第五阶段：基础设施完善（2-3周）
**目标**: 完善监控和运维能力

17. [ ] 配置管理增强
18. [ ] 日志系统提升
19. [ ] 监控系统集成
20. [ ] 定时任务优化

### 第六阶段：数据层和工具（2-3周）
**目标**: 统一数据访问和公共工具

21. [ ] 数据库模块重组
22. [ ] 公共工具层整理
23. [ ] 错误处理统一
24. [ ] 上下文管理优化

### 第七阶段：扩展和优化（2-3周）
**目标**: 插件系统和开发体验

25. [ ] 插件系统完善
26. [ ] 开发工具和脚手架
27. [ ] 性能优化和基准测试
28. [ ] 文档完善

**总预计时间**: 16-22周（约4-5.5个月）

---

## 迁移注意事项

### 兼容性策略

1. **渐进式迁移**: 不要一次性重构所有模块
2. **别名导出**: 在过渡期提供旧路径的别名
3. **废弃警告**: 使用编译器警告标记废弃的API
4. **版本标记**: 使用语义化版本管理变更

### 测试策略

1. **单元测试**: 每个模块重组后补充单元测试
2. **集成测试**: 确保模块间交互正常
3. **回归测试**: 防止功能退化
4. **性能测试**: 确保性能不下降

### 文档策略

1. **迁移指南**: 为每个阶段提供迁移文档
2. **API文档**: 使用godoc标准化API文档
3. **架构文档**: 更新架构设计文档
4. **示例代码**: 提供新架构的使用示例

---

## 依赖关系梳理

### 当前存在的循环依赖

1. `cluster/discovery` ↔ `def`
2. `rpc/client` ↔ `def`
3. `actor/mailbox` ↔ `interfaces`

### 解决方案

1. **接口下沉**: 将共享接口移到interfaces包
2. **类型分离**: 错误和常量独立到types包
3. **依赖注入**: 使用DI容器解耦模块依赖

---

## 成功指标

### 代码质量
- [ ] 单元测试覆盖率 > 70%
- [ ] 无循环依赖
- [ ] 代码重复率 < 3%
- [ ] 静态分析零告警

### 性能指标
- [ ] 消息处理延迟 < 1ms (P99)
- [ ] RPC调用延迟 < 5ms (P99)
- [ ] 系统吞吐量 > 100k msg/s
- [ ] 内存占用稳定，无泄漏

### 可维护性
- [ ] 模块职责单一清晰
- [ ] 依赖关系简单明确
- [ ] 接口抽象合理
- [ ] 文档完整准确

---

**备注**: 此重组方案需要较长时间实施，建议分阶段进行，每个阶段完成后进行充分测试和验证。在实施过程中保持向后兼容，避免影响现有用户。