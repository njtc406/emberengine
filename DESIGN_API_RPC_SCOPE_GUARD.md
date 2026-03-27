# Api 方法远程调用拦截（Scope Guard）

> 最后更新：2026-03-25  
> 状态：待实施

## 问题

`Api` 前缀方法设计为节点内调用，`Rpc` 前缀方法为跨节点调用。但当前 `HandleRequest` 不区分来源，远程 RPC 可以调到 `Api` 方法。

## 方案

在 `Dispatcher.DeliverRequest` 中，通过 `sender.IsLocal()` 判断调用路径。非本地 sender 时，用 `INodeMethodIndex` 检查 method 前缀，仅放行 `Rpc`/`RpcRo` 前缀。

```
sender.IsLocal() = true  → 跳过检查（本地调用）
sender.IsLocal() = false → 检查前缀 → 非 Rpc 前缀 → 拒绝
```

## 变更

| # | 文件 | 变更 |
|---|------|------|
| 1 | `engine/pkg/interfaces/IRpcClient.go` | `IRpcSender` 增加 `IsLocal() bool` |
| 2 | `engine/pkg/rpc/client/sender_local.go` | `IsLocal() → true` |
| 3 | `engine/pkg/rpc/client/sender_remote_grpc.go` | `IsLocal() → false` |
| 4 | `engine/pkg/rpc/client/sender_remote_rpcx.go` | `IsLocal() → false` |
| 5 | `engine/pkg/rpc/client/sender_remote_nats.go` | `IsLocal() → false` |
| 6 | `engine/pkg/rpc/client/sender.go` | `SenderManager` 增加 `methodIdx` 字段；`DeliverRequest` 加校验 |
| 7 | `engine/pkg/node/node.go` | `NewSenderManager` 传入 `MethodIndex`，调整初始化顺序 |
| 8 | `engine/pkg/def/` | 新增 `ErrRemoteCallLocalMethod` |

不变：`IMethodMgr`、`IEnvelopeMeta`、`HandleRequest`、`NewDispatcher` 签名、protobuf。

## 核心代码

```go
// IRpcSender 新增
IsLocal() bool

// localSender
func (lc *localSender) IsLocal() bool { return true }

// grpcSender / rpcxSender / natsSender
func (rc *xxxSender) IsLocal() bool { return false }

// SenderManager 新增字段
methodIdx inf.INodeMethodIndex

// Dispatcher.DeliverRequest 校验
if !sender.IsLocal() {
    if idx := c.sm.methodIdx; idx != nil {
        method := envelope.GetData().GetMethod()
        if !idx.HasRpcPrefix(method) && !idx.HasRpcReadOnlyPrefix(method) {
            return def.ErrRemoteCallLocalMethod
        }
    }
}
```

## 实施步骤

1. 新增 `ErrRemoteCallLocalMethod` 错误码
2. `IRpcSender` 增加 `IsLocal() bool`，四个 sender 各加 1 行实现
3. `SenderManager` 增加 `methodIdx`，`NewSenderManager` 增加参数
4. `Dispatcher.DeliverRequest` 增加校验
5. `node.go` 先创建 `MethodIndex`，再传入 `NewSenderManager`
6. 更新测试中 `NewSenderManager` 调用（`bus_benchmark_test.go`）
7. 更新 pool 测试中 `mockPoolSender` 增加 `IsLocal()` 实现
8. 运行测试验证

## 注意事项

### 并发安全

- **`SenderManager.methodIdx`**：init 后只读，安全。`MethodIndex` 在 `Node.Init()` 中创建后不再重新赋值。
- **`prefixBucketIndex.has()`**：init 后只读，安全。`byFirst` 数组在 `newPrefixBucketIndex` 中填充完毕后不再修改。
- **`envelope.GetData().GetMethod()`**：`Data` 内部有 `RWMutex` 保护，安全。在 `DeliverRequest` 调用时 envelope 所有权已归调用方，不存在并发写。
- **`prefixBucketIndex.add()` 风险**：`SetApiPrefix`/`SetRpcPrefix` 等方法是 public 的，如果用户在运行期调用，会与 `has()` 产生 data race。**这是已有问题，非本方案引入**，建议后续在这些方法上加文档说明"仅限启动阶段调用"或加锁。

### 性能

- 校验仅在 **远程路径** 触发（`!sender.IsLocal()`），本地调用零开销
- `HasRpcPrefix` 内部是 `byFirst[s[0]]` 数组查找 + `strings.HasPrefix`，O(1) 级别，无内存分配
- 远程路径本身已有网络 I/O（毫秒级），增加的纳秒级前缀校验完全可忽略

### PoolManager

- `PoolConnection.Sender` 类型为 `IRpcSender`，pool 中的 sender 全部是远程类型（grpc/rpcx/nats），由 `SenderManager.registerCreators` 注册
- pool 中 sender 的 `IsLocal()` 返回 `false`，与非 pool 行为一致
- pool 测试中 `mockPoolSender` 需补充 `IsLocal() bool` 实现
