# P3 RPC/Cluster 韧性增强开发文档

> 创建时间：2026年5月14日  
> 来源：`docs/ROADMAP.md` Phase 3 + `docs/NEXT_GOALS.md` Phase A-2 测试覆盖提升  
> 前置条件：P0/P1/P2 已完成，`go build/vet/test/race` 全绿

---

## 一、P3 总目标

P3 的核心目标是补齐 RPC 调用链和 Cluster/Router 的测试覆盖，确保超时、错误传播、去重、优雅关闭等关键路径有回归保护。同时推进 errorx 结构化错误码在 RPC wire error 场景的统一。

P3 不引入新功能，聚焦于**已有代码的韧性验证和收敛**。

---

## 二、P3 任务状态

| 编号 | 任务 | 当前状态 | 处理方式 |
|------|------|----------|----------|
| P3-1 | RPC 调用链回归测试 | ✅ 已完成 | 13 tests（Call/AsyncCall/Send 正常+超时+错误路径） |
| P3-2 | Remote Handler 回归测试 | ✅ 已完成 | 8 tests（去重/回复匹配/错误解析/nil dedup） |
| P3-3 | Router 单元测试 | ✅ 已完成 | 7 tests（nil 安全：Select/ByPid/ByServiceUid/ByRule/ByType/ByFilter） |
| P3-4 | errorx RPC wire error 收敛 | ✅ 已完成 | 验证确认：wire 测试已全面覆盖，sentinel 迁移为可选增强 |
| P3-5 | Graceful shutdown 集成验证 | ✅ 已完成 | 3 tests（AddAfterStop/StopIdempotent/StopClearsPending） |
| P3-6 | 全量构建 + 文档回填 | ✅ 已完成 | build/vet/test 全绿，P3 包 race clean |

---

## 三、开发原则

1. **不引入新功能**：P3 聚焦测试覆盖和已有代码的韧性验证。
2. **mock 优先**：使用 mock dispatcher/sender 进行单元测试，不依赖真实 etcd/NATS/gRPC。
3. **最小侵入**：如果发现 bug，修复代码应最小化。
4. **race 全绿**：所有新增测试必须通过 `-race -count=2`。

---

## 四、P3-1：RPC 调用链回归测试

### 问题描述

MessageBus 的 `call`/`asyncCall`/`send` 是框架最高频的热路径，当前测试仅覆盖参数校验和 MultiBus 聚合，缺少以下场景：

### 需要补齐的测试场景

| # | 场景 | 覆盖函数 | 优先级 |
|---|------|----------|--------|
| 1 | Call 正常路径：sender/receiver mock，回复成功 | `call()` | P0 |
| 2 | Call 超时路径：CallState 等待超时返回 ErrRPCCallTimeout | `call()` | P0 |
| 3 | Call DeliverRequest 失败：receiver 投递出错 | `call()` | P0 |
| 4 | Call sender=nil / receiver=nil 快速失败 | `call()` | P1 |
| 5 | Call 有预设 err（MultiBus 注入）快速返回 | `call()` | P1 |
| 6 | AsyncCall 正常路径：回调被执行 | `asyncCall()` | P0 |
| 7 | AsyncCall 无 callback 快速失败 | `AsyncCall()` | P1 |
| 8 | AsyncCall DeliverRequest 失败 | `asyncCall()` | P1 |
| 9 | Send 正常路径 | `send()` | P0 |
| 10 | Send receiver=nil 快速失败 | `send()` | P1 |
| 11 | Send DeliverRequest 失败 | `send()` | P1 |

### 实现要点

- 使用已有 `mockDispatcher`，扩展支持 `DeliverRequest` 注入错误
- 使用 `monitor.RpcMonitor` 真实实例 + `CallState` 模拟回复
- 超时测试使用短 timeout（10ms）

### 涉及文件

- `engine/pkg/rpc/message/msgbus/bus_test.go` — 扩展现有测试文件

---

## 五、P3-2：Remote Handler 回归测试

### 问题描述

`engine/pkg/rpc/remote/handler/handler.go` 是远程 RPC 的入口，负责：
1. 回复匹配（reply → RpcMonitor.Remove → state.Complete）
2. 请求去重（Seen → 跳过重复）
3. 请求反序列化 + context 重建
4. 错误解码（errorx.UnmarshalFromBytes）

当前 **0 个测试**。

### 需要补齐的测试场景

| # | 场景 | 优先级 |
|---|------|--------|
| 1 | Reply 匹配成功：state 被 Complete | P0 |
| 2 | Reply 匹配失败（迟到/超时）：丢弃不 panic | P0 |
| 3 | Reply 携带 error bytes：errorx 正确解码 | P0 |
| 4 | Request 去重命中：跳过处理 | P0 |
| 5 | Request 正常处理：envelope 构建正确 | P0 |
| 6 | Request 无 ReqId（send 场景）：跳过去重 | P1 |
| 7 | Request 反序列化失败：返回 error | P1 |
| 8 | Deduplicator nil：返回 error | P1 |

### 实现要点

- mock `IRpcSenderFactory`、`IDeDuplicator`、`IRpcDispatcher`
- 使用 `monitor.RpcMonitor` 真实实例测试回复匹配
- 使用 `codec.EncodeToAny` 构建测试 payload

### 涉及文件

- `engine/pkg/rpc/remote/handler/handler_test.go` — 新建

---

## 六、P3-3：Router 单元测试

### 问题描述

`engine/pkg/router/selector.go` 是一个薄代理层，将路由请求委托给 `EndpointManager.GetRepository()`。当前 **0 个测试**。

### 需要补齐的测试场景

| # | 场景 | 优先级 |
|---|------|--------|
| 1 | Router nil 安全：endpoints=nil 时返回 nil 不 panic | P0 |
| 2 | Select/SelectByPid/SelectByServiceUid 委托正确 | P1 |
| 3 | SelectByRule 自定义规则委托正确 | P1 |

### 实现要点

- Router 是薄代理，只需验证 nil 安全和委托调用
- 可能需要 mock EndpointManager 或直接构造

### 涉及文件

- `engine/pkg/router/selector_test.go` — 新建

---

## 七、P3-4：errorx RPC wire error 收敛

### 问题描述

当前 `def/error.go` 中的 sentinel 全部使用 `errors.New()`，跨节点传播时丢失错误码。`errorx` 包已有完整的序列化/反序列化能力（`MarshalToBytes`/`UnmarshalFromBytes`），但 RPC 层尚未统一使用。

### 任务拆分

| # | 任务 | 优先级 |
|---|------|--------|
| 1 | 验证 errorx 序列化/反序列化 round-trip 测试已覆盖 | P0 |
| 2 | 确认 handler.go 中 `errorx.UnmarshalFromBytes` 正确处理 nil/空/非法 bytes | P0 |
| 3 | 评估高价值 sentinel 迁移清单（ErrRPCCallTimeout/ErrServiceNotFound/ErrMethodNotFound） | P1 |
| 4 | 如果迁移，确保 errors.Is 兼容性不被破坏 | P1 |

### 涉及文件

- `engine/pkg/utils/errorx/` — 验证现有测试
- `engine/pkg/def/error.go` — 评估迁移候选
- `engine/pkg/rpc/remote/handler/handler.go` — 验证 UnmarshalFromBytes 边界

---

## 八、P3-5：Graceful shutdown 集成验证

### 问题描述

P1-4 已验证 Node Stop 幂等和逆序清理。P3 需要补充验证：
1. 正在进行的 RPC Call 在 shutdown 时是否能正确超时返回
2. AsyncCall 回调在 shutdown 后是否能安全执行或丢弃
3. sender 连接池关闭后新请求是否快速失败

### 需要补齐的测试场景

| # | 场景 | 优先级 |
|---|------|--------|
| 1 | RpcMonitor 清理：所有挂起 CallState 超时返回 | P0 |
| 2 | MessageBusFactory 关闭后 New 是否安全 | P1 |
| 3 | Service Stop 期间 PostJob 是否正确拒绝 | P0（已有 mailbox 测试覆盖） |

### 涉及文件

- `engine/pkg/monitor/` — 验证/补齐 RpcMonitor 关闭语义测试
- `engine/pkg/rpc/message/msgbus/` — 验证工厂关闭安全性

---

## 九、推荐实施顺序

```text
Step 1：P3-1 RPC 调用链回归测试
  └── 先覆盖最高频热路径，建立 mock 基础设施

Step 2：P3-2 Remote Handler 回归测试
  └── 复用 Step 1 的 mock，覆盖远程入口

Step 3：P3-3 Router 单元测试
  └── 薄代理层，快速完成

Step 4：P3-4 errorx 收敛
  └── 验证现有能力，评估迁移范围

Step 5：P3-5 Graceful shutdown 集成验证
  └── 验证 RpcMonitor 关闭语义

Step 6：P3-6 全量构建 + 文档回填
  └── go build/vet/test/race 全绿，ROADMAP 回填
```

---

## 十、验证命令

### P3 最小验证

```powershell
go test ./engine/pkg/rpc/... ./engine/pkg/router/... ./engine/pkg/monitor/... -count=1
```

### P3 并发验证

```powershell
go test -race -count=2 ./engine/pkg/rpc/... ./engine/pkg/router/... ./engine/pkg/monitor/...
```

### P3 收口验证

```powershell
go build ./...
go vet ./...
go test ./... -count=1
go test -race -count=2 ./engine/pkg/rpc/... ./engine/pkg/router/... ./engine/pkg/monitor/... ./engine/pkg/utils/errorx/...
```

---

## 十一、完成定义

P3 可以关闭的条件：

- [ ] P3-1：Call/AsyncCall/Send 超时+错误+正常路径有测试覆盖。
- [ ] P3-2：Remote Handler 去重/回复/错误解码有测试覆盖。
- [ ] P3-3：Router nil 安全有测试覆盖。
- [ ] P3-4：errorx round-trip 验证通过，迁移评估完成。
- [ ] P3-5：RpcMonitor 关闭语义有测试覆盖。
- [ ] `go build ./...`、`go vet ./...`、`go test ./... -count=1` 全部通过。
- [ ] 关键包 race 验证通过。
- [ ] ROADMAP/NEXT_GOALS 回填 P3 状态。

---

## 十二、风险与缓解措施

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| RpcMonitor 内部使用 TimingWheel | 单元测试需要真实时间 | 使用短 timeout（10-50ms）确保测试速度 |
| handler.go 依赖 codec.DecodeFromAny | 需要构造合法 protobuf Any | 使用 codec.EncodeToAny 生成测试数据 |
| errorx 迁移可能破坏 errors.Is 兼容 | 现有 sentinel 比对失效 | 评估阶段先不迁移，只验证新增 sentinel 使用 errorx |
| Router 是薄代理 | 测试价值有限 | 只验证 nil 安全，不过度测试委托逻辑 |
